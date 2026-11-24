// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Asama Inc.

package collectors

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/asama-ai-labs/redfish-inventory-exporter/models"
	"github.com/stmcginnis/gofish/redfish"
)

// joinChildIDs sorts the IDs first. gofish fetches linked chassis in parallel and returns
// them in completion order, so an unsorted join changes from one scrape to the next.
func joinChildIDs(ids []string) string {
	sorted := append([]string(nil), ids...)
	sort.Strings(sorted)
	return strings.Join(sorted, ",")
}

func chassisRedpoolExt(body []byte) (fru, ecl, mdate string, multiNodeLabels map[string]string, nodes []models.MultiNodeInfo) {
	multiNodeLabels = map[string]string{}
	if len(body) == 0 {
		return "", "", "", multiNodeLabels, nil
	}
	var doc struct {
		Oem struct {
			Redpool map[string]interface{} `json:"Redpool"`
		} `json:"Oem"`
	}
	if err := json.Unmarshal(body, &doc); err != nil || doc.Oem.Redpool == nil {
		return "", "", "", multiNodeLabels, nil
	}
	rp := doc.Oem.Redpool
	if v, ok := rp["FruPartNumber"].(string); ok {
		fru = strings.TrimSpace(v)
	}
	if v, ok := rp["EngineeringChangeLevel"].(string); ok {
		ecl = strings.TrimSpace(v)
	}
	if v, ok := rp["ManufactureDate"].(string); ok {
		mdate = strings.TrimSpace(v)
	}
	for _, key := range []string{
		"MultiNodeSystemPartNo",
		"MultiNodeSystemSerialNo",
		"MultiNodeChassisPartNo",
		"MultiNodeSelfID",
		"MultiNodeTwinType",
	} {
		if v, ok := rp[key]; ok && v != nil {
			s := strings.TrimSpace(fmt.Sprint(v))
			if s != "" {
				multiNodeLabels[strings.ToLower(key)] = s
			}
		}
	}
	if raw, ok := rp["MultiNodeNodes"]; ok {
		nodes = parseMultiNodeNodes(raw)
	}
	return fru, ecl, mdate, multiNodeLabels, nodes
}

func parseMultiNodeNodes(raw interface{}) []models.MultiNodeInfo {
	arr, ok := raw.([]interface{})
	if !ok {
		return nil
	}
	out := make([]models.MultiNodeInfo, 0, len(arr))
	for _, item := range arr {
		m, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		n := models.MultiNodeInfo{
			ID:                strField(m, "Id"),
			PowerStatus:       strField(m, "PowerStatus"),
			Health:            strField(m, "Health"),
			State:             strField(m, "State"),
			BoardPartNumber:   strField(m, "BoardPartNumber"),
			BoardSerialNumber: strField(m, "BoardSerialNumber"),
			BIOSVersion:       strField(m, "BIOSVersion"),
			CPLDVersion:       strField(m, "CPLDVersion"),
			BMCVersion:        strField(m, "BMCVersion"),
		}
		if f, ok := asFloat64(m["DCOutputPower"]); ok {
			n.DCOutputPower = &f
		}
		if f, ok := asFloat64(m["OutputCurrent"]); ok {
			n.OutputCurrent = &f
		}
		if temps, ok := m["Temperatures"].([]interface{}); ok {
			for _, t := range temps {
				tm, ok := t.(map[string]interface{})
				if !ok {
					continue
				}
				name := strField(tm, "Name")
				reading, ok := asFloat64(tm["ReadingCelsius"])
				if name == "" || !ok {
					continue
				}
				n.Temperatures = append(n.Temperatures, models.MultiNodeTemperature{
					Name:           name,
					ReadingCelsius: reading,
				})
			}
		}
		if n.ID != "" {
			out = append(out, n)
		}
	}
	return out
}

func strField(m map[string]interface{}, key string) string {
	v, ok := m[key]
	if !ok || v == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(v))
}

