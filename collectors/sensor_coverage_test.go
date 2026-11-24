// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Asama Inc.

package collectors

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stmcginnis/gofish"
	"github.com/stmcginnis/gofish/redfish"
)

func TestCollectChassisSensorsEmitsReadings(t *testing.T) {
	const sensor = `{
		"@odata.id": "/redfish/v1/Chassis/1/Sensors/Fan1",
		"@odata.type": "#Sensor.v1_0_0.Sensor",
		"Id": "Fan.Embedded.1",
		"Name": "System Board Fan1",
		"Reading": 3200,
		"ReadingType": "Rotational",
		"ReadingUnits": "RPM",
		"PhysicalContext": "SystemBoard",
		"Status": {"Health": "OK", "State": "Enabled"}
	}`
	mux := http.NewServeMux()
	mux.HandleFunc("/redfish/v1/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/redfish/v1/" && r.URL.Path != "/redfish/v1" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"@odata.id":"/redfish/v1/","Id":"Root","Name":"Root","RedfishVersion":"1.6.0"}`))
	})
	mux.HandleFunc("/redfish/v1/Chassis/1", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"@odata.id": "/redfish/v1/Chassis/1",
			"@odata.type": "#Chassis.v1_0_0.Chassis",
			"Id": "1",
			"Name": "Chassis",
			"ChassisType": "RackMount",
			"Sensors": {"@odata.id": "/redfish/v1/Chassis/1/Sensors"}
		}`))
	})
	mux.HandleFunc("/redfish/v1/Chassis/1/Sensors", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"@odata.id": "/redfish/v1/Chassis/1/Sensors",
			"Name": "Sensors",
			"Members": [{"@odata.id": "/redfish/v1/Chassis/1/Sensors/Fan1"}]
		}`))
	})
	mux.HandleFunc("/redfish/v1/Chassis/1/Sensors/Fan1", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(sensor))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	client, err := gofish.ConnectDefault(srv.URL)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer client.Logout()
	ch, err := redfish.GetChassis(client, "/redfish/v1/Chassis/1")
	if err != nil {
		t.Fatalf("chassis: %v", err)
	}

	c := NewCollector(client, nil, false)
	c.collectChassisSensors(ch)

	var out bytes.Buffer
	c.WritePrometheus(&out)
	text := out.String()
	want := `redfish_chassis_sensor_reading{chassis_id="1",physical_context="SystemBoard",reading_type="Rotational",reading_units="RPM",sensor_id="Fan.Embedded.1",sensor_name="System Board Fan1"} 3200`
	if !strings.Contains(text, want) {
		t.Fatalf("missing chassis sensor reading, got:\n%s", text)
	}
}

func TestMigrateSystemPowerUpperCritical(t *testing.T) {
	c := NewCollector(nil, nil, false)
	c.Reset()
	cov := c.coverageFor("System.Embedded.1")
	cov.markPowerControl("System Power Control")
	cov.markTotalConsumed()

	sensor := &redfish.Sensor{
		Reading: 264,
		Thresholds: redfish.Thresholds{
			UpperCritical: redfish.Threshold{Reading: 1920},
		},
	}
	sensor.ID = "SystemBoardPwrConsumption"
	sensor.Name = "System Board Pwr Consumption"
	c.migrateSystemPowerUpperCritical("System.Embedded.1", cov, sensor, sensor.Name)

	var out bytes.Buffer
	c.WritePrometheus(&out)
	text := out.String()
	want := `redfish_powersupply_power_upper_critical_watts{chassis_id="System.Embedded.1",power_control_name="System Power Control"} 1920`
	if !strings.Contains(text, want) {
		t.Fatalf("missing migrated upper critical watts metric, got:\n%s", text)
	}
}

func TestFlushPendingEnvironmentPowerSkipsSystemConsumptionDuplicate(t *testing.T) {
	c := NewCollector(nil, nil, false)
	cov := c.coverageFor("System.Embedded.1")
	cov.stageEnvironmentPowerWatts(625, "/redfish/v1/Chassis/System.Embedded.1/Sensors/SystemBoardPwrConsumption", "SystemBoardPwrConsumption")
	cov.markTotalConsumed()
	c.flushPendingEnvironmentPower("System.Embedded.1")

	var out bytes.Buffer
	c.WritePrometheus(&out)
	if strings.Contains(out.String(), "redfish_chassis_environment_power_watts") {
		t.Fatalf("expected no environment power when duplicate of total consumed")
	}
}

func TestFlushPendingEnvironmentPowerKeepsNonSystemSource(t *testing.T) {
	c := NewCollector(nil, nil, false)
	cov := c.coverageFor("1")
	cov.stageEnvironmentPowerWatts(200, "/redfish/v1/Chassis/1/Sensors/89L0", "89L0")
	cov.markTotalConsumed()
	c.flushPendingEnvironmentPower("1")

	var out bytes.Buffer
	c.WritePrometheus(&out)
	want := `redfish_chassis_environment_power_watts{chassis_id="1",data_source_ref="89L0",data_source_uri="/redfish/v1/Chassis/1/Sensors/89L0"} 200`
	if !strings.Contains(out.String(), want) {
		t.Fatalf("expected environment power for non-system sensor ref, got:\n%s", out.String())
	}
}
