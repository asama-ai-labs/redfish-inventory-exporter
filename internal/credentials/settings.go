// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Asama Inc.

package credentials

import (
	"errors"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const defaultTimeout = 5 * time.Second

// Selection is the provider choice after CLI, YAML, and defaults are merged.
type Selection struct {
	Name    string
	Timeout time.Duration
	values  map[string]any
}

// Settings returns the merged settings value for a provider.
func (s Selection) Settings(name string) any {
	if s.values == nil {
		return nil
	}
	return s.values[name]
}

// Active returns the provider to open. An external mode can replace the
// selected provider after conflict checks pass.
func (s Selection) Active(ext External) (string, error) {
	spec, ok := specByName(s.Name)
	if !ok {
		return "", errors.New("invalid credentials provider")
	}
	if spec.Conflict != nil {
		if err := spec.Conflict(ext); err != nil {
			return "", err
		}
	}
	for _, candidate := range allSpecs() {
		if candidate.Activate != nil && candidate.Activate(ext) {
			return candidate.Name, nil
		}
	}
	return s.Name, nil
}

// Resolve merges built-in defaults, the credentials YAML node, then CLI flags.
func Resolve(node *yaml.Node, flags *Flags) (Selection, error) {
	sel := defaultSelection()
	node = documentRoot(node)
	if node != nil && node.Kind != 0 {
		cliProvider := flags != nil && flags.Provider.set
		if err := applyYAML(&sel, node, cliProvider); err != nil {
			return Selection{}, err
		}
	}
	if flags != nil {
		if flags.Provider.set {
			sel.Name = flags.Provider.value
		}
		if flags.Timeout.set {
			sel.Timeout = flags.Timeout.value
		}
		for _, overlay := range flags.overlays {
			if overlay.overlay == nil {
				continue
			}
			overlay.overlay.Apply(sel.values[overlay.name])
		}
	}
	if err := sel.validate(); err != nil {
		return Selection{}, err
	}
	return sel, nil
}

func defaultSelection() Selection {
	sel := Selection{
		Timeout: defaultTimeout,
		values:  map[string]any{},
	}
	for _, spec := range allSpecs() {
		if spec.Defaults != nil {
			sel.values[spec.Name] = spec.Defaults()
		}
		if spec.Default {
			sel.Name = spec.Name
		}
	}
	return sel
}

func applyYAML(sel *Selection, node *yaml.Node, cliProvider bool) error {
	if node.Kind != yaml.MappingNode {
		return errors.New("credentials configuration is invalid")
	}
	if provider, ok, err := scalar(node, "provider"); err != nil {
		return err
	} else if ok {
		name, nerr := selectableName(provider)
		if nerr != nil {
			if !cliProvider {
				return nerr
			}
		} else {
			sel.Name = name
		}
	}
	if raw, ok, err := scalar(node, "timeout"); err != nil {
		return err
	} else if ok {
		parsed, err := time.ParseDuration(strings.TrimSpace(raw))
		if err != nil {
			return errors.New("credentials timeout must be a duration string")
		}
		if parsed <= 0 {
			return errors.New("credentials timeout must be positive")
		}
		sel.Timeout = parsed
	}
	for _, spec := range allSpecs() {
		child, ok := Child(node, spec.Name)
		if !ok || spec.MergeYAML == nil {
			continue
		}
		if err := spec.MergeYAML(sel.values[spec.Name], child); err != nil {
			return err
		}
	}
	return nil
}

func (s Selection) validate() error {
	if s.Timeout <= 0 {
		return errors.New("credentials timeout must be positive")
	}
	spec, ok := specByName(s.Name)
	if !ok || !spec.Selectable {
		return errors.New("invalid credentials provider")
	}
	if spec.Validate == nil {
		return nil
	}
	return spec.Validate(s.values[spec.Name], s.Timeout)
}

func selectableName(value string) (string, error) {
	name := strings.ToLower(strings.TrimSpace(value))
	spec, ok := specByName(name)
	if !ok || !spec.Selectable || name == "" {
		return "", errors.New("invalid credentials provider")
	}
	return name, nil
}

func scalar(node *yaml.Node, key string) (string, bool, error) {
	child, ok := Child(node, key)
	if !ok {
		return "", false, nil
	}
	if child.Kind != yaml.ScalarNode {
		return "", false, errors.New("credentials configuration is invalid")
	}
	return child.Value, true, nil
}
