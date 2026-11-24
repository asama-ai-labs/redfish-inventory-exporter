// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Asama Inc.

package onepassword

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/asama-ai-labs/redfish-inventory-exporter/internal/credentials"
)

func init() {
	credentials.Register(credentials.Spec{
		Name:          credentials.NameOnePassword,
		Selectable:    true,
		Defaults:      func() any { return defaults() },
		MergeYAML:     mergeYAML,
		CheckKeys:     checkKeys,
		RegisterFlags: registerFlags,
		Validate: func(settings any, timeout time.Duration) error {
			dst, _ := settings.(*Settings)
			if dst == nil {
				return errors.New("1password vault is invalid")
			}
			dst.Timeout = timeout
			return dst.validate()
		},
		Startup: func(any, credentials.Deps) error {
			return requireToken()
		},
		Conflict: func(ext credentials.External) error {
			if ext.Enabled(credentials.NameHashicorp) {
				return errors.New("credentials provider onepassword cannot be combined with HashiCorp Vault")
			}
			return nil
		},
		Open: func(ctx context.Context, settings any, deps credentials.Deps) (credentials.Provider, error) {
			dst, _ := settings.(*Settings)
			inner, err := NewSDK(ctx, dst)
			if err != nil {
				return nil, err
			}
			return adapter{inner: inner, lookup: deps.Lookup}, nil
		},
	})
}

type adapter struct {
	inner  *secretProvider
	lookup func(target string) credentials.Login
}

func (a adapter) Credentials(ctx context.Context, subject credentials.Subject) (credentials.Login, error) {
	login, err := credentials.PickLogin(ctx,
		func(ctx context.Context) (credentials.Login, error) {
			return a.fromItem(ctx, subject)
		},
		func(ctx context.Context) (credentials.Login, error) {
			return a.fromDefaultItem(ctx)
		},
		func(context.Context) (credentials.Login, error) {
			return a.fromYAML(subject.Target)
		},
	)
	if err != nil {
		return credentials.Login{}, redact(err)
	}
	return login, nil
}

func (a adapter) fromItem(ctx context.Context, subject credentials.Subject) (credentials.Login, error) {
	got, err := a.inner.Resolve(ctx, subject)
	if err != nil {
		return credentials.Login{}, err
	}
	return credentials.Login{Username: got.Username, Password: got.Password}, nil
}

func (a adapter) fromDefaultItem(ctx context.Context) (credentials.Login, error) {
	got, err := a.inner.ResolveDefault(ctx)
	if err != nil {
		return credentials.Login{}, err
	}
	return credentials.Login{Username: got.Username, Password: got.Password}, nil
}

func (a adapter) fromYAML(target string) (credentials.Login, error) {
	if a.lookup == nil {
		return credentials.Login{}, credentials.ErrNotFound
	}
	got := a.lookup(target)
	if got.Password == "" {
		return credentials.Login{}, credentials.ErrNotFound
	}
	return got, nil
}

func redact(err error) error {
	if errors.Is(err, credentials.ErrInvalidTarget) {
		return &credentials.Failure{Status: http.StatusBadRequest, Public: "target must be an IP address", Err: err}
	}
	var field *credentials.FieldError
	if errors.As(err, &field) {
		return &credentials.Failure{Status: http.StatusBadRequest, Public: field.Error(), Err: err}
	}
	return &credentials.Failure{Status: http.StatusInternalServerError, Public: "failed to resolve 1Password credentials", Err: err}
}
