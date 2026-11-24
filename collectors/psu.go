// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Asama Inc.

package collectors

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/asama-ai-labs/redfish-inventory-exporter/redpoolext"
	"github.com/stmcginnis/gofish/common"
	"github.com/stmcginnis/gofish/redfish"
)

func getRedfishResourceJSON(cli common.Client, uri string) ([]byte, bool) {
	if cli == nil || uri == "" {
		return nil, false
	}
	resp, err := cli.Get(uri)
	if err != nil || resp == nil {
		return nil, false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, false
	}
	b, err := io.ReadAll(resp.Body)
	if err != nil || len(b) == 0 {
		return nil, false
	}
	return b, true
}

func redpoolManufacturePerPowerSupplyFromPowerJSON(body []byte) []*redpoolext.RedpoolPowerSupplyExt {
	var doc struct {
		PowerSupplies []json.RawMessage `json:"PowerSupplies"`
	}
	if err := json.Unmarshal(body, &doc); err != nil {
		return nil
	}
	out := make([]*redpoolext.RedpoolPowerSupplyExt, len(doc.PowerSupplies))
	for i, raw := range doc.PowerSupplies {
		if ext, ok := redpoolext.ParseRedpoolPowerSupplyExtFromJSON(raw); ok {
			e := ext
			out[i] = &e
		}
	}
	return out
}

func powerSupplyRedpoolManufactureLabels(rp *redpoolext.RedpoolPowerSupplyExt) (week, year, mdate, fru, ecl string) {
	if rp == nil {
		return "", "", "", "", ""
	}
	if rp.ManufactureWeek != nil {
		week = fmt.Sprintf("%d", *rp.ManufactureWeek)
	}
	if rp.ManufactureYear != nil {
		year = fmt.Sprintf("%d", *rp.ManufactureYear)
	}
	mdate = strings.TrimSpace(rp.ManufactureDate)
	fru = strings.TrimSpace(rp.FruPartNumber)
	ecl = strings.TrimSpace(rp.EngineeringChangeLevel)
	return week, year, mdate, fru, ecl
}

