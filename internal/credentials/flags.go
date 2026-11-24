// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Asama Inc.

package credentials

import (
	"errors"
	"strings"
	"time"

	kingpin "gopkg.in/alecthomas/kingpin.v2"
)

// Flags holds the core CLI overrides and each provider's overlay.
type Flags struct {
	Provider providerFlag
	Timeout  optionalDuration
	overlays []namedOverlay
}

type namedOverlay struct {
	name    string
	overlay Overlay
}

type providerFlag struct {
	value string
	set   bool
}

func (p *providerFlag) Set(v string) error {
	name := strings.ToLower(strings.TrimSpace(v))
	spec, ok := specByName(name)
	if !ok || !spec.Selectable || name == "" {
		return errors.New("invalid credentials provider")
	}
	p.value = name
	p.set = true
	return nil
}

func (p *providerFlag) String() string { return p.value }
func (p *providerFlag) IsSet() bool    { return p.set }

type optionalString struct {
	value string
	set   bool
}

func (o *optionalString) Set(v string) error {
	o.value = v
	o.set = true
	return nil
}

func (o *optionalString) String() string { return o.value }
func (o *optionalString) IsSet() bool    { return o.set }

type optionalDuration struct {
	value time.Duration
	set   bool
}

func (o *optionalDuration) Set(v string) error {
	parsed, err := time.ParseDuration(strings.TrimSpace(v))
	if err != nil || parsed <= 0 {
		return errors.New("credentials timeout must be a positive duration")
	}
	o.value = parsed
	o.set = true
	return nil
}

func (o *optionalDuration) String() string {
	if !o.set {
		return ""
	}
	return o.value.String()
}

func (o *optionalDuration) IsSet() bool { return o.set }

// AddFlags registers credentials.provider, credentials.timeout, and each
// provider's own flags. The service-account token is not a flag.
func AddFlags(app *kingpin.Application) *Flags {
	flags := &Flags{}
	app.Flag("credentials.provider", "Credential provider: config (YAML hosts) or onepassword. The 1Password service-account token is read only from OP_SERVICE_ACCOUNT_TOKEN.").SetValue(&flags.Provider)
	app.Flag("credentials.timeout", "Timeout for a 1Password credential lookup.").SetValue(&flags.Timeout)
	for _, spec := range allSpecs() {
		if spec.RegisterFlags == nil {
			continue
		}
		flags.overlays = append(flags.overlays, namedOverlay{
			name:    spec.Name,
			overlay: spec.RegisterFlags(app),
		})
	}
	return flags
}
