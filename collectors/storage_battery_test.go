// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Asama Inc.

package collectors

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/asama-ai-labs/redfish-inventory-exporter/models"
	"github.com/stmcginnis/gofish/redfish"
)

func TestSetStorageMetricsEmitsBatteryMetrics(t *testing.T) {
	c := NewCollector(nil, nil, true)
	c.Reset()
	c.SetStorageMetrics(&models.StorageInfo{
		Batteries: []models.StorageBattery{
			{
				StorageID:                  "0",
				ControllerID:               "0",
				ID:                         "1",
				Name:                       "HPE Smart Storage Battery",
				Model:                      "875241-B21",
				Manufacturer:               "HPE",
				SerialNumber:               "DUMMY-SerialNumber",
				FirmwareVersion:            "0.70",
				SparePartNumber:            "878643-001",
				ChargeState:                "Idle",
				CapacityActualWattHours:    12.5,
				CapacityRatedWattHours:     13.5,
				CapacityActualAmpHours:     1.2,
				CapacityRatedAmpHours:      1.3,
				MaxChargeRateAmps:          2.1,
				MaxDischargeRateAmps:       2.2,
				MaxChargeVoltage:           3.3,
				StateOfHealthReading:       91,
				ChargeLevelPercent:         92,
				MaximumCapWatts:            96,
				RemainingChargeTimeSeconds: 37,
				Health:                     "OK",
				State:                      "Enabled",
			},
		},
	})
	var out bytes.Buffer
	c.WritePrometheus(&out)
	text := out.String()
	for _, want := range []string{
		"redfish_storage_battery_info",
		`battery_id="1"`,
		`spare_part_number="878643-001"`,
		"redfish_storage_battery_capacity_actual_watt_hours",
		"redfish_storage_battery_max_charge_voltage",
		`redfish_storage_battery_health_info{battery_id="1",controller_id="0",health="OK",storage_id="0"} 1`,
		`redfish_storage_battery_state_info{battery_id="1",controller_id="0",state="Enabled",storage_id="0"} 1`,
		`redfish_storage_battery_state_of_health_percent{battery_id="1",controller_id="0",storage_id="0"} 91`,
		`redfish_storage_battery_charge_level_percent{battery_id="1",controller_id="0",storage_id="0"} 92`,
		`redfish_storage_battery_maximum_cap_watts{battery_id="1",controller_id="0",storage_id="0"} 96`,
		`redfish_storage_battery_remaining_charge_time_seconds{battery_id="1",controller_id="0",storage_id="0"} 37`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q in metrics:\n%s", want, text)
		}
	}
}

func TestStorageControllerUnmarshalsBatteryLinks(t *testing.T) {
	raw := []byte(`{
		"@odata.id":"/redfish/v1/Systems/1/Storage/0/Controllers/0",
		"@odata.type":"#StorageController.v1_7_0.StorageController",
		"Id":"0",
		"Links":{
			"Batteries":[{"@odata.id":"/redfish/v1/Systems/1/Storage/0/Controllers/0/Batteries/1"}],
			"Batteries@odata.count":1
		}
	}`)
	var sc redfish.StorageController
	if err := json.Unmarshal(raw, &sc); err != nil {
		t.Fatal(err)
	}
	if sc.BatteriesCount != 1 {
		t.Fatalf("BatteriesCount = %d, want 1", sc.BatteriesCount)
	}
}

func TestApplyRedpoolBatteryOEMFromDoc(t *testing.T) {
	full := []byte(`{"Oem":{"Redpool":{"HpeSmartStorage":{"ChargeLevelPercent":99,"MaximumCapWatts":96,"RemainingChargeTimeSeconds":37}}}}`)
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(full, &doc); err != nil {
		t.Fatal(err)
	}
	m := models.StorageBattery{}
	applyRedpoolBatteryOEMFromDoc(doc, &m)
	if m.ChargeLevelPercent != 99 || m.MaximumCapWatts != 96 || m.RemainingChargeTimeSeconds != 37 {
		t.Fatalf("unexpected OEM parse: %#v", m)
	}
}