func (c *Collector) GetChassisInfo() (*models.ChassisResponse, error) {
	var partial []error
	chassis, err := c.client.Service.Chassis()
	if !keepPartial("chassis", err, &partial) {
		return nil, err
	}

	seen := map[string]bool{}
	var chassisInfos []models.ChassisInfo
	var walk func(ch *redfish.Chassis, depth int)
	walk = func(ch *redfish.Chassis, depth int) {
		if ch == nil || ch.ID == "" || seen[ch.ID] {
			return
		}
		seen[ch.ID] = true
		fru := ""
		ecl := ""
		mdate := ""
		mn := map[string]string{}
		var nodes []models.MultiNodeInfo
		if cli := ch.GetClient(); cli != nil && ch.ODataID != "" {
			if body, ok := getRedfishResourceJSON(cli, ch.ODataID); ok {
				fru, ecl, mdate, mn, nodes = chassisRedpoolExt(body)
			}
		}
		chassisInfo := models.ChassisInfo{
			ID:                     ch.ID,
			Model:                  ch.Model,
			SKU:                    ch.SKU,
			Manufacturer:           ch.Manufacturer,
			SerialNumber:           ch.SerialNumber,
			AssetTag:               ch.AssetTag,
			PartNumber:             ch.PartNumber,
			FruPartNumber:          fru,
			EngineeringChangeLevel: ecl,
			ManufactureDate:        mdate,
			MultiNodeSystemPart:    mn["multinodesystempartno"],
			MultiNodeSystemSerial:  mn["multinodesystemserialno"],
			MultiNodeChassisPart:   mn["multinodechassispartno"],
			MultiNodeSelfID:        mn["multinodeselfid"],
			MultiNodeTwinType:      mn["multinodetwintype"],
			MultiNodeNodes:         nodes,
			ChassisType:            string(ch.ChassisType),
			Description:            ch.Description,
			IndicatorLED:           string(ch.IndicatorLED),
			PowerState:             string(ch.PowerState),
			Status: models.Status{
				State:        statusStr(string(ch.Status.State)),
				Health:       statusStr(string(ch.Status.Health)),
				HealthRollup: statusStr(string(ch.Status.HealthRollup)),
			},
		}
		if parent, err := ch.ContainedBy(); err == nil && parent != nil {
			chassisInfo.ContainedBy = parent.ID
		}
		fillPhysicalSecurity(&chassisInfo, ch.PhysicalSecurity)
		chassisInfos = append(chassisInfos, chassisInfo)
		idx := len(chassisInfos) - 1
		if depth >= 2 {
			return
		}
		contained, err := ch.Contains()
		if err != nil {
			return
		}
		var childIDs []string
		for _, child := range contained {
			if child != nil && child.ID != "" {
				childIDs = append(childIDs, child.ID)
			}
			walk(child, depth+1)
		}
		if len(childIDs) > 0 {
			// Use saved idx: recursion appends descendants after this chassis.
			chassisInfos[idx].Contains = joinChildIDs(childIDs)
		}
	}
	for _, ch := range chassis {
		walk(ch, 0)
	}

	return &models.ChassisResponse{Chassis: chassisInfos}, errors.Join(partial...)
}

// fillPhysicalSecurity copies Chassis.PhysicalSecurity.IntrusionSensor when present.
func fillPhysicalSecurity(out *models.ChassisInfo, ps redfish.PhysicalSecurity) {
	if ps.IntrusionSensor == "" {
		return
	}
	out.IntrusionSensor = string(ps.IntrusionSensor)
}

func hasMultiNodeChassisInfo(chassis models.ChassisInfo) bool {
	return chassis.MultiNodeSystemPart != "" ||
		chassis.MultiNodeSystemSerial != "" ||
		chassis.MultiNodeChassisPart != "" ||
		chassis.MultiNodeSelfID != "" ||
		chassis.MultiNodeTwinType != ""
}

