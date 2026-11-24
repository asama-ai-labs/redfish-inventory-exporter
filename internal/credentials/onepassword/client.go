// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Asama Inc.

package onepassword

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/1password/onepassword-sdk-go"
	"github.com/asama-ai-labs/redfish-inventory-exporter/internal/credentials"
)

const (
	secretCacheTTL = 5 * time.Minute
	lookupAttempts = 3
)

const (
	integrationName    = "redfish-inventory-exporter"
	integrationVersion = "1.0.0"
)

// ErrLookupFailed is a redacted 1Password failure. It never includes secret values or references.
var ErrLookupFailed = errors.New("1password credential lookup failed")

// MissingFieldError means a required username or password field was not resolved.
// Vault and Item identify the missed lookup in logs; neither is a secret.
type MissingFieldError struct {
	Field string
	Vault string
	Item  string
}

func (e *MissingFieldError) Error() string {
	switch e.Field {
	case "username", "password":
		return "1password required field missing: " + e.Field
	default:
		return "1password required field missing"
	}
}

func (e *MissingFieldError) Unwrap() error {
	return credentials.ErrNotFound
}

type login struct {
	Username string
	Password string
}

type fieldValue struct {
	Secret  string
	Missing bool
}

type resolver interface {
	ResolveAll(ctx context.Context, refs []string) (map[string]fieldValue, error)
}

type secretProvider struct {
	settings *Settings
	resolver resolver

	mu       sync.Mutex
	cache    map[string]cachedLogin
	inflight map[string]*loginCall
	// sdkGate allows one detached SDK call. The WASM module runs one call at a
	// time, and a timed-out call keeps running until it finishes.
	sdkGate chan struct{}
}

type cachedLogin struct {
	value   login
	expires time.Time
}

type loginCall struct {
	done chan struct{}
	val  login
	err  error
}

func newProvider(settings *Settings, backend resolver) (*secretProvider, error) {
	if settings == nil {
		return nil, errors.New("1password vault is invalid")
	}
	if err := settings.validate(); err != nil {
		return nil, err
	}
	if backend == nil {
		return nil, errors.New("1password resolver is required")
	}
	return &secretProvider{
		settings: settings,
		resolver: backend,
		cache:    map[string]cachedLogin{},
		inflight: map[string]*loginCall{},
		sdkGate:  make(chan struct{}, 1),
	}, nil
}

func requireToken() error {
	if strings.TrimSpace(os.Getenv("OP_SERVICE_ACCOUNT_TOKEN")) == "" {
		return errors.New("OP_SERVICE_ACCOUNT_TOKEN is required when credentials provider is onepassword")
	}
	return nil
}

// NewSDK initializes a 1Password client from OP_SERVICE_ACCOUNT_TOKEN.
// The token is not stored and errors returned here do not include it.
func NewSDK(ctx context.Context, settings *Settings) (*secretProvider, error) {
	if err := requireToken(); err != nil {
		return nil, err
	}
	token := strings.TrimSpace(os.Getenv("OP_SERVICE_ACCOUNT_TOKEN"))
	if ctx == nil {
		ctx = context.Background()
	}
	client, err := onepassword.NewClient(
		ctx,
		onepassword.WithServiceAccountToken(token),
		onepassword.WithIntegrationInfo(integrationName, integrationVersion),
	)
	if err != nil {
		return nil, errors.New("failed to initialize 1Password client")
	}
	return newProvider(settings, sdkResolver{client: client})
}

func (p *secretProvider) Resolve(ctx context.Context, subject credentials.Subject) (login, error) {
	ip, err := credentials.NormalizeTargetIP(subject.Target)
	if err != nil {
		return login{}, err
	}
	item, err := credentials.RenderItemName(p.settings.ItemTemplate, credentials.Name{
		IP:           ip,
		Hostname:     subject.Hostname,
		SerialNumber: subject.SerialNumber,
	})
	if err != nil {
		if errors.Is(err, credentials.ErrInvalidTarget) || fieldErr(err) {
			return login{}, err
		}
		return login{}, ErrLookupFailed
	}
	return p.resolveItem(ctx, item)
}

func fieldErr(err error) bool {
	var field *credentials.FieldError
	return errors.As(err, &field)
}

// ResolveDefault reads the shared login. default_item is that item's title.
// When it is empty, every placeholder in the template is replaced with "default".
func (p *secretProvider) ResolveDefault(ctx context.Context) (login, error) {
	item := p.settings.DefaultItem
	if item == "" {
		var err error
		item, err = credentials.RenderDefaultItemName(p.settings.ItemTemplate)
		if err != nil {
			return login{}, credentials.ErrNotFound
		}
	}
	return p.resolveItem(ctx, item)
}

func (p *secretProvider) resolveItem(ctx context.Context, item string) (login, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if got, ok := p.cached(item, true); ok {
		return got, nil
	}
	got, err := p.singleflight(ctx, item, func() (login, error) {
		return p.fetchItem(ctx, item)
	})
	if err == nil {
		return got, nil
	}
	var missing *MissingFieldError
	if errors.As(err, &missing) {
		return login{}, err
	}
	if got, ok := p.cached(item, false); ok {
		return got, nil
	}
	return login{}, err
}

