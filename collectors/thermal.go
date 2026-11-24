// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Asama Inc.

package collectors

import (
	"encoding/json"
	"strings"

	"github.com/asama-ai-labs/redfish-inventory-exporter/redpoolext"
	"github.com/stmcginnis/gofish/redfish"
)

// thermalRelatedByMember maps MemberID (then Name) → first RelatedItem @odata.id basename/path.
func thermalRelatedByMember(body []byte) (fans, temps map[string]string) {
	fans = map[string]string{}
	temps = map[string]string{}
	if len(body) == 0 {
		return fans, temps
	}
	var doc struct {
		Fans []struct {
			MemberID    string `json:"MemberId"`
			Name        string `json:"Name"`
			RelatedItem []struct {
				ODataID string `json:"@odata.id"`
			} `json:"RelatedItem"`
		} `json:"Fans"`
		Temperatures []struct {
			MemberID    string `json:"MemberId"`
			Name        string `json:"Name"`
			RelatedItem []struct {
				ODataID string `json:"@odata.id"`
			} `json:"RelatedItem"`
		} `json:"Temperatures"`
	}
	if err := json.Unmarshal(body, &doc); err != nil {
		return fans, temps
	}
	for _, f := range doc.Fans {
		ref := firstRelatedRef(f.RelatedItem)
		if ref == "" {
			continue
		}
		if f.MemberID != "" {
			fans[f.MemberID] = ref
		}
		if f.Name != "" {
			fans[f.Name] = ref
		}
	}
	for _, t := range doc.Temperatures {
		ref := firstRelatedRef(t.RelatedItem)
		if ref == "" {
			continue
		}
		if t.MemberID != "" {
			temps[t.MemberID] = ref
		}
		if t.Name != "" {
			temps[t.Name] = ref
		}
	}
	return fans, temps
}

func firstRelatedRef(items []struct {
	ODataID string `json:"@odata.id"`
}) string {
	for _, it := range items {
		id := strings.TrimSpace(it.ODataID)
		if id != "" {
			return id
		}
	}
	return ""
}

// fanSpeedPercentFromRPM derives fan speed percent from RPM reading and MaxReadingRange
// when the BMC reports RPM units but not percent (e.g. Intel Thermal.Fans).
func fanSpeedPercentFromRPM(rpm, maxRange float64) (float64, bool) {
	if maxRange <= 0 || rpm < 0 {
		return 0, false
	}
	return rpm / maxRange * 100, true
}

// thermalMemberSkipped reports whether a Thermal fan/temperature member should not be exported.
// Absent placeholders (HPE/Supermicro) and Disabled empty slots (Lenovo) are skipped.
func thermalMemberSkipped(state string) bool {
	switch state {
	case "Absent", "Disabled":
		return true
	default:
		return false
	}
}