// collectChassisPower emits power / PSU gauges for one chassis.
func (c *Collector) collectChassisPower(ch *redfish.Chassis) {
	if c.Disabled("power") {
		return
	}
	chassisID := ch.ID
	pow, err := ch.Power()
	if err != nil || pow == nil {
		return
	}
	cov := c.coverageFor(chassisID)

	var rpByIndex []*redpoolext.RedpoolPowerSupplyExt
	if cli := ch.GetClient(); cli != nil && pow.ODataID != "" {
		if body, ok := getRedfishResourceJSON(cli, pow.ODataID); ok {
			rpByIndex = redpoolManufacturePerPowerSupplyFromPowerJSON(body)
		}
	}

	for _, pc := range pow.PowerControl {
		watts := pc.PowerConsumedWatts
		if watts == 0 && pc.PowerMetrics.IntervalInMin > 0 {
			watts = pc.PowerMetrics.AverageConsumedWatts
		}
		pcName := pc.Name
		if pcName == "" {
			pcName = pc.MemberID
		}
		cov.markPowerControl(pcName)
		pcLabels := map[string]string{
			"chassis_id":         chassisID,
			"power_control_name": pcName,
		}
		c.setGauge("redfish_powersupply_total_consumed_watts", pcLabels, float64(watts))
		if watts != 0 {
			cov.markTotalConsumed()
		}
		if pc.PowerLimit.LimitInWatts > 0 {
			c.setGauge("redfish_powersupply_power_limit_watts", pcLabels, float64(pc.PowerLimit.LimitInWatts))
		}
		if pc.PowerMetrics.IntervalInMin > 0 {
			if pc.PowerMetrics.MinConsumedWatts > 0 {
				c.setGauge("redfish_powersupply_min_consumed_watts", pcLabels, float64(pc.PowerMetrics.MinConsumedWatts))
			}
			if pc.PowerMetrics.MaxConsumedWatts > 0 {
				c.setGauge("redfish_powersupply_max_consumed_watts", pcLabels, float64(pc.PowerMetrics.MaxConsumedWatts))
			}
			if pc.PowerMetrics.AverageConsumedWatts > 0 {
				c.setGauge("redfish_powersupply_average_consumed_watts", pcLabels, float64(pc.PowerMetrics.AverageConsumedWatts))
			}
		}
	}
	for i, ps := range pow.PowerSupplies {
		psName := ps.Name
		if psName == "" {
			psName = ps.MemberID
		}
		health := statusStr(string(ps.Status.Health))
		state := statusStr(string(ps.Status.State))

		var mweek, myear, mdate, fru, ecl string
		if i < len(rpByIndex) {
			mweek, myear, mdate, fru, ecl = powerSupplyRedpoolManufactureLabels(rpByIndex[i])
		}

		c.setGauge("redfish_powersupply_info", map[string]string{
			"chassis_id":               chassisID,
			"name":                     psName,
			"member_id":                ps.MemberID,
			"manufacturer":             strings.TrimRight(ps.Manufacturer, " "),
			"model":                    strings.TrimRight(ps.Model, " "),
			"part_number":              strings.TrimRight(ps.PartNumber, " "),
			"fru_part_number":          fru,
			"psu_serial_number":        strings.TrimRight(ps.SerialNumber, " "),
			"power_supply_type":        string(ps.PowerSupplyType),
			"firmware_version":         strings.TrimRight(ps.FirmwareVersion, " "),
			"hot_pluggable":            strconv.FormatBool(ps.HotPluggable),
			"manufacture_week":         mweek,
			"manufacture_year":         myear,
			"manufacture_date":         mdate,
			"engineering_change_level": ecl,
		}, 1)
		if ps.LineInputVoltage > 0 {
			c.setGauge("redfish_powersupply_input_volts", map[string]string{
				"chassis_id": chassisID,
				"name":       psName,
			}, float64(ps.LineInputVoltage))
		}
		if ps.PowerInputWatts > 0 {
			c.setGauge("redfish_powersupply_input_watts", map[string]string{
				"chassis_id": chassisID,
				"name":       psName,
			}, float64(ps.PowerInputWatts))
		}
		outWatts := ps.LastPowerOutputWatts
		if outWatts == 0 {
			outWatts = ps.PowerOutputWatts
		}
		if outWatts > 0 {
			c.setGauge("redfish_powersupply_output_watts", map[string]string{
				"chassis_id": chassisID,
				"name":       psName,
			}, float64(outWatts))
		}
		c.setGauge("redfish_powersupply_health_info", map[string]string{
			"chassis_id": chassisID,
			"name":       psName,
			"health":     health,
		}, 1)
		c.setGauge("redfish_powersupply_state_info", map[string]string{
			"chassis_id": chassisID,
			"name":       psName,
			"state":      state,
		}, 1)
		if ps.EfficiencyPercent > 0 {
			c.setGauge("redfish_powersupply_efficiency_percent", map[string]string{
				"chassis_id": chassisID,
				"name":       psName,
			}, float64(ps.EfficiencyPercent))
		}
		if ps.PowerCapacityWatts > 0 {
			c.setGauge("redfish_powersupply_power_capacity_watts", map[string]string{
				"chassis_id": chassisID,
				"name":       psName,
			}, float64(ps.PowerCapacityWatts))
		}
	}
	for _, v := range pow.Voltages {
		vName := v.Name
		if vName == "" {
			vName = v.MemberID
		}
		vLabels := map[string]string{
			"chassis_id": chassisID,
			"name":       vName,
		}
		c.setGauge("redfish_power_voltage_output", vLabels, float64(v.ReadingVolts))
		// Omit when zero so absent BMC fields do not create useless series.
		if v.LowerThresholdCritical != 0 {
			c.setGauge("redfish_power_voltage_lower_threshold_critical", vLabels, float64(v.LowerThresholdCritical))
		}
		if v.LowerThresholdNonCritical != 0 {
			c.setGauge("redfish_power_voltage_lower_threshold_noncritical", vLabels, float64(v.LowerThresholdNonCritical))
		}
		if v.LowerThresholdFatal != 0 {
			c.setGauge("redfish_power_voltage_lower_threshold_fatal", vLabels, float64(v.LowerThresholdFatal))
		}
		if v.UpperThresholdCritical != 0 {
			c.setGauge("redfish_power_voltage_upper_threshold_critical", vLabels, float64(v.UpperThresholdCritical))
		}
		if v.UpperThresholdNonCritical != 0 {
			c.setGauge("redfish_power_voltage_upper_threshold_noncritical", vLabels, float64(v.UpperThresholdNonCritical))
		}
		if v.UpperThresholdFatal != 0 {
			c.setGauge("redfish_power_voltage_upper_threshold_fatal", vLabels, float64(v.UpperThresholdFatal))
		}
		if v.MinReadingRange != 0 {
			c.setGauge("redfish_power_voltage_min_reading_range", vLabels, float64(v.MinReadingRange))
		}
		if v.MaxReadingRange != 0 {
			c.setGauge("redfish_power_voltage_max_reading_range", vLabels, float64(v.MaxReadingRange))
		}
		vHealth := statusStr(string(v.Status.Health))
		vState := statusStr(string(v.Status.State))
		c.setGauge("redfish_power_voltage_info", vLabels, 1)
		c.setGauge("redfish_power_voltage_health_info", map[string]string{
			"chassis_id": chassisID,
			"name":       vName,
			"health":     vHealth,
		}, 1)
		c.setGauge("redfish_power_voltage_state_info", map[string]string{
			"chassis_id": chassisID,
			"name":       vName,
			"state":      vState,
		}, 1)
	}
	for i, r := range pow.Redundancy {
		redName := r.Name
		if redName == "" {
			redName = r.MemberID
		}
		if redName == "" {
			redName = r.ID
		}
		redID := r.MemberID
		if redID == "" {
			redID = r.ID
		}
		mode := string(r.Mode)
		if mode == "" {
			mode = "Unknown"
		}
		memberCount := len(pow.RedundancySet(i))
		rHealth := statusStr(string(r.Status.Health))
		rState := statusStr(string(r.Status.State))
		c.setGauge("redfish_powersupply_redundancy_info", map[string]string{
			"chassis_id":         chassisID,
			"name":               redName,
			"redundancy_id":      redID,
			"mode":               mode,
			"redundancy_enabled": strconv.FormatBool(r.RedundancyEnabled),
			"min_num_needed":     strconv.Itoa(r.MinNumNeeded),
			"max_num_supported":  strconv.Itoa(r.MaxNumSupported),
			"member_count":       strconv.Itoa(memberCount),
		}, 1)
		c.setGauge("redfish_powersupply_redundancy_health_info", map[string]string{
			"chassis_id":    chassisID,
			"redundancy_id": redID,
			"health":        rHealth,
		}, 1)
		c.setGauge("redfish_powersupply_redundancy_state_info", map[string]string{
			"chassis_id":    chassisID,
			"redundancy_id": redID,
			"state":         rState,
		}, 1)
		for _, psu := range pow.RedundancySet(i) {
			psuName := psu.Name
			if psuName == "" {
				psuName = psu.MemberID
			}
			if psuName == "" {
				psuName = psu.ID
			}
			c.setGauge("redfish_powersupply_redundancy_member_info", map[string]string{
				"chassis_id":    chassisID,
				"redundancy_id": redID,
				"psu_name":      psuName,
			}, 1)
		}
	}
}
