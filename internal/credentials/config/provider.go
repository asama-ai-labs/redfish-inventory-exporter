// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Asama Inc.

// Package config is the default credential provider. It reads the
// username and password from the collector config file.
package config

import (
	"context"
	"errors"

	"github.com/asama-ai-labs/redfish-inventory-exporter/internal/credentials"
)

func init() {
	credentials.Register(credentials.Spec{
		Name:       credentials.NameConfig,
		Selectable: true,
		Default:    true,
		Startup: func(_ any, deps credentials.Deps) error {
			if deps.Lookup == nil {
				return errors.New("config credential lookup is required")
			}
			return nil
		},
		Open: func(_ context.Context, _ any, deps credentials.Deps) (credentials.Provider, error) {
			return lookupProvider{lookup: deps.Lookup}, nil
		},
	})
}

type lookupProvider struct {
	lookup func(target string) credentials.Login
}

func (p lookupProvider) Credentials(_ context.Context, subject credentials.Subject) (credentials.Login, error) {
	return p.lookup(subject.Target), nil
}
