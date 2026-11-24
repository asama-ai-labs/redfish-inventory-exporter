// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Asama Inc.

package collectors

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/asama-ai-labs/redfish-inventory-exporter/models"
	"github.com/asama-ai-labs/redfish-inventory-exporter/redpoolext"
	"github.com/stmcginnis/gofish/common"
	"github.com/stmcginnis/gofish/redfish"
)

// memoryTypeLabel is SMBIOS-style technology (DDR4, DDR5, …). Redfish MemoryType is a coarse
// class (often "DRAM" on empty HPE slots); it is not used here — return "" when device type
// is unknown so the metric stays DDR*-only or empty.
func memoryTypeLabel(m *redfish.Memory) string {
	if m == nil {
		return ""
	}
	if m.MemoryDeviceType != "" {
		return string(m.MemoryDeviceType)
	}
	return ""
}

// ratedSpeedMHz returns max(AllowedSpeedsMHz) when present, else configured OperatingSpeedMhz.
func ratedSpeedMHz(m *redfish.Memory) int {
	if m == nil {
		return 0
	}
	max := 0
	for _, v := range m.AllowedSpeedsMHz {
		if v > max {
			max = v
		}
	}
	if max > 0 {
		return max
	}
	return m.OperatingSpeedMhz
}

// errorCorrectionLabel is the BMC's own ErrorCorrection, or "Unknown" when it sent none.
// A bus wider than the data path shows that check bits exist, not which scheme corrects them.
func errorCorrectionLabel(m *redfish.Memory) string {
	if m == nil {
		return "Unknown"
	}
	if s := string(m.ErrorCorrection); s != "" {
		return s
	}
	return "Unknown"
}

func memoryDeviceLocator(m *redfish.Memory) string {
	if m == nil {
		return ""
	}
	if s := strings.TrimSpace(m.DeviceLocator); s != "" {
		return s
	}
	if s := strings.TrimSpace(m.Location.PartLocation.ServiceLabel); s != "" {
		return s
	}
	return ""
}

func memoryEnabledLabel(m *redfish.Memory) string {
	if m == nil {
		return ""
	}
	if m.Enabled {
		return "true"
	}
	st := strings.ToLower(string(m.Status.State))
	if st == "enabled" && m.CapacityMiB > 0 {
		// Many BMCs omit Enabled; treat populated + Enabled state as active.
		return "true"
	}
	if st == "disabled" {
		return "false"
	}
	return ""
}

func memoryMediaCSV(m *redfish.Memory) string {
	if m == nil || len(m.MemoryMedia) == 0 {
		return ""
	}
	var b strings.Builder
	for _, mm := range m.MemoryMedia {
		s := strings.TrimSpace(string(mm))
		if s == "" {
			continue
		}
		if b.Len() > 0 {
			b.WriteString(",")
		}
		b.WriteString(s)
	}
	return b.String()
}

func optionalIntPromLabel(p *int) string {
	if p == nil {
		return "unknown"
	}
	return fmt.Sprintf("%d", *p)
}

func memoryRedpoolLabels(rp *redpoolext.RedpoolMemoryExt) (week, year, mdate, fru, ecc string) {
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
	if rp.EccBits != nil {
		ecc = fmt.Sprintf("%d", *rp.EccBits)
	}
	return week, year, mdate, fru, ecc
}

