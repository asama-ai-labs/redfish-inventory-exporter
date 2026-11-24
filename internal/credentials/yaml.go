// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Asama Inc.

package credentials

import (
	"errors"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// ValidateDocument rejects a service-account token and unknown credential keys.
// It does not check provider values. A nil or empty node is valid.
func ValidateDocument(node *yaml.Node) error {
	node = documentRoot(node)
	if node == nil || node.Kind == 0 {
		return nil
	}
	if err := rejectTokenKeys(node); err != nil {
		return err
	}
	return rejectUnknownCredentialKeys(node)
}

func documentRoot(node *yaml.Node) *yaml.Node {
	if node == nil {
		return nil
	}
	if node.Kind == yaml.DocumentNode {
		if len(node.Content) == 0 {
			return nil
		}
		return node.Content[0]
	}
	return node
}

func rejectTokenKeys(node *yaml.Node) error {
	if node == nil {
		return nil
	}
	switch node.Kind {
	case yaml.MappingNode:
		for i := 0; i+1 < len(node.Content); i += 2 {
			key := node.Content[i].Value
			if strings.Contains(strings.ToLower(key), "token") {
				return errors.New("credentials configuration must not include a service account token; set OP_SERVICE_ACCOUNT_TOKEN in the environment")
			}
			if err := rejectTokenKeys(node.Content[i+1]); err != nil {
				return err
			}
		}
	case yaml.SequenceNode, yaml.DocumentNode:
		for _, child := range node.Content {
			if err := rejectTokenKeys(child); err != nil {
				return err
			}
		}
	}
	return nil
}

func rejectUnknownCredentialKeys(creds *yaml.Node) error {
	if creds.Kind != yaml.MappingNode {
		return errors.New("credentials configuration is invalid")
	}
	allowed := map[string]struct{}{"provider": {}, "timeout": {}}
	for _, spec := range allSpecs() {
		if spec.Selectable {
			allowed[spec.Name] = struct{}{}
		}
	}
	for _, key := range Keys(creds) {
		if _, ok := allowed[key]; !ok {
			return fmt.Errorf("unknown credentials field %q", key)
		}
	}
	for _, spec := range allSpecs() {
		child, ok := Child(creds, spec.Name)
		if !ok || spec.CheckKeys == nil {
			continue
		}
		if err := spec.CheckKeys(child); err != nil {
			return err
		}
	}
	return nil
}

// Child returns the value node for key in a mapping.
func Child(node *yaml.Node, key string) (*yaml.Node, bool) {
	if node == nil || node.Kind != yaml.MappingNode {
		return nil, false
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			return node.Content[i+1], true
		}
	}
	return nil, false
}

// Keys returns the keys of a mapping node.
func Keys(node *yaml.Node) []string {
	if node == nil {
		return nil
	}
	keys := make([]string, 0, len(node.Content)/2)
	for i := 0; i+1 < len(node.Content); i += 2 {
		keys = append(keys, node.Content[i].Value)
	}
	return keys
}

// RejectKeys reports the first key that is not in allowed.
func RejectKeys(node *yaml.Node, allowed ...string) error {
	if node == nil {
		return nil
	}
	if node.Kind != yaml.MappingNode {
		return errors.New("credentials configuration is invalid")
	}
	set := map[string]struct{}{}
	for _, key := range allowed {
		set[key] = struct{}{}
	}
	for _, key := range Keys(node) {
		if _, ok := set[key]; !ok {
			return fmt.Errorf("unknown credentials field %q", key)
		}
	}
	return nil
}
