// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Asama Inc.

package main

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/asama-ai-labs/redfish-inventory-exporter/internal/credentials"
)

func TestFailCredentialLookupRedacts(t *testing.T) {
	rr := httptest.NewRecorder()
	writeCredentialError(rr, "https://user:yaml-pass@10.0.0.5/redfish", errors.New("op://bmc/10.0.0.5/password hunter2"))
	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("status %d", rr.Code)
	}
	body := rr.Body.String()
	if body != "failed to resolve credentials\n" {
		t.Fatalf("body %q", body)
	}
	for _, leaked := range []string{"op://", "hunter2", "yaml-pass", "password"} {
		if strings.Contains(body, leaked) {
			t.Fatalf("response leaked %q", leaked)
		}
	}

	rr = httptest.NewRecorder()
	writeCredentialError(rr, "bmc.example", &credentials.Failure{
		Status: http.StatusBadRequest,
		Public: "target must be an IP address",
		Err:    credentials.ErrInvalidTarget,
	})
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status %d", rr.Code)
	}
	if strings.Contains(rr.Body.String(), "bmc.example") {
		t.Fatalf("body %q", rr.Body.String())
	}
}