// collectChassisThermal emits thermal gauges for one chassis.
func (c *Collector) collectChassisThermal(ch *redfish.Chassis) {
	if c.Disabled("thermal") {
		return
	}
	chassisID := ch.ID
	therm, err := ch.Thermal()
	if err != nil || therm == nil {
		return
	}
	state := statusStr(string(therm.Status.State))
	health := statusStr(string(therm.Status.Health))
	if health == "Unknown" && string(therm.Status.HealthRollup) != "" {
		health = statusStr(string(therm.Status.HealthRollup))
	}
	thermalURL := therm.ODataID
	if thermalURL == "" {
		thermalURL = "/redfish/v1/Chassis/" + chassisID + "/Thermal"
	}

	var relatedFans, relatedTemps map[string]string
	if cli := ch.GetClient(); cli != nil && therm.ODataID != "" {
		if body, ok := getRedfishResourceJSON(cli, therm.ODataID); ok {
			relatedFans, relatedTemps = thermalRelatedByMember(body)
		}
	}

	c.setGauge("redfish_thermal_summary_info", map[string]string{
		"chassis_id": chassisID,
		"url":        thermalURL,
	}, 1)
	c.setGauge("redfish_thermal_summary_health_info", map[string]string{
		"chassis_id": chassisID,
		"health":     health,
	}, 1)
	c.setGauge("redfish_thermal_summary_state_info", map[string]string{
		"chassis_id": chassisID,
		"state":      state,
	}, 1)
	for _, fan := range therm.Fans {
		if thermalMemberSkipped(string(fan.Status.State)) {
			continue
		}
		fanName := fan.Name
		if fanName == "" {
			fanName = fan.MemberID
		}
		relatedRef := relatedFans[fan.MemberID]
		if relatedRef == "" {
			relatedRef = relatedFans[fanName]
		}
		fanMetricLabels := map[string]string{
			"chassis_id": chassisID,
			"fan_name":   fanName,
		}
		fanInfoLabels := map[string]string{
			"chassis_id":       chassisID,
			"fan_name":         fanName,
			"physical_context": string(fan.PhysicalContext),
			"related_ref":      relatedRef,
		}
		if fan.ReadingUnits == redfish.PercentReadingUnits {
			c.setGauge("redfish_thermal_fan_speed_percent", fanMetricLabels, float64(fan.Reading))
		} else {
			c.setGauge("redfish_thermal_fan_speed_rpm", fanMetricLabels, float64(fan.Reading))
			if pct, ok := fanSpeedPercentFromRPM(float64(fan.Reading), float64(fan.MaxReadingRange)); ok {
				c.setGauge("redfish_thermal_fan_speed_percent", fanMetricLabels, pct)
			}
		}
		// Redfish ThermalFan: thresholds/ranges use the same units as Reading (RPM or Percent).
		// Omit when zero so absent BMC fields do not create useless series.
		if fan.LowerThresholdCritical != 0 {
			c.setGauge("redfish_thermal_fan_lower_threshold_critical", fanMetricLabels, float64(fan.LowerThresholdCritical))
		}
		if fan.LowerThresholdNonCritical != 0 {
			c.setGauge("redfish_thermal_fan_lower_threshold_noncritical", fanMetricLabels, float64(fan.LowerThresholdNonCritical))
		}
		if fan.LowerThresholdFatal != 0 {
			c.setGauge("redfish_thermal_fan_lower_threshold_fatal", fanMetricLabels, float64(fan.LowerThresholdFatal))
		}
		if fan.UpperThresholdCritical != 0 {
			c.setGauge("redfish_thermal_fan_upper_threshold_critical", fanMetricLabels, float64(fan.UpperThresholdCritical))
		}
		if fan.UpperThresholdNonCritical != 0 {
			c.setGauge("redfish_thermal_fan_upper_threshold_noncritical", fanMetricLabels, float64(fan.UpperThresholdNonCritical))
		}
		if fan.UpperThresholdFatal != 0 {
			c.setGauge("redfish_thermal_fan_upper_threshold_fatal", fanMetricLabels, float64(fan.UpperThresholdFatal))
		}
		if fan.MinReadingRange != 0 {
			c.setGauge("redfish_thermal_fan_min_reading_range", fanMetricLabels, float64(fan.MinReadingRange))
		}
		if fan.MaxReadingRange != 0 {
			c.setGauge("redfish_thermal_fan_max_reading_range", fanMetricLabels, float64(fan.MaxReadingRange))
		}
		fanHealth := statusStr(string(fan.Status.Health))
		fanState := statusStr(string(fan.Status.State))
		c.setGauge("redfish_thermal_fan_info", fanInfoLabels, 1)
		c.setGauge("redfish_thermal_fan_health_info", map[string]string{
			"chassis_id": chassisID,
			"fan_name":   fanName,
			"health":     fanHealth,
		}, 1)
		c.setGauge("redfish_thermal_fan_state_info", map[string]string{
			"chassis_id": chassisID,
			"fan_name":   fanName,
			"state":      fanState,
		}, 1)
	}
	for _, temp := range therm.Temperatures {
		if thermalMemberSkipped(string(temp.Status.State)) {
			continue
		}
		sensorName := temp.Name
		if sensorName == "" {
			sensorName = temp.MemberID
		}
		relatedRef := relatedTemps[temp.MemberID]
		if relatedRef == "" {
			relatedRef = relatedTemps[sensorName]
		}
		tempMetricLabels := map[string]string{
			"chassis_id":  chassisID,
			"sensor_name": sensorName,
		}
		tempInfoLabels := map[string]string{
			"chassis_id":       chassisID,
			"sensor_name":      sensorName,
			"physical_context": string(temp.PhysicalContext),
			"related_ref":      relatedRef,
		}
		c.setGauge("redfish_thermal_sensor_temperature", tempMetricLabels, float64(temp.ReadingCelsius))
		// Omit when zero so absent BMC fields do not create useless series.
		if temp.LowerThresholdCritical != 0 {
			c.setGauge("redfish_thermal_sensor_lower_threshold_critical", tempMetricLabels, float64(temp.LowerThresholdCritical))
		}
		if temp.LowerThresholdNonCritical != 0 {
			c.setGauge("redfish_thermal_sensor_lower_threshold_noncritical", tempMetricLabels, float64(temp.LowerThresholdNonCritical))
		}
		if temp.LowerThresholdFatal != 0 {
			c.setGauge("redfish_thermal_sensor_lower_threshold_fatal", tempMetricLabels, float64(temp.LowerThresholdFatal))
		}
		if temp.UpperThresholdCritical != 0 {
			c.setGauge("redfish_thermal_sensor_upper_threshold_critical", tempMetricLabels, float64(temp.UpperThresholdCritical))
		}
		if temp.UpperThresholdNonCritical != 0 {
			c.setGauge("redfish_thermal_sensor_upper_threshold_noncritical", tempMetricLabels, float64(temp.UpperThresholdNonCritical))
		}
		if temp.UpperThresholdFatal != 0 {
			c.setGauge("redfish_thermal_sensor_upper_threshold_fatal", tempMetricLabels, float64(temp.UpperThresholdFatal))
		}
		if temp.MinReadingRangeTemp != 0 {
			c.setGauge("redfish_thermal_sensor_min_reading_range", tempMetricLabels, float64(temp.MinReadingRangeTemp))
		}
		if temp.MaxReadingRangeTemp != 0 {
			c.setGauge("redfish_thermal_sensor_max_reading_range", tempMetricLabels, float64(temp.MaxReadingRangeTemp))
		}
		tHealth := statusStr(string(temp.Status.Health))
		tState := statusStr(string(temp.Status.State))
		c.setGauge("redfish_thermal_sensor_info", tempInfoLabels, 1)
		c.setGauge("redfish_thermal_sensor_health_info", map[string]string{
			"chassis_id":  chassisID,
			"sensor_name": sensorName,
			"health":      tHealth,
		}, 1)
		c.setGauge("redfish_thermal_sensor_state_info", map[string]string{
			"chassis_id":  chassisID,
			"sensor_name": sensorName,
			"state":       tState,
		}, 1)
	}

	c.collectChassisFanPWM(ch)
}

