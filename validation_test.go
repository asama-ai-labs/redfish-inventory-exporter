// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Asama Inc.

package main

import (
	"net/url"
	"strings"
	"testing"
)

func TestValidateRedfishQuery(t *testing.T) {
	q := url.Values{}
	_, _, _, err := ValidateRedfishQuery(q)
	if err == nil {
		t.Fatal("expected error for empty query")
	}
	if !strings.Contains(err.Error(), "target parameter is missing") {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(err.Error(), "GET /redfish?") {
		t.Fatalf("expected help in error: %v", err)
	}

	q.Set("target", "10.0.0.1")
	_, _, _, err = ValidateRedfishQuery(q)
	if err == nil {
		t.Fatal("expected error missing hostname")
	}
	if !strings.Contains(err.Error(), "hostname parameter is missing") {
		t.Fatalf("unexpected error: %v", err)
	}

	q.Set("hostname", "h1")
	_, _, _, err = ValidateRedfishQuery(q)
	if err == nil {
		t.Fatal("expected error missing serialnumber")
	}
	if !strings.Contains(err.Error(), "serialnumber parameter is missing") {
		t.Fatalf("unexpected error: %v", err)
	}

	q.Set("serialnumber", "s1")
	target, host, serial, err := ValidateRedfishQuery(q)
	if err != nil {
		t.Fatal(err)
	}
	if target != "10.0.0.1" || host != "h1" || serial != "s1" {
		t.Fatalf("got %q %q %q", target, host, serial)
	}

	q.Set("hostname", "  trimmed  ")
	q.Set("serialnumber", " sn ")
	_, host, serial, err = ValidateRedfishQuery(q)
	if err != nil {
		t.Fatal(err)
	}
	if host != "trimmed" || serial != "sn" {
		t.Fatalf("trim: host=%q serial=%q", host, serial)
	}
}
