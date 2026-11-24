// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Asama Inc.

package collectors

import (
	"bytes"
	"strings"
	"testing"

	"github.com/asama-ai-labs/redfish-inventory-exporter/models"
)

func TestSetBMCMetricsEmitsUUIDLabel(t *testing.T) {
	c := NewCollector(nil, nil, false)
	c.Reset()
	c.SetBMCMetrics(&models.BMCManagerInfo{
		ID:               "Managers/1",
		Name:             "BMC",
		Model:            "iLO 5",
		Manufacturer:     "HPE",
		FirmwareVersion:  "2.80",
		UUID:             "550e8400-e29b-41d4-a716-446655440000",
		BMCManagerState:  "Enabled",
		BMCManagerHealth: "OK",
	})
	var out bytes.Buffer
	c.WritePrometheus(&out)
	text := out.String()
	want := `redfish_bmc_manager_info{firmware_version="2.80",id="Managers/1",manufacturer="HPE",model="iLO 5",name="BMC",uuid="550e8400-e29b-41d4-a716-446655440000"} 1`
	if !strings.Contains(text, want) {
		t.Fatalf("missing BMC manager info with uuid in metrics:\n%s", text)
	}
}

func TestSetBMCMetricsEmitsEmptyUUIDWhenOmitted(t *testing.T) {
	c := NewCollector(nil, nil, false)
	c.Reset()
	c.SetBMCMetrics(&models.BMCManagerInfo{
		ID:               "Managers/1",
		Name:             "BMC",
		Model:            "iLO 5",
		Manufacturer:     "HPE",
		FirmwareVersion:  "2.80",
		BMCManagerState:  "Enabled",
		BMCManagerHealth: "OK",
	})
	var out bytes.Buffer
	c.WritePrometheus(&out)
	text := out.String()
	if !strings.Contains(text, `redfish_bmc_manager_info{`) || !strings.Contains(text, `uuid=""`) {
		t.Fatalf("expected redfish_bmc_manager_info with empty uuid label, got:\n%s", text)
	}
}
