// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Asama Inc.

package onepassword

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/1password/onepassword-sdk-go"
	"github.com/asama-ai-labs/redfish-inventory-exporter/internal/credentials"
)

func TestResolveUsernameAndPassword(t *testing.T) {
	fake := &fakeResolver{values: map[string]fieldValue{
		"op://bmc/10.1.2.3/username": {Secret: "op-user"},
		"op://bmc/10.1.2.3/password": {Secret: "op-pass"},
	}}
	p := testProvider(t, fake, &Settings{
		Vault:        "bmc",
		ItemTemplate: "{{ip}}",
		Fields:       Fields{Username: "username", Password: "password"},
		Timeout:      2 * time.Second,
	})
	got, err := p.Resolve(context.Background(), credentials.Subject{Target: "https://10.1.2.3:443"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Username != "op-user" || got.Password != "op-pass" {
		t.Fatalf("got %+v", got)
	}
	if strings.Join(fake.refs, ",") != "op://bmc/10.1.2.3/username,op://bmc/10.1.2.3/password" {
		t.Fatalf("refs %v", fake.refs)
	}
	if fake.hasDeadline || fake.sawDone {
		t.Fatal("lookup context was cancellable")
	}
}

func TestTimeoutDoesNotCancelSDKCall(t *testing.T) {
	release := make(chan struct{})
	entered := make(chan struct{})
	fake := &fakeResolver{entered: entered, release: release}
	p := testProvider(t, fake, &Settings{
		Vault:        "bmc",
		ItemTemplate: "{{ip}}",
		Fields:       Fields{Username: "username", Password: "password"},
		Timeout:      40 * time.Millisecond,
	})
	done := make(chan error, 1)
	go func() {
		_, err := p.Resolve(context.Background(), credentials.Subject{Target: "10.1.2.3"})
		done <- err
	}()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("resolver was not called")
	}
	if fake.hasDeadline || fake.sawDone {
		t.Fatal("sdk context can be cancelled")
	}
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected timeout")
		}
	case <-time.After(time.Second):
		t.Fatal("lookup did not return on timeout")
	}
	close(release)
}

func TestIPv6TemplateAndMissingField(t *testing.T) {
	fake := &fakeResolver{values: map[string]fieldValue{
		"op://bmc/bmc-2001:db8::1/user": {Secret: "op-user"},
		"op://bmc/bmc-2001:db8::1/pass": {Missing: true},
	}}
	p := testProvider(t, fake, &Settings{
		Vault:        "bmc",
		ItemTemplate: "bmc-{{ip}}",
		Fields:       Fields{Username: "user", Password: "pass"},
		Timeout:      time.Second,
	})
	_, err := p.Resolve(context.Background(), credentials.Subject{Target: "[2001:0db8::1]:443"})
	var missing *MissingFieldError
	if !errors.As(err, &missing) || missing.Field != "password" {
		t.Fatalf("err %v", err)
	}
	if missing.Vault != "bmc" || missing.Item != "bmc-2001:db8::1" {
		t.Fatalf("not-found error missing vault/item context: %+v", missing)
	}
	if strings.Contains(err.Error(), "op://") || strings.Contains(err.Error(), "2001:") {
		t.Fatalf("reference leaked: %v", err)
	}
	if fake.calls != 1 {
		t.Fatalf("missing field was retried %d times", fake.calls)
	}
}

func TestSDKFailureIsRedacted(t *testing.T) {
	fake := &fakeResolver{err: errors.New("boom op://bmc/10.0.0.1/password secret=hunter2")}
	p := testProvider(t, fake, &Settings{
		Vault:        "bmc",
		ItemTemplate: "{{ip}}",
		Fields:       Fields{Username: "username", Password: "password"},
		Timeout:      time.Second,
	})
	_, err := p.Resolve(context.Background(), credentials.Subject{Target: "10.0.0.1"})
	if !errors.Is(err, ErrLookupFailed) {
		t.Fatalf("err %v", err)
	}
	if strings.Contains(err.Error(), "op://") || strings.Contains(err.Error(), "hunter2") || strings.Contains(err.Error(), "boom") {
		t.Fatalf("sdk error leaked: %v", err)
	}
	if fake.calls != lookupAttempts {
		t.Fatalf("calls = %d, want %d", fake.calls, lookupAttempts)
	}
}