// SetChassisMetrics sets chassis gauges (used by collect.go).
func (c *Collector) SetChassisMetrics(chassisResponse *models.ChassisResponse) {
	if chassisResponse != nil {
		for _, chassis := range chassisResponse.Chassis {
			partNumber := chassis.PartNumber
			if partNumber == "" && chassis.FruPartNumber != "" {
				partNumber = chassis.FruPartNumber
			}
			c.setGauge("redfish_chassis_info", map[string]string{
				"id":                       chassis.ID,
				"model":                    chassis.Model,
				"sku":                      chassis.SKU,
				"manufacturer":             chassis.Manufacturer,
				"device_serial_number":     chassis.SerialNumber,
				"asset_tag":                chassis.AssetTag,
				"part_number":              partNumber,
				"fru_part_number":          chassis.FruPartNumber,
				"chassis_type":             chassis.ChassisType,
				"contained_by":             chassis.ContainedBy,
				"contains":                 chassis.Contains,
				"manufacture_date":         chassis.ManufactureDate,
				"engineering_change_level": chassis.EngineeringChangeLevel,
			}, 1)
			if hasMultiNodeChassisInfo(chassis) {
				c.setGauge("redfish_multinode_chassis_info", map[string]string{
					"id":                      chassis.ID,
					"multinode_system_part":   chassis.MultiNodeSystemPart,
					"multinode_system_serial": chassis.MultiNodeSystemSerial,
					"multinode_chassis_part":  chassis.MultiNodeChassisPart,
					"multinode_self_id":       chassis.MultiNodeSelfID,
					"multinode_twin_type":     chassis.MultiNodeTwinType,
				}, 1)
			}
			c.setGauge("redfish_chassis_power_state_info", map[string]string{
				"id":          chassis.ID,
				"power_state": statusStr(chassis.PowerState),
			}, 1)
			c.setGauge("redfish_chassis_health_info", map[string]string{
				"id":            chassis.ID,
				"health":        chassis.Status.Health,
				"health_rollup": chassis.Status.HealthRollup,
			}, 1)
			c.setGauge("redfish_chassis_state_info", map[string]string{
				"id":    chassis.ID,
				"state": chassis.Status.State,
			}, 1)
			if chassis.IntrusionSensor != "" {
				c.setGauge("redfish_chassis_intrusion_state", map[string]string{
					"id":               chassis.ID,
					"intrusion_sensor": chassis.IntrusionSensor,
				}, 1)
			}
			for _, node := range chassis.MultiNodeNodes {
				c.setGauge("redfish_chassis_multinode_info", map[string]string{
					"chassis_id":          chassis.ID,
					"node_id":             node.ID,
					"power_status":        node.PowerStatus,
					"board_part_number":   node.BoardPartNumber,
					"board_serial_number": node.BoardSerialNumber,
					"bios_version":        node.BIOSVersion,
					"cpld_version":        node.CPLDVersion,
					"bmc_version":         node.BMCVersion,
				}, 1)
				c.setGauge("redfish_chassis_multinode_health_info", map[string]string{
					"chassis_id": chassis.ID,
					"node_id":    node.ID,
					"health":     statusStr(node.Health),
				}, 1)
				c.setGauge("redfish_chassis_multinode_state_info", map[string]string{
					"chassis_id": chassis.ID,
					"node_id":    node.ID,
					"state":      statusStr(node.State),
				}, 1)
				if node.DCOutputPower != nil {
					c.setGauge("redfish_chassis_multinode_dc_output_power_watts", map[string]string{
						"chassis_id": chassis.ID,
						"node_id":    node.ID,
					}, *node.DCOutputPower)
				}
				if node.OutputCurrent != nil {
					c.setGauge("redfish_chassis_multinode_output_current_amps", map[string]string{
						"chassis_id": chassis.ID,
						"node_id":    node.ID,
					}, *node.OutputCurrent)
				}
				for _, t := range node.Temperatures {
					c.setGauge("redfish_chassis_multinode_temperature_celsius", map[string]string{
						"chassis_id":  chassis.ID,
						"node_id":     node.ID,
						"sensor_name": t.Name,
					}, t.ReadingCelsius)
				}
			}
		}
	}
}
