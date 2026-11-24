// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Asama Inc.

package collectors

import (
	"regexp"
	"strings"

	"github.com/stmcginnis/gofish/redfish"
)

type chassisSensorCoverage struct {
	powerControlName          string
	hasTotalConsumed          bool
	emittedPowerUpperCritical bool
	pendingEnvironmentPower   *environmentPowerPending
}

type environmentPowerPending struct {
	reading       float64
	dataSourceURI string
	dataSourceRef string
}

var systemPowerConsumption = regexp.MustCompile(`(?i)(SystemBoardPwrConsumption|PW_Consumption|PwrConsumption)`)

func (c *Collector) coverageFor(chassisID string) *chassisSensorCoverage {
	if c.chassisSensorCoverage == nil {
		c.chassisSensorCoverage = make(map[string]*chassisSensorCoverage)
	}
	cov, ok := c.chassisSensorCoverage[chassisID]
	if !ok {
		cov = &chassisSensorCoverage{}
		c.chassisSensorCoverage[chassisID] = cov
	}
	return cov
}

func (cov *chassisSensorCoverage) markPowerControl(name string) {
	if cov.powerControlName == "" {
		cov.powerControlName = strings.TrimSpace(name)
	}
}

func (cov *chassisSensorCoverage) markTotalConsumed() {
	cov.hasTotalConsumed = true
}

func isSystemPowerConsumptionSensor(id, name string) bool {
	combined := id + " " + name
	return systemPowerConsumption.MatchString(combined)
}

func isSystemPowerConsumptionRef(ref string) bool {
	return systemPowerConsumption.MatchString(strings.TrimSpace(ref))
}

func (cov *chassisSensorCoverage) stageEnvironmentPowerWatts(reading float64, dataSourceURI, dataSourceRef string) {
	cov.pendingEnvironmentPower = &environmentPowerPending{
		reading:       reading,
		dataSourceURI: dataSourceURI,
		dataSourceRef: dataSourceRef,
	}
}

func (c *Collector) flushPendingEnvironmentPower(chassisID string) {
	if c.chassisSensorCoverage == nil {
		return
	}
	cov := c.chassisSensorCoverage[chassisID]
	if cov == nil || cov.pendingEnvironmentPower == nil {
		return
	}
	p := cov.pendingEnvironmentPower
	cov.pendingEnvironmentPower = nil
	if cov.hasTotalConsumed && isSystemPowerConsumptionRef(p.dataSourceRef) {
		return
	}
	c.setGauge("redfish_chassis_environment_power_watts", map[string]string{
		"chassis_id":      chassisID,
		"data_source_uri": p.dataSourceURI,
		"data_source_ref": p.dataSourceRef,
	}, p.reading)
}

func (c *Collector) migrateSystemPowerUpperCritical(chassisID string, cov *chassisSensorCoverage, s *redfish.Sensor, name string) {
	if cov == nil || s == nil || cov.emittedPowerUpperCritical {
		return
	}
	if !isSystemPowerConsumptionSensor(s.ID, name) || !cov.hasTotalConsumed {
		return
	}
	if cov.powerControlName == "" {
		return
	}
	upper := s.Thresholds.UpperCritical.Reading
	if upper <= 0 {
		return
	}
	c.setGauge("redfish_powersupply_power_upper_critical_watts", map[string]string{
		"chassis_id":         chassisID,
		"power_control_name": cov.powerControlName,
	}, float64(upper))
	cov.emittedPowerUpperCritical = true
}
