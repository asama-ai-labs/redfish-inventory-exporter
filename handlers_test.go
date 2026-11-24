// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Asama Inc.

package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stmcginnis/gofish"

	"github.com/asama-ai-labs/redfish-inventory-exporter/collectors"
)

// mockBMC serves a service root, and a Systems collection when withSystem is set.
func mockBMC(t *testing.T, withSystem bool) *gofish.APIClient {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/redfish/v1/", func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/redfish/v1/", "/redfish/v1":
			_, _ = w.Write([]byte(`{"@odata.id":"/redfish/v1/","RedfishVersion":"1.6.0","Systems":{"@odata.id":"/redfish/v1/Systems"}}`))
		case "/redfish/v1/Systems":
			if !withSystem {
				http.NotFound(w, r)
				return
			}
			_, _ = w.Write([]byte(`{"@odata.id":"/redfish/v1/Systems","Members":[{"@odata.id":"/redfish/v1/Systems/1"}],"Members@odata.count":1}`))
		case "/redfish/v1/Systems/1":
			_, _ = w.Write([]byte(`{"@odata.id":"/redfish/v1/Systems/1","Id":"1","Model":"M","Manufacturer":"V","SerialNumber":"S","PowerState":"On","HostName":"h"}`))
		default:
			http.NotFound(w, r)
		}
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	client, err := gofish.ConnectContext(context.Background(), gofish.ClientConfig{
		Endpoint: srv.URL, Username: "u", Password: "p", BasicAuth: true, Insecure: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func scrape(t *testing.T, withSystem bool) *httptest.ResponseRecorder {
	t.Helper()
	collector := collectors.NewCollector(mockBMC(t, withSystem), nil, false)
	rec := httptest.NewRecorder()
	NewHandlers(collector).MetricsHandler(rec, httptest.NewRequest(http.MethodGet, "/redfish", nil), "bmc1")
	return rec
}

func TestFailedScrapeReportsUpZeroInExpositionFormat(t *testing.T) {
	rec := scrape(t, false)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	if rec.Header().Get("X-Redfish-Collector-Error") == "" {
		t.Fatal("missing X-Redfish-Collector-Error")
	}
	body := rec.Body.String()
	if !strings.Contains(body, `redfish_inventory_exporter_up{target="bmc1"} 0`) {
		t.Fatalf("no up 0 in the body:\n%s", body)
	}
	if strings.Contains(body, "redfish_system_info") {
		t.Fatalf("a failed scrape must not carry Redfish gauges:\n%s", body)
	}
}

func TestSuccessfulScrapeReportsUpOneAndSystemDuration(t *testing.T) {
	rec := scrape(t, true)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body:\n%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{
		`redfish_inventory_exporter_up{target="bmc1"} 1`,
		`redfish_inventory_exporter_scrape_collector_duration_seconds{collector="system",target="bmc1"}`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %q in:\n%s", want, body)
		}
	}
}
