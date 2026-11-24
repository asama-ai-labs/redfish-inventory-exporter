// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Asama Inc.

package hashicorp

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/asama-ai-labs/redfish-inventory-exporter/internal/credentials"
	"github.com/asama-ai/redfish_exporter/vault"
)

func TestFallsBackToConfigWhenAllowed(t *testing.T) {
	p := &provider{
		allowFallback: true,
		vault:         &fakeVault{err: errors.New("vault down op://secret")},
		fallback: func(string) credentials.Login {
			return credentials.Login{Username: "yaml-user", Password: "yaml-pass"}
		},
	}
	got, err := p.Credentials(context.Background(), credentials.Subject{Target: "10.0.0.8"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Username != "yaml-user" || got.Password != "yaml-pass" {
		t.Fatalf("fallback %+v", got)
	}

	p.vault = &fakeVault{creds: &vault.RedfishCreds{Username: "vault-user", Password: "vault-pass"}}
	got, err = p.Credentials(context.Background(), credentials.Subject{Target: "10.0.0.8"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Username != "vault-user" || got.Password != "vault-pass" {
		t.Fatalf("vault credentials %+v", got)
	}

	p.vault = &fakeVault{byTarget: map[string]*vault.RedfishCreds{
		"default": {Username: "vault-default", Password: "vault-default-pass"},
	}}
	p.fallback = func(string) credentials.Login {
		return credentials.Login{Username: "yaml-user", Password: "yaml-pass"}
	}
	got, err = p.Credentials(context.Background(), credentials.Subject{Target: "10.0.0.8"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Username != "vault-default" || got.Password != "vault-default-pass" {
		t.Fatalf("vault default %+v", got)
	}

	p.vault = &fakeVault{err: errors.New("vault down")}
	p.fallback = func(string) credentials.Login {
		return credentials.Login{Username: "yaml-user"}
	}
	got, err = p.Credentials(context.Background(), credentials.Subject{Target: "10.0.0.8"})
	if err != nil || got.Username != "yaml-user" {
		t.Fatalf("username fallback %+v err %v", got, err)
	}
}

type fakeVault struct {
	creds    *vault.RedfishCreds
	err      error
	byTarget map[string]*vault.RedfishCreds
}

func (f *fakeVault) GetCredentials(_ context.Context, target string) (*vault.RedfishCreds, error) {
	if f.byTarget != nil {
		creds, ok := f.byTarget[target]
		if !ok {
			return nil, errors.New("missing")
		}
		return creds, nil
	}
	return f.creds, f.err
}

func (f *fakeVault) Close() error { return nil }

func TestVaultFailureFailsTheScrapeByDefault(t *testing.T) {
	yaml := func(string) credentials.Login { return credentials.Login{Username: "yaml-user", Password: "yaml-pass"} }
	for name, v := range map[string]*fakeVault{
		"unreachable":    {err: errors.New("connection refused")},
		"no secret":      {byTarget: map[string]*vault.RedfishCreds{}},
		"empty password": {creds: &vault.RedfishCreds{Username: "u"}},
	} {
		t.Run(name, func(t *testing.T) {
			p := &provider{vault: v, fallback: yaml}
			got, err := p.Credentials(context.Background(), credentials.Subject{Target: "10.0.0.8"})
			if err == nil {
				t.Fatalf("expected an error, got login %+v", got)
			}
			if got != (credentials.Login{}) {
				t.Fatalf("the config login leaked into the result: %+v", got)
			}
			status, msg := credentials.PublicFailure(err)
			if status != http.StatusBadGateway || msg == "" {
				t.Fatalf("PublicFailure = %d %q", status, msg)
			}
		})
	}
}

func TestVaultSuccessIsUnaffectedByTheFallbackSetting(t *testing.T) {
	p := &provider{vault: &fakeVault{creds: &vault.RedfishCreds{Username: "v", Password: "pw"}}}
	got, err := p.Credentials(context.Background(), credentials.Subject{Target: "10.0.0.8"})
	if err != nil || got.Username != "v" || got.Password != "pw" {
		t.Fatalf("got %+v err %v", got, err)
	}
}

func TestRejectTokenFlag(t *testing.T) {
	for _, args := range [][]string{
		{"--hashicorp.token=s.abc"},
		{"--port=9611", "--hashicorp.token", "s.abc"},
	} {
		if err := RejectTokenFlag(args); err == nil {
			t.Errorf("RejectTokenFlag(%v) = nil, want an error", args)
		}
	}
	for _, args := range [][]string{
		nil,
		{"--vault.type=hashicorp", "--hashicorp.kv.mount=redfish"},
		{"--", "--hashicorp.token=x"},
	} {
		if err := RejectTokenFlag(args); err != nil {
			t.Errorf("RejectTokenFlag(%v) = %v, want nil", args, err)
		}
	}
}