func (c *Collector) GetMemoryInfo() (*models.MemoryInfo, error) {
	systems, err := c.client.Service.Systems()
	if err != nil {
		return nil, err
	}
	if len(systems) == 0 {
		return nil, nil
	}
	system := systems[0]

	var partial []error
	memory, err := system.Memory()
	if !keepPartial("memory", err, &partial) {
		return nil, err
	}

	info := &models.MemoryInfo{
		TotalSystemMemoryGiB: system.MemorySummary.TotalSystemMemoryGiB,
	}

	for _, m := range memory {
		mdt := string(m.MemoryDeviceType)
		device := models.MemoryDevice{
			ID:                m.ID,
			CapacityMiB:       int(m.CapacityMiB),
			OperatingSpeedMHz: m.OperatingSpeedMhz,
			MaxRatedSpeedMHz:  ratedSpeedMHz(m),
			Manufacturer:      m.Manufacturer,
			PartNumber:        m.PartNumber,
			SerialNumber:      m.SerialNumber,
			MemoryType:        memoryTypeLabel(m),
			MemoryDeviceType:  mdt,
			ErrorCorrection:   errorCorrectionLabel(m),
			Health:            statusStr(string(m.Status.Health)),
			State:             statusStr(string(m.Status.State)),
			HealthRollup:      statusStr(string(m.Status.HealthRollup)),
			DeviceLocator:     memoryDeviceLocator(m),
			BaseModuleType:    strings.TrimSpace(string(m.BaseModuleType)),
			RankCount:         m.RankCount,
			BusWidthBits:      m.BusWidthBits,
			DataWidthBits:     m.DataWidthBits,
			MemoryMedia:       memoryMediaCSV(m),
			Enabled:           memoryEnabledLabel(m),
		}
		usedMemoryJSON := false
		if cli := m.GetClient(); cli != nil {
			if body, ok := readMemoryResourceBody(cli, m.ODataID); ok {
				device.LocSocket, device.LocMemoryController, device.LocChannel, device.LocSlot =
					parseMemoryLocationFromMemoryJSON(body)
				if ext, ok := redpoolext.ParseRedpoolMemoryExtFromJSON(body); ok {
					device.Redpool = &ext
				}
				usedMemoryJSON = true
			}
		}
		if !usedMemoryJSON {
			device.LocSocket, device.LocMemoryController, device.LocChannel, device.LocSlot =
				intPtrsFromMemoryLocation(m.MemoryLocation)
		}
		info.MemoryDevices = append(info.MemoryDevices, device)
	}

	return info, errors.Join(partial...)
}

func (c *Collector) GetMemorySummary() (*models.MemorySummary, error) {
	systems, err := c.client.Service.Systems()
	if err != nil || len(systems) == 0 {
		return nil, err
	}
	system := systems[0]

	memory, _ := system.Memory()
	var maxRated, maxOperating int
	var memoryType string

	for _, m := range memory {
		if m.OperatingSpeedMhz > maxOperating {
			maxOperating = m.OperatingSpeedMhz
		}
		if r := ratedSpeedMHz(m); r > maxRated {
			maxRated = r
		}
		if memoryType == "" {
			memoryType = memoryTypeLabel(m)
		}
	}

	errorCorrection := ""
	for _, m := range memory {
		if ec := errorCorrectionLabel(m); ec != "Unknown" {
			errorCorrection = ec
			break
		}
	}
	if errorCorrection == "" && len(memory) > 0 {
		errorCorrection = string(memory[0].ErrorCorrection)
	}

	summary := &models.MemorySummary{
		TotalSystemMemoryGiB: system.MemorySummary.TotalSystemMemoryGiB,
		TotalSystemMemoryMiB: int(system.MemorySummary.TotalSystemMemoryGiB * 1024),
		MemoryDeviceCount:    len(memory),
		MemoryMirroring:      string(system.MemorySummary.MemoryMirroring),
		OperatingSpeedMHz:    maxOperating,
		MaxOperatingSpeedMHz: maxRated,
		MemoryType:           memoryType,
		ErrorCorrection:      errorCorrection,
		Health:               statusStr(string(system.MemorySummary.Status.Health)),
		State:                statusStr(string(system.MemorySummary.Status.State)),
		HealthRollup:         statusStr(string(system.MemorySummary.Status.HealthRollup)),
	}
	return summary, nil
}

