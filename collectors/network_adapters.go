// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Asama Inc.

package collectors

import (
	"encoding/json"
	"errors"
	"math"
	"strconv"
	"strings"

	"github.com/asama-ai-labs/redfish-inventory-exporter/models"
	"github.com/asama-ai-labs/redfish-inventory-exporter/redpoolext"
	"github.com/stmcginnis/gofish/redfish"
)

// GetNetworkAdapterPortInventory walks Chassis → NetworkAdapters and their Ports.
func (c *Collector) GetNetworkAdapterPortInventory() (*models.NetworkAdapterPortInventory, error) {
	chassisList, err := c.client.Service.Chassis()
	if err != nil {
		return nil, err
	}
	var inv models.NetworkAdapterPortInventory
	var partial []error
	for _, ch := range chassisList {
		if ch == nil {
			continue
		}
		adapters, err := ch.NetworkAdapters()
		if !keepPartial("network adapters", err, &partial) || len(adapters) == 0 {
			continue
		}
		for _, ad := range adapters {
			if ad == nil {
				continue
			}
			info := models.ChassisNetworkAdapterInfo{
				ChassisID:       ch.ID,
				ID:              ad.ID,
				Name:            ad.Name,
				Manufacturer:    ad.Manufacturer,
				Model:           ad.Model,
				SerialNumber:    ad.SerialNumber,
				PartNumber:      ad.PartNumber,
				SKU:             ad.SKU,
				FirmwareVersion: firstNetworkAdapterFirmware(ad),
				UUID:            networkAdapterUUIDFromOEM(ad),
				Health:          statusStr(string(ad.Status.Health)),
				State:           statusStr(string(ad.Status.State)),
				HealthRollup:    statusStr(string(ad.Status.HealthRollup)),
			}
			if cli := ad.GetClient(); cli != nil && ad.ODataID != "" {
				if body, ok := getRedfishResourceJSON(cli, ad.ODataID); ok {
					rp := redpoolext.ParseRedpoolStringFields(body,
						"FruPartNumber", "ManufactureDate", "EngineeringChangeLevel")
					info.FruPartNumber = rp["FruPartNumber"]
					info.ManufactureDate = rp["ManufactureDate"]
					info.EngineeringChangeLevel = rp["EngineeringChangeLevel"]
				}
			}
			inv.Adapters = append(inv.Adapters, info)
			// Ports is the Redfish port collection: NetworkPorts is deprecated. gofish returns
			// the members it read together with an error when another member fails, so keep
			// what came back.
			ports, _ := ad.Ports()
			for _, p := range ports {
				if p == nil {
					continue
				}
				inv.Ports = append(inv.Ports, networkPortInfoFromPort(ch.ID, ad.ID, p))
			}
		}
	}
	return &inv, errors.Join(partial...)
}

func networkPortInfoFromPort(chassisID, adapterID string, p *redfish.Port) models.ChassisNetworkPortInfo {
	info := models.ChassisNetworkPortInfo{
		ChassisID:            chassisID,
		AdapterID:            adapterID,
		ID:                   p.ID,
		Name:                 p.Name,
		PhysicalPortNumber:   p.PortID,
		LinkStatus:           string(p.LinkStatus),
		ActiveLinkTechnology: string(p.LinkNetworkTechnology),
		Health:               statusStr(string(p.Status.Health)),
		State:                statusStr(string(p.Status.State)),
	}
	if len(p.Ethernet.AssociatedMACAddresses) > 0 {
		info.MACAddress = strings.TrimSpace(p.Ethernet.AssociatedMACAddresses[0])
	}
	if p.CurrentSpeedGbps > 0 {
		info.CurrentLinkSpeedMbps = mbpsFromGbps(p.CurrentSpeedGbps)
	}
	return info
}

func mbpsFromGbps(gbps float32) int {
	return int(math.Round(float64(gbps) * 1000))
}

// standardLinkStatus reports every port with Port.LinkStatus.
// NetworkPort Up/Down map to LinkUp/LinkDown. Values outside that set are omitted.
func standardLinkStatus(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "up", "linkup", "link up", "connected":
		return "LinkUp"
	case "down", "linkdown", "link down", "disconnected":
		return "LinkDown"
	case "nolink", "no link", "no_link":
		return "NoLink"
	case "starting":
		return "Starting"
	case "training", "linknegotiating", "link negotiating", "negotiating":
		return "Training"
	default:
		return ""
	}
}