func TestResolveCachesAndRetries(t *testing.T) {
	fake := &fakeResolver{values: map[string]fieldValue{
		"op://bmc/10.0.0.1/username": {Secret: "op-user"},
		"op://bmc/10.0.0.1/password": {Secret: "op-pass"},
	}}
	p := testProvider(t, fake, &Settings{
		Vault:        "bmc",
		ItemTemplate: "{{ip}}",
		Fields:       Fields{Username: "username", Password: "password"},
		Timeout:      time.Second,
	})
	subject := credentials.Subject{Target: "10.0.0.1"}
	if _, err := p.Resolve(context.Background(), subject); err != nil {
		t.Fatal(err)
	}
	fake.err = errors.New("module is closed")
	got, err := p.Resolve(context.Background(), subject)
	if err != nil || got.Password != "op-pass" || fake.calls != 1 {
		t.Fatalf("cache got=%+v err=%v calls=%d", got, err, fake.calls)
	}
	p.mu.Lock()
	for key, ent := range p.cache {
		ent.expires = time.Now().Add(-time.Second)
		p.cache[key] = ent
	}
	p.mu.Unlock()
	got, err = p.Resolve(context.Background(), subject)
	if err != nil || got.Username != "op-user" || got.Password != "op-pass" {
		t.Fatalf("stale got=%+v err=%v", got, err)
	}
	if fake.calls != 4 {
		t.Fatalf("calls = %d, want 4", fake.calls)
	}
}

func TestConfiguredDefaultItem(t *testing.T) {
	fake := &fakeResolver{values: map[string]fieldValue{
		"op://bmc/iDRAC admin credentials/username": {Secret: "shared-user"},
		"op://bmc/iDRAC admin credentials/password": {Secret: "shared-pass"},
	}}
	p := testProvider(t, fake, &Settings{
		Vault:        "bmc",
		ItemTemplate: "{{ip}}",
		DefaultItem:  "iDRAC admin credentials",
		Fields:       Fields{Username: "username", Password: "password"},
		Timeout:      time.Second,
	})
	got, err := p.ResolveDefault(context.Background())
	if err != nil || got.Username != "shared-user" || got.Password != "shared-pass" {
		t.Fatalf("got %+v err %v", got, err)
	}
	if strings.Join(fake.refs, ",") != "op://bmc/iDRAC admin credentials/username,op://bmc/iDRAC admin credentials/password" {
		t.Fatalf("refs %v", fake.refs)
	}
	if err := (&Settings{
		Vault: "bmc", ItemTemplate: "{{ip}}", DefaultItem: "bad/title",
		Fields: Fields{Username: "username", Password: "password"}, Timeout: time.Second,
	}).validate(); err == nil {
		t.Fatal("slash in default item was accepted")
	}
	if err := (&Settings{
		Vault: "bmc", ItemTemplate: "{{ip}}", DefaultItem: "shared:prod",
		Fields: Fields{Username: "username", Password: "password"}, Timeout: time.Second,
	}).validate(); err == nil {
		t.Fatal("colon in default item was accepted")
	}
}

func TestAdapterKeepsDeviceItemMiss(t *testing.T) {
	cold := testProvider(t, &fakeResolver{}, &Settings{
		Vault:        "bmc",
		ItemTemplate: "{{ip}}",
		Fields:       Fields{Username: "username", Password: "password"},
		Timeout:      time.Second,
	})
	_, err := (adapter{inner: cold, lookup: func(string) credentials.Login {
		return credentials.Login{}
	}}).Credentials(context.Background(), credentials.Subject{Target: "10.0.0.5"})
	var missing *MissingFieldError
	if !errors.As(err, &missing) || missing.Item != "10.0.0.5" || missing.Vault != "bmc" {
		t.Fatalf("err %v", err)
	}
}

