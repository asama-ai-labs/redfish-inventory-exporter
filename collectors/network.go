// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Asama Inc.

package collectors

import (
	"errors"
	"fmt"

	"github.com/asama-ai-labs/redfish-inventory-exporter/models"
)

func (c *Collector) GetNetworkInfo() (*models.NetworkInfo, error) {
	systems, err := c.client.Service.Systems()
	if err != nil || len(systems) == 0 {
		return nil, err
	}
	system := systems[0]

	var partial []error
	ethInterfaces, err := system.EthernetInterfaces()
	if !keepPartial("network", err, &partial) {
		return nil, err
	}

	var info models.NetworkInfo
	for _, eth := range ethInterfaces {
		netInterface := models.NetworkInterface{
			ID:               eth.ID,
			Name:             eth.Name,
			Description:      eth.Description,
			MacAddress:       eth.MACAddress,
			SpeedMbps:        eth.SpeedMbps,
			MTUSize:          eth.MTUSize,
			AutoNeg:          eth.AutoNeg,
			FullDuplex:       eth.FullDuplex,
			InterfaceEnabled: eth.InterfaceEnabled,
			LinkStatus:       string(eth.LinkStatus),
			Status:           string(eth.Status.State),
			Health:           statusStr(string(eth.Status.Health)),
			State:            statusStr(string(eth.Status.State)),
			HealthRollup:     statusStr(string(eth.Status.HealthRollup)),
			PermanentMACAddr: eth.PermanentMACAddress,
			HostName:         eth.HostName,
			FQDN:             eth.FQDN,
		}
		info.Interfaces = append(info.Interfaces, netInterface)
	}

	return &info, errors.Join(partial...)
}

// SetNetworkMetrics sets network interface gauges (used by collect.go).
func (c *Collector) SetNetworkMetrics(networkInfo *models.NetworkInfo) {
	if networkInfo != nil {
		for _, iface := range networkInfo.Interfaces {
			c.setGauge("redfish_network", map[string]string{
				"id":                    iface.ID,
				"name":                  iface.Name,
				"mac_address":           iface.MacAddress,
				"permanent_mac_address": iface.PermanentMACAddr,
				"hostname":              iface.HostName,
				"fqdn":                  iface.FQDN,
				"link_status":           iface.LinkStatus,
				"speed_mbps":            fmt.Sprintf("%d", iface.SpeedMbps),
				"interface_enabled":     fmt.Sprintf("%t", iface.InterfaceEnabled),
				"port_number":           "",
				"port_mac_address":      "",
				"port_link_status":      "",
			}, 1)
			c.setGauge("redfish_network_health_info", map[string]string{
				"id":     iface.ID,
				"health": iface.Health,
			}, 1)
			c.setGauge("redfish_network_state_info", map[string]string{
				"id":    iface.ID,
				"state": iface.State,
			}, 1)
		}
	}
}
