// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Asama Inc.

package collectors

import (
	"bytes"
	"strings"
	"testing"
)

func TestEscapePrometheusHelp(t *testing.T) {
	if got := escapePrometheusHelp(`a\b` + "\n"); got != `a\\b\n` {
		t.Fatalf("escape: %q", got)
	}
}

func TestEnrichPrometheusHelp(t *testing.T) {
	in := `# HELP redfish_chassis_health_info
# TYPE redfish_chassis_health_info gauge
redfish_chassis_health_info{id="1",health="OK"} 1
`
	var out bytes.Buffer
	if err := enrichPrometheusHelp(strings.NewReader(in), &out); err != nil {
		t.Fatal(err)
	}
	s := out.String()
	if !strings.Contains(s, "# HELP redfish_chassis_health_info ") {
		t.Fatalf("missing HELP prefix: %q", s)
	}
	if !strings.Contains(s, "OK, Warning, Critical") {
		t.Fatalf("missing enum doc: %q", s)
	}
}

func TestPrometheusFamilyHelpAllDocFamilies(t *testing.T) {
	// Documented info/value metrics from promMetricDoc + a few representative patterns.
	for _, family := range []string{
		"redfish_bios_attributes",
		"redfish_bmc_manager_info",
		"redfish_memory",
		"redfish_powersupply_total_consumed_watts",
		"redfish_thermal_sensor_temperature",
		"redfish_inventory_exporter_up",
	} {
		if s := prometheusFamilyHelp(family); s == "" {
			t.Fatalf("empty help for %q", family)
		}
	}
	if s := prometheusFamilyHelp("redfish_processor_health_info"); !strings.Contains(s, "OK, Warning, Critical") {
		t.Fatalf("health metric: %q", s)
	}
	if s := prometheusFamilyHelp("redfish_hypothetical_future_gauge"); s == "" || !strings.Contains(s, "redfish_hypothetical_future_gauge") {
		t.Fatalf("fallback: %q", s)
	}
}
