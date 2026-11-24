// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Asama Inc.

package main

import "testing"

func TestSplitTarget(t *testing.T) {
	good := map[string]string{
		"10.0.0.1":         "10.0.0.1",
		"10.0.0.1:8443":    "10.0.0.1",
		"bmc.example.com":  "bmc.example.com",
		"bmc.example:443":  "bmc.example",
		"[2001:db8::1]":    "2001:db8::1",
		"[2001:db8::1]:80": "2001:db8::1",
		"2001:db8::1":      "2001:db8::1",
	}
	for in, want := range good {
		got, err := splitTarget(in)
		if err != nil || got != want {
			t.Errorf("splitTarget(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, in := range []string{"", "10.0.0.1/redfish/v1", "user@10.0.0.1", "10.0.0.1?x=1", "a b", "10.0.0.1:", "host:not:valid", `10.0.0.1\x`} {
		if _, err := splitTarget(in); err == nil {
			t.Errorf("splitTarget(%q) should fail", in)
		}
	}
}

func TestTargetPolicy(t *testing.T) {
	p, err := parseTargetPolicy("10.1.0.0/16, 192.0.2.7, BMC-1.example.com")
	if err != nil {
		t.Fatal(err)
	}
	allow := []string{"10.1.2.3", "192.0.2.7", "bmc-1.example.com", "::ffff:10.1.2.3"}
	deny := []string{"10.2.0.1", "192.0.2.8", "10.1.attacker.com", "other.example.com"}
	for _, h := range allow {
		if !p.allows(h) {
			t.Errorf("%s should be allowed", h)
		}
	}
	for _, h := range deny {
		if p.allows(h) {
			t.Errorf("%s should be denied", h)
		}
	}
	empty, _ := parseTargetPolicy("")
	if !empty.allows("anything") {
		t.Error("empty policy allows every host")
	}
	if _, err := parseTargetPolicy("10.1.*"); err == nil {
		t.Error("IP-shaped glob must be rejected")
	}
}

func TestScrapeLimiter(t *testing.T) {
	l := newScrapeLimiter(2)
	if !l.tryAcquire() || !l.tryAcquire() {
		t.Fatal("two slots should be free")
	}
	if l.tryAcquire() {
		t.Fatal("third acquire must be refused")
	}
	l.release()
	if !l.tryAcquire() {
		t.Fatal("a released slot should be reusable")
	}
	if !newScrapeLimiter(0).tryAcquire() {
		t.Fatal("zero means no limit")
	}
}

func TestBMCTLSConfig(t *testing.T) {
	cfg, err := bmcTLSConfig(false, "")
	if err != nil || !cfg.InsecureSkipVerify {
		t.Fatalf("default skips verification: %v %v", cfg, err)
	}
	cfg, err = bmcTLSConfig(true, "")
	if err != nil || cfg.InsecureSkipVerify {
		t.Fatalf("--tls.verify must verify: %v %v", cfg, err)
	}
	if _, err := bmcTLSConfig(false, "/nonexistent/ca.pem"); err == nil {
		t.Fatal("a missing CA file must fail")
	}
}
