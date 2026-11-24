// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Asama Inc.

package collectors

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"

	"github.com/asama-ai-labs/redfish-inventory-exporter/models"
)

// systemRedpoolExt reads Oem.Redpool promoted by Redpool (DellSystem rollups, airflow, HPE PCA, etc.).
func systemRedpoolExt(body []byte) (rollups map[string]string, airflowCFM, exhaustC *float64, pca, post, fanRed, aggHealth string) {
	rollups = map[string]string{}
	if len(body) == 0 {
		return rollups, nil, nil, "", "", "", ""
	}
	var doc struct {
		Oem struct {
			Redpool map[string]interface{} `json:"Redpool"`
		} `json:"Oem"`
	}
	if err := json.Unmarshal(body, &doc); err != nil || doc.Oem.Redpool == nil {
		return rollups, nil, nil, "", "", "", ""
	}
	rp := doc.Oem.Redpool
	for k, v := range rp {
		if !strings.HasSuffix(k, "RollupStatus") {
			continue
		}
		s := strings.TrimSpace(fmtSprint(v))
		if s != "" {
			rollups[k] = s
		}
	}
	if v, ok := rp["EstimatedSystemAirflowCFM"]; ok {
		if f, ok := asFloat64(v); ok {
			airflowCFM = &f
		}
	}
	if v, ok := rp["EstimatedExhaustTemperatureCelsius"]; ok {
		if f, ok := asFloat64(v); ok {
			exhaustC = &f
		}
	}
	pca = strings.TrimSpace(fmtSprint(rp["PCAPartNumber"]))
	post = strings.TrimSpace(fmtSprint(rp["PostState"]))
	fanRed = strings.TrimSpace(fmtSprint(rp["FanRedundancy"]))
	aggHealth = strings.TrimSpace(fmtSprint(rp["AggregateServerHealth"]))
	return rollups, airflowCFM, exhaustC, pca, post, fanRed, aggHealth
}

func fmtSprint(v interface{}) string {
	if v == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(v))
}

func asFloat64(v interface{}) (float64, bool) {
	switch t := v.(type) {
	case float64:
		return t, true
	case float32:
		return float64(t), true
	case int:
		return float64(t), true
	case int64:
		return float64(t), true
	case json.Number:
		f, err := t.Float64()
		return f, err == nil
	default:
		return 0, false
	}
}

// GetSystemInfo returns system identification and power state using standard Redfish only.
// Vendor-specific serial number mapping (e.g. Dell ServiceTag) is done by redpool normalizers.
func (c *Collector) GetSystemInfo() (*models.SystemInfo, error) {
	log.Println("Collecting system info...")
	systems, err := c.client.Service.Systems()
	if err != nil {
		log.Printf("Error getting systems: %v", err)
		return nil, err
	}
	if len(systems) == 0 {
		log.Println("No systems found")
		return nil, nil
	}
	system := systems[0]
	serialNumber := system.SerialNumber
	log.Printf("Found system: %s, %s, %s", system.Model, system.Manufacturer, serialNumber)

	info := &models.SystemInfo{
		Model:        system.Model,
		Manufacturer: system.Manufacturer,
		SerialNumber: serialNumber,
		HostName:     system.HostName,
		PowerState:   string(system.PowerState),
		UUID:         system.UUID,
		SubModel:     system.SubModel,
		Health:       statusStr(string(system.Status.Health)),
		HealthRollup: statusStr(string(system.Status.HealthRollup)),
		State:        statusStr(string(system.Status.State)),
	}
	if cli := system.GetClient(); cli != nil && system.ODataID != "" {
		if body, ok := getRedfishResourceJSON(cli, system.ODataID); ok {
			rollups, airflow, exhaust, pca, post, fanRed, agg := systemRedpoolExt(body)
			info.OEMRollups = rollups
			info.PCAPartNumber = pca
			info.PostState = post
			info.FanRedundancy = fanRed
			info.AggregateServerHealth = agg
			if airflow != nil {
				info.EstimatedAirflowCFM = airflow
			}
			if exhaust != nil {
				info.EstimatedExhaustTempC = exhaust
			}
		}
	}
	return info, nil
}

// SetSystemMetrics sets system inventory gauges (used by collect.go).
func (c *Collector) SetSystemMetrics(sysInfo *models.SystemInfo) {
	if sysInfo == nil {
		return
	}
	c.setGauge("redfish_system_info", map[string]string{
		"serial_number":   sysInfo.SerialNumber,
		"model":           sysInfo.Model,
		"manufacturer":    sysInfo.Manufacturer,
		"hostname":        sysInfo.HostName,
		"uuid":            sysInfo.UUID,
		"sub_model":       sysInfo.SubModel,
		"pca_part_number": sysInfo.PCAPartNumber,
	}, 1)
	c.setGauge("redfish_system_health_info", map[string]string{
		"health":                  sysInfo.Health,
		"health_rollup":           sysInfo.HealthRollup,
		"aggregate_server_health": sysInfo.AggregateServerHealth,
	}, 1)
	c.setGauge("redfish_system_state_info", map[string]string{
		"state":       sysInfo.State,
		"power_state": sysInfo.PowerState,
		"post_state":  sysInfo.PostState,
	}, 1)
	c.setGauge("redfish_system_fan_redundancy_info", map[string]string{
		"fan_redundancy": sysInfo.FanRedundancy,
	}, 1)
	for name, health := range sysInfo.OEMRollups {
		c.setGauge("redfish_system_rollup_health_info", map[string]string{
			"rollup": name,
			"health": health,
		}, 1)
	}
	if sysInfo.EstimatedAirflowCFM != nil {
		c.setGauge("redfish_system_estimated_airflow_cfm", map[string]string{}, *sysInfo.EstimatedAirflowCFM)
		c.systemAirflowEmitted = true
	}
	if sysInfo.EstimatedExhaustTempC != nil {
		c.setGauge("redfish_system_estimated_exhaust_temperature_celsius", map[string]string{}, *sysInfo.EstimatedExhaustTempC)
	}
}