// oemFloatByKeys walks Oem.<vendor> maps for the first matching numeric key.
func oemFloatByKeys(oem json.RawMessage, keys ...string) (float64, bool) {
	if len(oem) == 0 {
		return 0, false
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(oem, &top); err != nil {
		return 0, false
	}
	for _, block := range top {
		var fields map[string]any
		if err := json.Unmarshal(block, &fields); err != nil {
			continue
		}
		for _, key := range keys {
			v, ok := fields[key]
			if !ok || v == nil {
				continue
			}
			switch n := v.(type) {
			case float64:
				return n, true
			case json.Number:
				f, err := n.Float64()
				if err == nil {
					return f, true
				}
			}
		}
	}
	return 0, false
}

// fanPWMReading prefers Oem.Redpool, then a top-level number, then other vendor OEM.
func fanPWMReading(body []byte, top *float64, oem json.RawMessage, key string) (float64, bool) {
	if v, ok := redpoolext.ParseRedpoolFloatField(body, key); ok {
		return v, true
	}
	if top != nil {
		return *top, true
	}
	return oemFloatByKeys(oem, key)
}

// collectChassisFanPWM emits average and per-fan PWM from Oem.Redpool.
// Direct BMC payloads still match a top-level number or vendor OEM.
func (c *Collector) collectChassisFanPWM(ch *redfish.Chassis) {
	cli := ch.GetClient()
	if cli == nil {
		return
	}
	chassisID := ch.ID
	root := strings.TrimSuffix(ch.ODataID, "/")
	if root == "" {
		root = "/redfish/v1/Chassis/" + chassisID
	}

	if body, ok := getRedfishResourceJSON(cli, root+"/EnvironmentMetrics"); ok {
		var doc struct {
			AverageFanPWM *float64 `json:"AverageFanPWM"`
			PowerWatts    *struct {
				Reading       *float64 `json:"Reading"`
				DataSourceUri string   `json:"DataSourceUri"`
			} `json:"PowerWatts"`
			TemperatureCelsius *struct {
				Reading       *float64 `json:"Reading"`
				DataSourceUri string   `json:"DataSourceUri"`
			} `json:"TemperatureCelsius"`
			Oem json.RawMessage `json:"Oem"`
		}
		if err := json.Unmarshal(body, &doc); err == nil {
			pwm, found := fanPWMReading(body, doc.AverageFanPWM, doc.Oem, "AverageFanPWM")
			if found {
				c.setGauge("redfish_thermal_average_fan_pwm", map[string]string{
					"chassis_id": chassisID,
				}, pwm)
			}
			if doc.PowerWatts != nil && doc.PowerWatts.Reading != nil {
				dataSourceURI := doc.PowerWatts.DataSourceUri
				dataSourceRef := dataSourceURI
				if i := strings.LastIndex(dataSourceRef, "/"); i >= 0 {
					dataSourceRef = dataSourceRef[i+1:]
				}
				c.coverageFor(chassisID).stageEnvironmentPowerWatts(
					*doc.PowerWatts.Reading,
					dataSourceURI,
					dataSourceRef,
				)
			}
			if doc.TemperatureCelsius != nil && doc.TemperatureCelsius.Reading != nil {
				ds := doc.TemperatureCelsius.DataSourceUri
				if i := strings.LastIndex(ds, "/"); i >= 0 {
					ds = ds[i+1:]
				}
				c.setGauge("redfish_chassis_environment_temperature_celsius", map[string]string{
					"chassis_id":      chassisID,
					"data_source_uri": doc.TemperatureCelsius.DataSourceUri,
					"data_source_ref": ds,
				}, *doc.TemperatureCelsius.Reading)
			}
		}
	}

	if body, ok := getRedfishResourceJSON(cli, root+"/ThermalSubsystem/Fans"); ok {
		var coll struct {
			Members []struct {
				ODataID string `json:"@odata.id"`
			} `json:"Members"`
		}
		if err := json.Unmarshal(body, &coll); err != nil {
			return
		}
		for _, m := range coll.Members {
			if m.ODataID == "" {
				continue
			}
			fb, ok := getRedfishResourceJSON(cli, m.ODataID)
			if !ok {
				continue
			}
			var fan struct {
				ID     string          `json:"Id"`
				Name   string          `json:"Name"`
				FanPWM *float64        `json:"FanPWM"`
				Oem    json.RawMessage `json:"Oem"`
			}
			if err := json.Unmarshal(fb, &fan); err != nil {
				continue
			}
			pwm, found := fanPWMReading(fb, fan.FanPWM, fan.Oem, "FanPWM")
			if !found {
				continue
			}
			fanName := fan.Name
			if fanName == "" {
				fanName = fan.ID
			}
			c.setGauge("redfish_thermal_fan_speed_percent", map[string]string{
				"chassis_id": chassisID,
				"fan_name":   fanName,
			}, pwm)
		}
	}
}
