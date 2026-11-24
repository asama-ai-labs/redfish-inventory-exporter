// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Asama Inc.

package collectors

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stmcginnis/gofish"
	"github.com/stmcginnis/gofish/redfish"
)

// memoryService serves a system whose Memory collection lists three DIMMs, one of
// which answers 404.
func memoryService(t *testing.T) *gofish.APIClient {
	t.Helper()
	docs := map[string]string{
		"/redfish/v1/":                   `{"@odata.id":"/redfish/v1/","Systems":{"@odata.id":"/redfish/v1/Systems"}}`,
		"/redfish/v1/Systems":            `{"@odata.id":"/redfish/v1/Systems","Members":[{"@odata.id":"/redfish/v1/Systems/1"}],"Members@odata.count":1}`,
		"/redfish/v1/Systems/1":          `{"@odata.id":"/redfish/v1/Systems/1","Id":"1","Memory":{"@odata.id":"/redfish/v1/Systems/1/Memory"}}`,
		"/redfish/v1/Systems/1/Memory":   `{"@odata.id":"/redfish/v1/Systems/1/Memory","Members":[{"@odata.id":"/redfish/v1/Systems/1/Memory/1"},{"@odata.id":"/redfish/v1/Systems/1/Memory/2"},{"@odata.id":"/redfish/v1/Systems/1/Memory/3"}],"Members@odata.count":3}`,
		"/redfish/v1/Systems/1/Memory/1": `{"@odata.id":"/redfish/v1/Systems/1/Memory/1","Id":"1","Name":"DIMM 1","CapacityMiB":16384}`,
		"/redfish/v1/Systems/1/Memory/3": `{"@odata.id":"/redfish/v1/Systems/1/Memory/3","Id":"3","Name":"DIMM 3","CapacityMiB":16384}`,
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, ok := docs[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, body)
	}))
	t.Cleanup(srv.Close)
	client, err := gofish.ConnectContext(context.Background(), gofish.ClientConfig{Endpoint: srv.URL, HTTPClient: srv.Client()})
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func TestOneFailingDIMMKeepsTheOthers(t *testing.T) {
	c := NewCollector(memoryService(t), nil, false)
	info, err := c.GetMemoryInfo()
	if err == nil || !isPartial(err) {
		t.Fatalf("want a partial error, got %v", err)
	}
	if info == nil || len(info.MemoryDevices) != 2 {
		t.Fatalf("want the 2 readable DIMMs, got %+v", info)
	}
}

func TestCollectMetricsReportsDegradedCollector(t *testing.T) {
	c := NewCollector(memoryService(t), map[string]bool{
		"processor": true, "pcie": true, "bmc": true, "storage": true, "network": true,
		"chassis": true, "thermal": true, "power": true, "sensors": true, "bios": true,
		"firmware_inventory": true,
	}, false)
	c.Reset()
	if err := c.CollectMetrics("192.0.2.10"); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	c.WritePrometheus(&out)
	text := out.String()
	want := `redfish_inventory_exporter_collector_success{collector="memory",target="192.0.2.10"} 0`
	if !strings.Contains(text, want) {
		t.Fatalf("missing %q in:\n%s", want, text)
	}
	if n := strings.Count(text, "redfish_memory_"); n == 0 {
		t.Fatalf("the readable DIMMs produced no redfish_memory_* metrics:\n%s", text)
	}
}

func TestCollectorSuccessIsOneWhenNothingFailed(t *testing.T) {
	c := NewCollector(nil, nil, false)
	c.Reset()
	c.RecordCollectorSuccess("192.0.2.10", "memory", nil)
	var out bytes.Buffer
	c.WritePrometheus(&out)
	want := `redfish_inventory_exporter_collector_success{collector="memory",target="192.0.2.10"} 1`
	if !strings.Contains(out.String(), want) {
		t.Fatalf("missing %q in:\n%s", want, out.String())
	}
}

func TestExporterStatsAloneAreNotRedfishMetrics(t *testing.T) {
	c := NewCollector(nil, nil, false)
	c.Reset()
	c.RecordCollectorSuccess("192.0.2.10", "memory", nil)
	c.RecordScrapeMetrics("192.0.2.10", time.Now(), nil)
	if c.HasRedfishMetrics() {
		t.Fatal("redfish_inventory_exporter_* stats must not count as Redfish data")
	}
	c.setGauge("redfish_system_info", map[string]string{"model": "x"}, 1)
	if !c.HasRedfishMetrics() {
		t.Fatal("a redfish_* gauge must count as Redfish data")
	}
}

func TestErrorCorrectionLabelIsOnlyWhatTheBMCSent(t *testing.T) {
	wider := &redfish.Memory{BusWidthBits: 72, DataWidthBits: 64}
	if got := errorCorrectionLabel(wider); got != "Unknown" {
		t.Fatalf("a wider bus is not a scheme: got %q", got)
	}
	wider.ErrorCorrection = redfish.MultiBitECCErrorCorrection
	if got := errorCorrectionLabel(wider); got != "MultiBitECC" {
		t.Fatalf("reported scheme must pass through: got %q", got)
	}
	if got := errorCorrectionLabel(nil); got != "Unknown" {
		t.Fatalf("nil memory: got %q", got)
	}
}
