// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Asama Inc.

package collectors

import (
	"bytes"
	"strings"
	"testing"

	"github.com/asama-ai-labs/redfish-inventory-exporter/models"
)

func TestSetSystemMetricsEmitsUUIDLabel(t *testing.T) {
	c := NewCollector(nil, nil, false)
	c.Reset()
	c.SetSystemMetrics(&models.SystemInfo{
		Model:        "PowerEdge R630",
		Manufacturer: "Dell Inc.",
		SerialNumber: "SVCTAG01",
		HostName:     "r630.example",
		PowerState:   "On",
		UUID:         "550e8400-e29b-41d4-a716-446655440000",
	})
	var out bytes.Buffer
	c.WritePrometheus(&out)
	text := out.String()
	want := `redfish_system_info{hostname="r630.example",manufacturer="Dell Inc.",model="PowerEdge R630",pca_part_number="",serial_number="SVCTAG01",sub_model="",uuid="550e8400-e29b-41d4-a716-446655440000"} 1`
	if !strings.Contains(text, want) {
		t.Fatalf("missing system info with uuid in metrics:\n%s", text)
	}
}

func TestSetSystemMetricsEmitsEmptyUUIDWhenOmitted(t *testing.T) {
	c := NewCollector(nil, nil, false)
	c.Reset()
	c.SetSystemMetrics(&models.SystemInfo{
		Model:        "Test",
		Manufacturer: "Vendor",
		SerialNumber: "SN1",
		HostName:     "host",
		PowerState:   "On",
	})
	var out bytes.Buffer
	c.WritePrometheus(&out)
	text := out.String()
	if !strings.Contains(text, `redfish_system_info{`) || !strings.Contains(text, `uuid=""`) {
		t.Fatalf("expected redfish_system_info with empty uuid label, got:\n%s", text)
	}
}
