// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Asama Inc.

package collectors

import (
	"github.com/asama-ai-labs/redfish-inventory-exporter/models"
)

func (c *Collector) GetBMCManagerInfo() (*models.BMCManagerInfo, error) {
	managers, err := c.client.Service.Managers()
	if err != nil || len(managers) == 0 {
		return nil, err
	}
	manager := managers[0]

	info := &models.BMCManagerInfo{
		ID:               manager.ID,
		Name:             manager.Name,
		Model:            manager.Model,
		Manufacturer:     manager.Manufacturer,
		FirmwareVersion:  manager.FirmwareVersion,
		UUID:             manager.UUID,
		BMCManagerState:  string(manager.Status.State),
		BMCManagerHealth: string(manager.Status.Health),
	}

	return info, nil
}

// SetBMCMetrics sets BMC manager gauges (used by collect.go).
func (c *Collector) SetBMCMetrics(bmcInfo *models.BMCManagerInfo) {
	if bmcInfo != nil {
		bmcHealth := statusStr(bmcInfo.BMCManagerHealth)
		bmcState := statusStr(bmcInfo.BMCManagerState)
		c.setGauge("redfish_bmc_manager_info", map[string]string{
			"id":               bmcInfo.ID,
			"name":             bmcInfo.Name,
			"model":            bmcInfo.Model,
			"manufacturer":     bmcInfo.Manufacturer,
			"firmware_version": bmcInfo.FirmwareVersion,
			"uuid":             bmcInfo.UUID,
		}, 1)
		c.setGauge("redfish_bmc_manager_health_info", map[string]string{
			"id":     bmcInfo.ID,
			"health": bmcHealth,
		}, 1)
		c.setGauge("redfish_bmc_manager_state_info", map[string]string{
			"id":    bmcInfo.ID,
			"state": bmcState,
		}, 1)
	}
}
