// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Asama Inc.

// Package hashicorp loads Redfish logins from HashiCorp Vault.
// A failed or empty lookup fails the scrape. With --hashicorp.config-fallback it falls back to
// the config file instead.
package hashicorp

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	alog "github.com/apex/log"
	"github.com/asama-ai-labs/redfish-inventory-exporter/internal/credentials"
	"github.com/asama-ai/redfish_exporter/vault"
	kingpin "gopkg.in/alecthomas/kingpin.v2"
)

const Name = credentials.NameHashicorp

// tokenFlag is the dependency's flag for the Vault token. It is accepted only from VAULT_TOKEN.
const tokenFlag = "--hashicorp.token"

var configFallback *bool

// AddFlags registers --hashicorp.config-fallback and hides --hashicorp.token, which
// RejectTokenFlag refuses. Call it after vault.AddFlags.
func AddFlags(app *kingpin.Application) {
	configFallback = app.Flag("hashicorp.config-fallback",
		"When Vault has no usable credentials for a target (no secret, bad keys, or Vault unreachable), "+
			"use the config file login instead of failing the scrape.").Bool()
	if f := app.GetFlag("hashicorp.token"); f != nil {
		f.Hidden()
	}
}

// RejectTokenFlag returns an error when args set --hashicorp.token. A token on the command line
// shows up in process listings, so it is read only from the VAULT_TOKEN environment variable.
func RejectTokenFlag(args []string) error {
	for _, a := range args {
		if a == "--" {
			return nil
		}
		if a == tokenFlag || strings.HasPrefix(a, tokenFlag+"=") {
			return fmt.Errorf("%s is not accepted: a token on the command line shows up in process listings; set VAULT_TOKEN instead", tokenFlag)
		}
	}
	return nil
}

func init() {
	credentials.Register(credentials.Spec{
		Name: Name,
		Activate: func(ext credentials.External) bool {
			return ext.Enabled(Name)
		},
		Open: func(ctx context.Context, _ any, deps credentials.Deps) (credentials.Provider, error) {
			manager, err := vault.NewVaultManager(
				&vault.VaultConfig{Type: vault.GetVaultType()},
				alog.WithFields(alog.Fields{"component": "vault"}),
			)
			if err != nil {
				return nil, err
			}
			if err := manager.HealthCheck(ctx); err != nil {
				_ = manager.Close()
				return nil, err
			}
			allow := configFallback != nil && *configFallback
			if allow {
				alog.Warn("--hashicorp.config-fallback is set: a Vault failure uses the config file login")
			}
			return &provider{vault: manager, fallback: deps.Lookup, allowFallback: allow}, nil
		},
	})
}

type source interface {
	GetCredentials(ctx context.Context, target string) (*vault.RedfishCreds, error)
	Close() error
}

type provider struct {
	vault    source
	fallback func(target string) credentials.Login
	// allowFallback uses the config file login when Vault has none. Off, that fails the scrape.
	allowFallback bool
}

func (p *provider) Credentials(ctx context.Context, subject credentials.Subject) (credentials.Login, error) {
	target := subject.Target
	login, err := credentials.PickLogin(ctx,
		func(ctx context.Context) (credentials.Login, error) {
			return p.fromVault(ctx, target)
		},
		func(ctx context.Context) (credentials.Login, error) {
			if target == credentials.DefaultName {
				return credentials.Login{}, credentials.ErrNotFound
			}
			return p.fromVault(ctx, credentials.DefaultName)
		},
	)
	if err != nil {
		if p.allowFallback {
			// PickLogin drops a login that has no password. The config fallback can
			// still carry a username, so it stays outside that chain.
			alog.Warn("Failed to get creds from Vault, using config file instead (--hashicorp.config-fallback)")
			return p.fallbackLogin(target), nil
		}
		alog.Error("Vault has no usable credentials for the target; the scrape fails")
		return credentials.Login{}, &credentials.Failure{
			Status: http.StatusBadGateway,
			Public: "vault has no usable credentials for this target",
			Err:    err,
		}
	}
	return login, nil
}

func (p *provider) fromVault(ctx context.Context, name string) (credentials.Login, error) {
	creds, err := p.vault.GetCredentials(ctx, name)
	if err != nil || creds == nil || strings.TrimSpace(creds.Password) == "" {
		return credentials.Login{}, credentials.ErrNotFound
	}
	return credentials.Login{Username: creds.Username, Password: creds.Password}, nil
}

func (p *provider) Close() error {
	if p == nil || p.vault == nil {
		return nil
	}
	return p.vault.Close()
}

func (p *provider) fallbackLogin(target string) credentials.Login {
	if p.fallback == nil {
		return credentials.Login{}
	}
	return p.fallback(target)
}
