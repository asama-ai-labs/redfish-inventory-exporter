// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Asama Inc.

package collectors

import (
	"strings"

	"github.com/stmcginnis/gofish/redfish"
)

// sensorHasReadingOrThreshold reports whether a sensor carries a nonzero reading or any
// nonzero threshold, so stub/placeholder sensors (no data at all) are skipped to keep
// series volume reasonable. Intrusion/discrete sensors often read 0 while healthy —
// still export them when Id/Name indicates intrusion.
func sensorHasReadingOrThreshold(s *redfish.Sensor) bool {
	if s.Reading != 0 || s.ReadingRangeMin != 0 || s.ReadingRangeMax != 0 {
		return true
	}
	th := s.Thresholds
	if th.UpperCritical.Reading != 0 ||
		th.UpperCaution.Reading != 0 ||
		th.UpperFatal.Reading != 0 ||
		th.LowerCritical.Reading != 0 ||
		th.LowerCaution.Reading != 0 ||
		th.LowerFatal.Reading != 0 {
		return true
	}
	id := strings.ToLower(s.ID + " " + s.Name)
	return strings.Contains(id, "intrusion")
}

func sensorIsIntrusion(s *redfish.Sensor, name string) bool {
	return strings.Contains(strings.ToLower(s.ID+" "+name), "intrusion")
}

func (c *Collector) emitChassisIntrusionState(chassisID string, s *redfish.Sensor, name string) {
	intrusion := "Normal"
	if strings.EqualFold(string(s.Status.Health), "Critical") ||
		strings.EqualFold(string(s.Status.Health), "Warning") {
		intrusion = string(s.Status.Health)
	} else if s.Reading != 0 {
		intrusion = "HardwareIntrusion"
	}
	c.setGauge("redfish_chassis_intrusion_state", map[string]string{
		"id":               chassisID,
		"intrusion_sensor": intrusion,
	}, 1)
}

// collectChassisSensors emits Chassis/Sensors gauges (reading, ranges, thresholds).
func (c *Collector) collectChassisSensors(ch *redfish.Chassis) {
	if c.Disabled("sensors") {
		return
	}
	chassisID := ch.ID
	cov := c.coverageFor(chassisID)
	sensors, err := ch.Sensors()
	if err != nil || len(sensors) == 0 {
		return
	}
	for _, s := range sensors {
		if s == nil || !sensorHasReadingOrThreshold(s) {
			continue
		}
		name := s.Name
		if name == "" {
			name = s.ID
		}
		intrusion := sensorIsIntrusion(s, name)
		labels := map[string]string{
			"chassis_id":       chassisID,
			"sensor_id":        s.ID,
			"sensor_name":      name,
			"reading_type":     string(s.ReadingType),
			"reading_units":    s.ReadingUnits,
			"physical_context": string(s.PhysicalContext),
		}
		c.migrateSystemPowerUpperCritical(chassisID, cov, s, name)
		c.setGauge("redfish_chassis_sensor_info", labels, 1)
		c.setGauge("redfish_chassis_sensor_reading", labels, float64(s.Reading))
		if s.ReadingRangeMin != 0 {
			c.setGauge("redfish_chassis_sensor_min_reading_range", labels, float64(s.ReadingRangeMin))
		}
		if s.ReadingRangeMax != 0 {
			c.setGauge("redfish_chassis_sensor_max_reading_range", labels, float64(s.ReadingRangeMax))
		}
		c.emitChassisSensorThresholds(labels, s)
		c.setGauge("redfish_chassis_sensor_health_info", map[string]string{
			"chassis_id":  chassisID,
			"sensor_id":   s.ID,
			"sensor_name": name,
			"health":      statusStr(string(s.Status.Health)),
		}, 1)
		c.setGauge("redfish_chassis_sensor_state_info", map[string]string{
			"chassis_id":  chassisID,
			"sensor_id":   s.ID,
			"sensor_name": name,
			"state":       statusStr(string(s.Status.State)),
		}, 1)
		if intrusion {
			c.emitChassisIntrusionState(chassisID, s, name)
		}
	}
}

func (c *Collector) emitChassisSensorThresholds(labels map[string]string, s *redfish.Sensor) {
	th := s.Thresholds
	if th.UpperCritical.Reading != 0 {
		c.setGauge("redfish_chassis_sensor_upper_threshold_critical", labels, float64(th.UpperCritical.Reading))
	}
	if th.UpperCaution.Reading != 0 {
		c.setGauge("redfish_chassis_sensor_upper_threshold_noncritical", labels, float64(th.UpperCaution.Reading))
	}
	if th.UpperFatal.Reading != 0 {
		c.setGauge("redfish_chassis_sensor_upper_threshold_fatal", labels, float64(th.UpperFatal.Reading))
	}
	if th.LowerCritical.Reading != 0 {
		c.setGauge("redfish_chassis_sensor_lower_threshold_critical", labels, float64(th.LowerCritical.Reading))
	}
	if th.LowerCaution.Reading != 0 {
		c.setGauge("redfish_chassis_sensor_lower_threshold_noncritical", labels, float64(th.LowerCaution.Reading))
	}
	if th.LowerFatal.Reading != 0 {
		c.setGauge("redfish_chassis_sensor_lower_threshold_fatal", labels, float64(th.LowerFatal.Reading))
	}
}