func TestRejectsDNSWithoutLookup(t *testing.T) {
	fake := &fakeResolver{}
	p := testProvider(t, fake, &Settings{
		Vault:        "bmc",
		ItemTemplate: "{{ip}}",
		Fields:       Fields{Username: "username", Password: "password"},
		Timeout:      time.Second,
	})
	_, err := p.Resolve(context.Background(), credentials.Subject{Target: "bmc.example:443"})
	if !errors.Is(err, credentials.ErrInvalidTarget) {
		t.Fatalf("err %v", err)
	}
	if fake.refs != nil {
		t.Fatal("resolver was called")
	}
}

func TestNewSDKRequiresToken(t *testing.T) {
	settings := &Settings{
		Vault:        "bmc",
		ItemTemplate: "{{ip}}",
		Fields:       Fields{Username: "username", Password: "password"},
		Timeout:      time.Second,
	}
	for _, token := range []string{"", "   "} {
		t.Setenv("OP_SERVICE_ACCOUNT_TOKEN", token)
		_, err := NewSDK(context.Background(), settings)
		if err == nil {
			t.Fatal("expected missing token")
		}
		if strings.Contains(err.Error(), "ops_") {
			t.Fatalf("token leaked: %v", err)
		}
	}
}

func TestAdapterFallsBack(t *testing.T) {
	fake := &fakeResolver{values: map[string]fieldValue{
		"op://bmc/default/username": {Secret: "default-user"},
		"op://bmc/default/password": {Secret: "default-pass"},
	}}
	inner := testProvider(t, fake, &Settings{
		Vault:        "bmc",
		ItemTemplate: "{{ip}}",
		Fields:       Fields{Username: "username", Password: "password"},
		Timeout:      time.Second,
	})
	got, err := (adapter{inner: inner}).Credentials(context.Background(), credentials.Subject{Target: "10.0.0.5"})
	if err != nil || got.Username != "default-user" || got.Password != "default-pass" {
		t.Fatalf("default item got %+v err %v", got, err)
	}

	cold := testProvider(t, &fakeResolver{}, &Settings{
		Vault:        "bmc",
		ItemTemplate: "{{ip}}",
		Fields:       Fields{Username: "username", Password: "password"},
		Timeout:      time.Second,
	})
	got, err = (adapter{inner: cold, lookup: func(string) credentials.Login {
		return credentials.Login{Username: "yaml-user", Password: "yaml-pass"}
	}}).Credentials(context.Background(), credentials.Subject{Target: "10.0.0.5"})
	if err != nil || got.Username != "yaml-user" || got.Password != "yaml-pass" {
		t.Fatalf("yaml got %+v err %v", got, err)
	}
}

func TestAdapterDoesNotFallBack(t *testing.T) {
	fake := &fakeResolver{values: map[string]fieldValue{
		"op://bmc/10.0.0.5/username": {Secret: "op-user"},
		"op://bmc/10.0.0.5/password": {Missing: true},
	}}
	inner := testProvider(t, fake, &Settings{
		Vault:        "bmc",
		ItemTemplate: "{{ip}}",
		Fields:       Fields{Username: "username", Password: "password"},
		Timeout:      time.Second,
	})
	got, err := (adapter{inner: inner}).Credentials(context.Background(), credentials.Subject{Target: "10.0.0.5"})
	if err == nil || got.Username != "" || got.Password != "" {
		t.Fatalf("got %+v err %v", got, err)
	}
	status, msg := credentials.PublicFailure(err)
	if status != 500 || msg != "failed to resolve 1Password credentials" {
		t.Fatalf("status %d message %q", status, msg)
	}
}