func firstNetworkAdapterFirmware(a *redfish.NetworkAdapter) string {
	for i := range a.Controllers {
		v := a.Controllers[i].FirmwarePackageVersion
		if v != "" {
			return v
		}
	}
	return ""
}

// networkAdapterUUIDFromOEM reads Oem.Redpool.UUID (Lenovo) or Oem.*.UUID.
func networkAdapterUUIDFromOEM(a *redfish.NetworkAdapter) string {
	if a == nil || len(a.OEM) == 0 {
		return ""
	}
	var root map[string]json.RawMessage
	if err := json.Unmarshal(a.OEM, &root); err != nil {
		return ""
	}
	if rpRaw, ok := root["Redpool"]; ok && len(rpRaw) > 0 {
		var rp map[string]interface{}
		if err := json.Unmarshal(rpRaw, &rp); err == nil {
			if s, ok := rp["UUID"].(string); ok && strings.TrimSpace(s) != "" {
				return strings.TrimSpace(s)
			}
		}
	}
	for _, key := range []string{"Lenovo", "Hpe", "Dell"} {
		blockRaw, ok := root[key]
		if !ok || len(blockRaw) == 0 {
			continue
		}
		var block map[string]interface{}
		if err := json.Unmarshal(blockRaw, &block); err != nil {
			continue
		}
		if s, ok := block["UUID"].(string); ok && strings.TrimSpace(s) != "" {
			return strings.TrimSpace(s)
		}
	}
	return ""
}

// SetNetworkAdapterPortMetrics emits redfish_network_adapter_* and redfish_network_port_* gauges.
func (c *Collector) SetNetworkAdapterPortMetrics(inv *models.NetworkAdapterPortInventory) {
	if inv == nil {
		return
	}
	for _, a := range inv.Adapters {
		c.setGauge("redfish_network_adapter_info", map[string]string{
			"chassis_id":               a.ChassisID,
			"id":                       a.ID,
			"name":                     a.Name,
			"manufacturer":             a.Manufacturer,
			"model":                    a.Model,
			"serial_number":            a.SerialNumber,
			"part_number":              a.PartNumber,
			"sku":                      a.SKU,
			"firmware_version":         a.FirmwareVersion,
			"uuid":                     a.UUID,
			"fru_part_number":          a.FruPartNumber,
			"manufacture_date":         a.ManufactureDate,
			"engineering_change_level": a.EngineeringChangeLevel,
		}, 1)
		c.setGauge("redfish_network_adapter_health_info", map[string]string{
			"chassis_id":    a.ChassisID,
			"id":            a.ID,
			"health":        a.Health,
			"health_rollup": a.HealthRollup,
		}, 1)
		c.setGauge("redfish_network_adapter_state_info", map[string]string{
			"chassis_id": a.ChassisID,
			"id":         a.ID,
			"state":      a.State,
		}, 1)
	}
	for _, p := range inv.Ports {
		c.setGauge("redfish_network_port_info", map[string]string{
			"chassis_id":             p.ChassisID,
			"adapter_id":             p.AdapterID,
			"id":                     p.ID,
			"name":                   p.Name,
			"physical_port_number":   p.PhysicalPortNumber,
			"active_link_technology": p.ActiveLinkTechnology,
			"mac_address":            p.MACAddress,
		}, 1)
		c.setGauge("redfish_network_port_health_info", map[string]string{
			"chassis_id": p.ChassisID,
			"adapter_id": p.AdapterID,
			"id":         p.ID,
			"health":     p.Health,
		}, 1)
		c.setGauge("redfish_network_port_state_info", map[string]string{
			"chassis_id":              p.ChassisID,
			"adapter_id":              p.AdapterID,
			"id":                      p.ID,
			"state":                   p.State,
			"link_status":             standardLinkStatus(p.LinkStatus),
			"current_link_speed_mbps": strconv.Itoa(p.CurrentLinkSpeedMbps),
		}, 1)
	}
}