func (p *secretProvider) fetchItem(ctx context.Context, item string) (login, error) {
	userRef, err := secretReference(p.settings.Vault, item, p.settings.Fields.Username)
	if err != nil {
		return login{}, ErrLookupFailed
	}
	passRef, err := secretReference(p.settings.Vault, item, p.settings.Fields.Password)
	if err != nil {
		return login{}, ErrLookupFailed
	}
	budget, cancel := context.WithTimeout(ctx, p.settings.Timeout)
	defer cancel()

	var last error
retry:
	for attempt := 0; attempt < lookupAttempts; attempt++ {
		if budget.Err() != nil {
			break
		}
		got, err := p.fetchOnce(budget, item, userRef, passRef)
		if err == nil {
			p.remember(item, got)
			return got, nil
		}
		last = err
		var missing *MissingFieldError
		if errors.As(err, &missing) || budget.Err() != nil {
			return login{}, err
		}
		if attempt+1 == lookupAttempts {
			break
		}
		timer := time.NewTimer(time.Duration(attempt+1) * 100 * time.Millisecond)
		select {
		case <-budget.Done():
			timer.Stop()
			break retry
		case <-timer.C:
		}
	}
	if last == nil {
		last = ErrLookupFailed
	}
	return login{}, last
}

func (p *secretProvider) fetchOnce(ctx context.Context, item, userRef, passRef string) (login, error) {
	// The 1Password SDK runs a shared WASM module. Cancelling the context passed
	// into that module closes it for every later lookup. Wait on our timeout,
	// and call the SDK with a context that is not cancelled.
	resolved, err := resolveDetached(ctx, p.sdkGate, func() (map[string]fieldValue, error) {
		return p.resolver.ResolveAll(context.Background(), []string{userRef, passRef})
	})
	if err != nil {
		return login{}, ErrLookupFailed
	}
	username, err := requiredField(resolved, userRef, "username", p.settings.Vault, item)
	if err != nil {
		return login{}, err
	}
	password, err := requiredField(resolved, passRef, "password", p.settings.Vault, item)
	if err != nil {
		return login{}, err
	}
	return login{Username: username, Password: password}, nil
}

func requiredField(resolved map[string]fieldValue, ref, logical, vault, item string) (string, error) {
	value, ok := resolved[ref]
	if !ok || value.Missing || value.Secret == "" {
		return "", &MissingFieldError{Field: logical, Vault: vault, Item: item}
	}
	return value.Secret, nil
}

func resolveDetached[T any](ctx context.Context, gate chan struct{}, fn func() (T, error)) (T, error) {
	select {
	case gate <- struct{}{}:
	case <-ctx.Done():
		var zero T
		return zero, ErrLookupFailed
	}
	type result struct {
		value T
		err   error
	}
	done := make(chan result, 1)
	go func() {
		defer func() { <-gate }()
		value, err := fn()
		done <- result{value, err}
	}()
	select {
	case <-ctx.Done():
		var zero T
		return zero, ErrLookupFailed
	case got := <-done:
		return got.value, got.err
	}
}

func secretReference(vaultName, item, field string) (string, error) {
	if err := credentials.ValidateSegment(vaultName); err != nil {
		return "", err
	}
	if err := credentials.ValidateItemTitle(item); err != nil {
		return "", err
	}
	if err := credentials.ValidateSegment(field); err != nil {
		return "", err
	}
	return "op://" + vaultName + "/" + item + "/" + field, nil
}

type sdkResolver struct {
	client *onepassword.Client
}

func (s sdkResolver) ResolveAll(ctx context.Context, refs []string) (map[string]fieldValue, error) {
	resp, err := s.client.Secrets().ResolveAll(ctx, refs)
	if err != nil {
		return nil, ErrLookupFailed
	}
	out := make(map[string]fieldValue, len(refs))
	for _, ref := range refs {
		item, ok := resp.IndividualResponses[ref]
		if !ok {
			out[ref] = fieldValue{Missing: true}
			continue
		}
		if item.Error != nil {
			if missingReference(item.Error.Type) {
				out[ref] = fieldValue{Missing: true}
				continue
			}
			return nil, ErrLookupFailed
		}
		if item.Content == nil || item.Content.Secret == "" {
			out[ref] = fieldValue{Missing: true}
			continue
		}
		out[ref] = fieldValue{Secret: item.Content.Secret}
	}
	return out, nil
}

func missingReference(kind onepassword.ResolveReferenceErrorTypes) bool {
	switch kind {
	case onepassword.ResolveReferenceErrorTypeVariantFieldNotFound,
		onepassword.ResolveReferenceErrorTypeVariantItemNotFound,
		onepassword.ResolveReferenceErrorTypeVariantVaultNotFound,
		onepassword.ResolveReferenceErrorTypeVariantNoMatchingSections:
		return true
	default:
		return false
	}
}