func TestHostnameAndSerialTemplate(t *testing.T) {
	fake := &fakeResolver{values: map[string]fieldValue{
		"op://bmc/node-a-SN1/username": {Secret: "op-user"},
		"op://bmc/node-a-SN1/password": {Secret: "op-pass"},
	}}
	inner := testProvider(t, fake, &Settings{
		Vault:        "bmc",
		ItemTemplate: "{{hostname}}-{{serialnumber}}",
		Fields:       Fields{Username: "username", Password: "password"},
		Timeout:      time.Second,
	})
	got, err := (adapter{inner: inner}).Credentials(context.Background(), credentials.Subject{
		Target: "10.1.2.3", Hostname: "node-a", SerialNumber: "SN1",
	})
	if err != nil || got.Username != "op-user" || got.Password != "op-pass" {
		t.Fatalf("got %+v err %v refs %v", got, err, fake.refs)
	}

	fake.refs = nil
	_, err = (adapter{inner: inner}).Credentials(context.Background(), credentials.Subject{
		Target: "10.1.2.3", SerialNumber: "SN1",
	})
	status, msg := credentials.PublicFailure(err)
	if status != 400 || msg != "hostname is required by the item template" || fake.refs != nil {
		t.Fatalf("status %d message %q refs %v err %v", status, msg, fake.refs, err)
	}

	_, err = (adapter{inner: inner}).Credentials(context.Background(), credentials.Subject{
		Target: "10.1.2.3", Hostname: "node a", SerialNumber: "SN1",
	})
	status, msg = credentials.PublicFailure(err)
	if status != 400 || msg != "hostname is not a valid item name" || strings.Contains(msg, "node") {
		t.Fatalf("status %d message %q", status, msg)
	}
}

func TestDefaultItemReplacesEveryPlaceholder(t *testing.T) {
	fake := &fakeResolver{values: map[string]fieldValue{
		"op://bmc/pdu-default-default/username": {Secret: "default-user"},
		"op://bmc/pdu-default-default/password": {Secret: "default-pass"},
	}}
	inner := testProvider(t, fake, &Settings{
		Vault:        "bmc",
		ItemTemplate: "pdu-{{hostname}}-{{serialnumber}}",
		Fields:       Fields{Username: "username", Password: "password"},
		Timeout:      time.Second,
	})
	got, err := (adapter{inner: inner}).Credentials(context.Background(), credentials.Subject{
		Target: "10.0.0.5", Hostname: "missing-item", SerialNumber: "SN9",
	})
	if err != nil || got.Username != "default-user" {
		t.Fatalf("got %+v err %v refs %v", got, err, fake.refs)
	}
}

func TestMissingReferenceClassification(t *testing.T) {
	if !missingReference(onepassword.ResolveReferenceErrorTypeVariantFieldNotFound) ||
		!missingReference(onepassword.ResolveReferenceErrorTypeVariantItemNotFound) ||
		!missingReference(onepassword.ResolveReferenceErrorTypeVariantVaultNotFound) {
		t.Fatal("expected not-found variants to be missing fields")
	}
	if missingReference(onepassword.ResolveReferenceErrorTypeVariantParsing) ||
		missingReference(onepassword.ResolveReferenceErrorTypeVariantOther) {
		t.Fatal("expected parse and other errors to fail the lookup")
	}
}

type fakeResolver struct {
	refs        []string
	values      map[string]fieldValue
	err         error
	calls       int
	hasDeadline bool
	sawDone     bool
	entered     chan struct{}
	release     chan struct{}
}

func (f *fakeResolver) ResolveAll(ctx context.Context, refs []string) (map[string]fieldValue, error) {
	f.calls++
	f.refs = append([]string(nil), refs...)
	if _, ok := ctx.Deadline(); ok {
		f.hasDeadline = true
	}
	f.sawDone = ctx.Err() != nil
	if f.entered != nil {
		close(f.entered)
	}
	if f.release != nil {
		<-f.release
	}
	if f.err != nil {
		return nil, f.err
	}
	return f.values, nil
}

func testProvider(t *testing.T, backend resolver, settings *Settings) *secretProvider {
	t.Helper()
	p, err := newProvider(settings, backend)
	if err != nil {
		t.Fatal(err)
	}
	return p
}
