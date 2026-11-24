// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Asama Inc.

package collectors

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/asama-ai-labs/redfish-inventory-exporter/models"
	"github.com/stmcginnis/gofish/redfish"
)

func (c *Collector) GetPCIeInfo() (*models.PCIeInfo, error) {
	var info models.PCIeInfo
	seen := map[string]bool{}

	appendDevices := func(devices []*redfish.PCIeDevice) {
		sort.SliceStable(devices, func(i, j int) bool {
			return devices[i].ID < devices[j].ID
		})
		for _, d := range devices {
			if d == nil || d.ID == "" || seen[d.ID] {
				continue
			}
			dev, ok := pcieDeviceFromRedfish(d)
			if !ok {
				continue
			}
			seen[d.ID] = true
			info.Devices = append(info.Devices, dev)
		}
	}

	var partial []error
	if systems, err := c.client.Service.Systems(); err == nil && len(systems) > 0 {
		devices, err := systems[0].PCIeDevices()
		if keepPartial("pcie", err, &partial) {
			appendDevices(devices)
		}
	}
	chassisList, chErr := c.client.Service.Chassis()
	if keepPartial("pcie chassis", chErr, &partial) {
		for _, ch := range chassisList {
			if ch == nil {
				continue
			}
			devices, err := ch.PCIeDevices()
			if !keepPartial("pcie", err, &partial) || len(devices) == 0 {
				continue
			}
			appendDevices(devices)
		}
	}

	return &info, errors.Join(partial...)
}

func pcieDeviceFromRedfish(d *redfish.PCIeDevice) (models.PCIeDevice, bool) {
	location := strings.TrimSpace(d.Slot.Location.Info)
	if location == "" && d.Slot.Location.PartLocation.ServiceLabel != "" {
		location = strings.TrimSpace(d.Slot.Location.PartLocation.ServiceLabel)
	}

	device := models.PCIeDevice{
		ID:              d.ID,
		FirmwareVersion: strings.TrimSpace(d.FirmwareVersion),
		Manufacturer:    strings.TrimSpace(d.Manufacturer),
		Model:           strings.TrimSpace(d.Model),
		StatusHealth:    statusStr(string(d.Status.Health)),
		StatusState:     statusStr(string(d.Status.State)),
		Location:        location,
		Name:            strings.TrimSpace(d.Name),
		PCIeType:        strings.TrimSpace(string(d.PCIeInterface.PCIeType)),
		MaxPCIeType:     strings.TrimSpace(string(d.PCIeInterface.MaxPCIeType)),
		LanesInUse:      d.PCIeInterface.LanesInUse,
		MaxLanes:        d.PCIeInterface.MaxLanes,
	}

	functions, err := d.PCIeFunctions()
	if err == nil && len(functions) > 0 {
		sort.SliceStable(functions, func(i, j int) bool {
			a, b := functions[i], functions[j]
			if a == nil && b == nil {
				return false
			}
			if a == nil {
				return false
			}
			if b == nil {
				return true
			}
			if a.FunctionID != b.FunctionID {
				return a.FunctionID < b.FunctionID
			}
			return a.Name < b.Name
		})
		var f *redfish.PCIeFunction
		for _, fn := range functions {
			if fn != nil {
				f = fn
				break
			}
		}
		if f != nil {
			physicalCount := 0
			for _, fn := range functions {
				if fn != nil && strings.EqualFold(string(fn.FunctionType), "Physical") {
					physicalCount++
				}
			}
			if physicalCount == 0 {
				physicalCount = len(functions)
			}
			device.DeviceID = pciIDToInt(f.DeviceID)
			device.SubsystemDeviceID = pciIDToInt(f.SubsystemID)
			device.SubsystemVendorID = pciIDToInt(f.SubsystemVendorID)
			device.VendorID = pciIDToInt(f.VendorID)
			device.ClassCode = f.ClassCode
			device.DeviceClass = string(f.DeviceClass)
			if strings.TrimSpace(f.Name) != "" {
				device.Name = f.Name
			}
			device.FunctionCount = physicalCount
		}
	}

	// Emit inventory even when PCIeFunctions is empty (e.g. Supermicro NVMe stubs).
	if device.Name == "" {
		device.Name = d.ID
	}
	return device, true
}

// SetPCIeMetrics sets PCIe device gauges (used by collect.go).
func (c *Collector) SetPCIeMetrics(pcieInfo *models.PCIeInfo) {
	if pcieInfo != nil {
		for _, device := range pcieInfo.Devices {
			c.setGauge("redfish_pcie_device_info", map[string]string{
				"id":                  device.ID,
				"device_id":           fmt.Sprintf("%d", device.DeviceID),
				"subsystem_device_id": fmt.Sprintf("%d", device.SubsystemDeviceID),
				"subsystem_vendor_id": fmt.Sprintf("%d", device.SubsystemVendorID),
				"vendor_id":           fmt.Sprintf("%d", device.VendorID),
				"class_code":          device.ClassCode,
				"device_class":        device.DeviceClass,
				"name":                device.Name,
				"function_count":      fmt.Sprintf("%d", device.FunctionCount),
				"firmware_version":    device.FirmwareVersion,
				"manufacturer":        device.Manufacturer,
				"model":               device.Model,
				"location":            device.Location,
				"pcie_type":           device.PCIeType,
				"max_pcie_type":       device.MaxPCIeType,
				"lanes_in_use":        fmt.Sprintf("%d", device.LanesInUse),
				"max_lanes":           fmt.Sprintf("%d", device.MaxLanes),
			}, 1)
			c.setGauge("redfish_pcie_device_health", map[string]string{
				"id":     device.ID,
				"health": device.StatusHealth,
			}, 1)
			c.setGauge("redfish_pcie_device_state", map[string]string{
				"id":    device.ID,
				"state": device.StatusState,
			}, 1)
		}
	}
}
