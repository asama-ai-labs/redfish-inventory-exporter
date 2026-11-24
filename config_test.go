// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Asama Inc.

package main

import (
	"strings"
	"testing"
)

func TestHostCredentialsFallback(t *testing.T) {
	cfg, err := ParseConfig([]byte("hosts:\n  default:\n    username: admin\n    password: yaml-secret\n"))
	if err != nil {
		t.Fatal(err)
	}
	got := cfg.HostCredentials("10.0.0.1")
	if got.Username != "admin" || got.Password != "yaml-secret" {
		t.Fatalf("yaml credentials changed: %+v", got)
	}

	cfg.Hosts["10.0.0.2"] = HostConfig{Username: "host-user"}
	got = cfg.HostCredentials("10.0.0.2")
	if got.Username != "admin" || got.Password != "yaml-secret" {
		t.Fatalf("empty host password did not use yaml default: %+v", got)
	}
}

func TestParseConfigRejectsCredentialToken(t *testing.T) {
	const token = "super-secret-token-value"
	_, err := ParseConfig([]byte("credentials:\n  token: " + token + "\n"))
	if err == nil {
		t.Fatal("expected token rejection")
	}
	if strings.Contains(err.Error(), token) {
		t.Fatalf("token leaked: %v", err)
	}
}

func TestHasPassword(t *testing.T) {
	empty, err := ParseConfig([]byte("hosts:\n  default:\n    username: admin\n"))
	if err != nil {
		t.Fatal(err)
	}
	if empty.HasPassword() {
		t.Fatal("username without a password is not configured credentials")
	}
	withPassword, err := ParseConfig([]byte("hosts:\n  10.0.0.1:\n    username: admin\n    password: secret\n"))
	if err != nil {
		t.Fatal(err)
	}
	if !withPassword.HasPassword() {
		t.Fatal("a host password counts as configured credentials")
	}
}