// SetMemoryMetrics sets memory and memory summary gauges (used by collect.go).
func (c *Collector) SetMemoryMetrics(memInfo *models.MemoryInfo, memSummary *models.MemorySummary) {
	if memInfo != nil {
		for _, device := range memInfo.MemoryDevices {
			memType := device.MemoryType
			errCorr := device.ErrorCorrection
			if errCorr == "" {
				errCorr = "Unknown"
			}
			maxRated := device.MaxRatedSpeedMHz
			if maxRated == 0 {
				maxRated = device.OperatingSpeedMHz
			}
			mweek, myear, mdate, fru, ecc := memoryRedpoolLabels(device.Redpool)
			c.setGauge("redfish_memory", map[string]string{
				"id":                      device.ID,
				"manufacturer":            device.Manufacturer,
				"part_number":             device.PartNumber,
				"device_serial_number":    device.SerialNumber,
				"capacity_mib":            fmt.Sprintf("%d", device.CapacityMiB),
				"operating_speed_mhz":     fmt.Sprintf("%d", device.OperatingSpeedMHz),
				"max_operating_speed_mhz": fmt.Sprintf("%d", maxRated),
				"memory_type":             memType,
				"error_correction":        errCorr,
				"manufacture_week":        mweek,
				"manufacture_year":        myear,
				"manufacture_date":        mdate,
				"fru_part_number":         fru,
				"ecc_bits":                ecc,
				"device_locator":          device.DeviceLocator,
				"base_module_type":        device.BaseModuleType,
				"rank_count":              fmt.Sprintf("%d", device.RankCount),
				"bus_width_bits":          fmt.Sprintf("%d", device.BusWidthBits),
				"data_width_bits":         fmt.Sprintf("%d", device.DataWidthBits),
				"loc_socket":              optionalIntPromLabel(device.LocSocket),
				"loc_imc":                 optionalIntPromLabel(device.LocMemoryController),
				"loc_channel":             optionalIntPromLabel(device.LocChannel),
				"loc_slot":                optionalIntPromLabel(device.LocSlot),
				"memory_media":            device.MemoryMedia,
				"enabled":                 device.Enabled,
			}, 1)
			c.setGauge("redfish_memory_health_info", map[string]string{
				"id":     device.ID,
				"health": device.Health,
			}, 1)
			c.setGauge("redfish_memory_state_info", map[string]string{
				"id":    device.ID,
				"state": device.State,
			}, 1)
		}
	}
	if memSummary != nil {
		c.setGauge("redfish_memory_summary_info", map[string]string{
			"total_system_memory_gib": fmt.Sprintf("%.2f", memSummary.TotalSystemMemoryGiB),
			"total_system_memory_mib": fmt.Sprintf("%d", memSummary.TotalSystemMemoryMiB),
			"device_count":            fmt.Sprintf("%d", memSummary.MemoryDeviceCount),
		}, 1)
		c.setGauge("redfish_memory_summary_health_info", map[string]string{
			"health": memSummary.Health,
		}, 1)
		c.setGauge("redfish_memory_summary_state_info", map[string]string{
			"state": memSummary.State,
		}, 1)
	}
}

// collectMemoryAlarmTrips emits MemoryMetrics HealthData.AlarmTrips as 0/1 gauges.
func (c *Collector) collectMemoryAlarmTrips() {
	systems, err := c.client.Service.Systems()
	if err != nil || len(systems) == 0 {
		return
	}
	memory, err := systems[0].Memory()
	if err != nil && !isPartial(err) {
		return
	}
	for _, m := range memory {
		if m == nil {
			continue
		}
		metrics, err := m.Metrics()
		if err != nil || metrics == nil {
			continue
		}
		trips := metrics.HealthData.AlarmTrips
		emit := func(tripType string, active bool) {
			v := 0.0
			if active {
				v = 1
			}
			c.setGauge("redfish_memory_alarm_trip", map[string]string{
				"id":        m.ID,
				"trip_type": tripType,
			}, v)
		}
		emit("CorrectableECCError", trips.CorrectableECCError)
		emit("UncorrectableECCError", trips.UncorrectableECCError)
		emit("Temperature", trips.Temperature)
		emit("SpareBlock", trips.SpareBlock)
		emit("AddressParityError", trips.AddressParityError)
	}
}

// readMemoryResourceBody performs a GET on a Memory @odata.id and returns the JSON body.
func readMemoryResourceBody(client common.Client, odataID string) ([]byte, bool) {
	if client == nil || strings.TrimSpace(odataID) == "" {
		return nil, false
	}
	resp, err := client.Get(odataID)
	if err != nil || resp == nil {
		return nil, false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, false
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, false
	}
	return body, true
}

// parseMemoryLocationFromMemoryJSON extracts MemoryLocation fields using pointer semantics:
//   - If the MemoryLocation property is absent or JSON null, all return values are nil.
//   - If the object is present, each field is nil only when that JSON property was omitted
//     (distinguishing omitted from numeric 0, which is returned as a non-nil pointer to 0).
func parseMemoryLocationFromMemoryJSON(body []byte) (socket, memCtrl, channel, slot *int) {
	var top struct {
		MemoryLocation json.RawMessage `json:"MemoryLocation"`
	}
	if err := json.Unmarshal(body, &top); err != nil {
		return nil, nil, nil, nil
	}
	raw := bytes.TrimSpace(top.MemoryLocation)
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil, nil, nil
	}
	var loc struct {
		Socket           *int `json:"Socket"`
		MemoryController *int `json:"MemoryController"`
		Channel          *int `json:"Channel"`
		Slot             *int `json:"Slot"`
	}
	if err := json.Unmarshal(raw, &loc); err != nil {
		return nil, nil, nil, nil
	}
	return loc.Socket, loc.MemoryController, loc.Channel, loc.Slot
}

func intPtrsFromMemoryLocation(loc redfish.MemoryLocation) (socket, memCtrl, channel, slot *int) {
	s := loc.Socket
	mc := loc.MemoryController
	ch := loc.Channel
	sl := loc.Slot
	return &s, &mc, &ch, &sl
}
