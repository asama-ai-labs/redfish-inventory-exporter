// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Asama Inc.

// Package credentials selects a Redfish login provider.
//
// A provider lives in its own package and registers a Spec from init.
// Core code knows the provider name, timeout, and Provider interface.
// It does not know a provider's YAML fields, flags, or client.
package credentials

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	kingpin "gopkg.in/alecthomas/kingpin.v2"
	"gopkg.in/yaml.v3"
)

const (
	// NameConfig reads usernames and passwords from the collector config file.
	NameConfig = "config"
	// NameOnePassword reads them from a 1Password item named by target IP.
	NameOnePassword = "onepassword"
	// NameHashicorp reads them from HashiCorp Vault, with the config file as fallback.
	// It is selected by --vault.type, not by credentials.provider.
	NameHashicorp = "hashicorp"
)

// Login is a Redfish username and password. Providers return it only in memory.
type Login struct {
	Username string
	Password string
}

// Subject identifies one scrape. Target is the BMC address. Hostname and
// SerialNumber come from the /redfish query and are used only when the item
// template contains {{hostname}} or {{serialnumber}}.
type Subject struct {
	Target       string
	Hostname     string
	SerialNumber string
}

// Provider loads the login for one scrape target.
type Provider interface {
	Credentials(ctx context.Context, subject Subject) (Login, error)
}

// Deps is the startup context passed to every provider factory.
// Providers ignore the fields they do not use.
type Deps struct {
	Lookup func(target string) Login
}

// External is process state that can activate or conflict with a provider.
// Modes are provider names, for example "hashicorp" when --vault.type is set.
type External struct {
	Modes map[string]bool
}

// Enabled reports whether a mode was set by the caller.
func (e External) Enabled(name string) bool {
	return e.Modes[name]
}

// Overlay applies CLI values onto a provider's settings.
type Overlay interface {
	Apply(settings any)
}

// Spec is everything the core needs to load one provider.
type Spec struct {
	Name string
	// Selectable providers may be named by credentials.provider.
	Selectable bool
	// Default is the provider used when credentials.provider is omitted.
	Default bool
	// Defaults returns the provider's settings value, usually a pointer.
	Defaults func() any
	// MergeYAML copies a credentials.<name> mapping onto settings.
	MergeYAML func(settings any, node *yaml.Node) error
	// CheckKeys rejects unknown keys in that mapping.
	CheckKeys func(node *yaml.Node) error
	// RegisterFlags adds the provider's CLI overrides.
	RegisterFlags func(app *kingpin.Application) Overlay
	// Validate checks the merged settings. Timeout is the effective credentials.timeout.
	Validate func(settings any, timeout time.Duration) error
	// Startup preflights this provider before Open builds it.
	Startup func(settings any, deps Deps) error
	// Open builds the provider. settings is the value returned by Defaults.
	Open func(ctx context.Context, settings any, deps Deps) (Provider, error)
	// Conflict fails when this provider cannot run with an external mode.
	Conflict func(ext External) error
	// Activate selects this provider when an external mode is set.
	Activate func(ext External) bool
}

// Failure is a redacted provider error safe to return to a client.
type Failure struct {
	Status int
	Public string
	Err    error
}

func (f *Failure) Error() string {
	if f == nil || f.Public == "" {
		return "failed to resolve credentials"
	}
	return f.Public
}

func (f *Failure) Unwrap() error {
	if f == nil {
		return nil
	}
	return f.Err
}

// PublicFailure returns an HTTP status and a client-safe message.
func PublicFailure(err error) (int, string) {
	var failure *Failure
	if errors.As(err, &failure) && failure.Status != 0 {
		return failure.Status, failure.Error()
	}
	if errors.Is(err, ErrInvalidTarget) {
		return http.StatusBadRequest, "target must be an IP address"
	}
	return http.StatusInternalServerError, "failed to resolve credentials"
}

var (
	mu    sync.RWMutex
	specs = map[string]Spec{}
)

// Register adds a provider. Registration happens from init.
// Registering the same name twice panics.
func Register(spec Spec) {
	mu.Lock()
	defer mu.Unlock()
	if spec.Name == "" || spec.Open == nil {
		panic("credential provider spec is incomplete")
	}
	if _, exists := specs[spec.Name]; exists {
		panic("credential provider already registered: " + spec.Name)
	}
	specs[spec.Name] = spec
}

// Known reports whether name was registered.
func Known(name string) bool {
	mu.RLock()
	defer mu.RUnlock()
	_, ok := specs[name]
	return ok
}

func specByName(name string) (Spec, bool) {
	mu.RLock()
	defer mu.RUnlock()
	spec, ok := specs[name]
	return spec, ok
}

func allSpecs() []Spec {
	mu.RLock()
	defer mu.RUnlock()
	out := make([]Spec, 0, len(specs))
	for _, spec := range specs {
		out = append(out, spec)
	}
	return out
}

// Open builds the named provider from a resolved selection.
func Open(ctx context.Context, name string, sel Selection, deps Deps) (Provider, error) {
	spec, ok := specByName(name)
	if !ok {
		return nil, fmt.Errorf("unknown credential provider %q", name)
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if spec.Startup != nil {
		if err := spec.Startup(sel.values[name], deps); err != nil {
			return nil, err
		}
	}
	return spec.Open(ctx, sel.values[name], deps)
}
