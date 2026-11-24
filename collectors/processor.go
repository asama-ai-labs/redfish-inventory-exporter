// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Asama Inc.

package collectors

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"

	"github.com/asama-ai-labs/redfish-inventory-exporter/models"
	"github.com/stmcginnis/gofish/redfish"
)

func formatProcessorMemory(mem []redfish.ProcessorMemory) string {
	if len(mem) == 0 {
		return ""
	}
	parts := make([]string, 0, len(mem))
	for _, m := range mem {
		if m.CapacityMiB == 0 || m.MemoryType == "" {
			continue
		}
		parts = append(parts, fmt.Sprintf("%s:%d", m.MemoryType, m.CapacityMiB))
	}
	return strings.Join(parts, "|")
}

// processorCache is the Processor.ProcessorMemory entry gofish v0.20 drops.
// Its UnmarshalJSON reads that property as a link collection, and SpeedMHz is
// often JSON null, which cannot land on the int field.
type processorCache struct {
	CapacityMiB *int   `json:"CapacityMiB"`
	MemoryType  string `json:"MemoryType"`
}

func formatProcessorCache(mem []processorCache) string {
	parts := make([]string, 0, len(mem))
	for _, m := range mem {
		if m.CapacityMiB == nil || *m.CapacityMiB == 0 || m.MemoryType == "" {
			continue
		}
		parts = append(parts, fmt.Sprintf("%s:%d", m.MemoryType, *m.CapacityMiB))
	}
	return strings.Join(parts, "|")
}

func processorMemoryLabel(p *redfish.Processor) string {
	if p == nil {
		return ""
	}
	if formatted := formatProcessorMemory(p.ProcessorMemory); formatted != "" {
		return formatted
	}
	raw := processorRawJSON(p)
	if len(raw) == 0 {
		return ""
	}
	var body struct {
		ProcessorMemory []processorCache `json:"ProcessorMemory"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		return ""
	}
	return formatProcessorCache(body.ProcessorMemory)
}

// processorRawJSON returns the resource body gofish kept after UnmarshalJSON.
// That unmarshal treats ProcessorMemory as a link collection, so the cache
// entries are only still present in this original document.
func processorRawJSON(p *redfish.Processor) []byte {
	if p == nil {
		return nil
	}
	field := reflect.ValueOf(p).Elem().FieldByName("rawData")
	if !field.IsValid() || field.Kind() != reflect.Slice || field.Type().Elem().Kind() != reflect.Uint8 {
		return nil
	}
	raw := field.Bytes()
	if len(raw) == 0 {
		return nil
	}
	out := make([]byte, len(raw))
	copy(out, raw)
	return out
}

// firstCPUProcessor returns the first processor with ProcessorType "CPU", or nil if none.
func firstCPUProcessor(processors []*redfish.Processor) *redfish.Processor {
	for _, p := range processors {
		if p == nil {
			continue
		}
		if strings.EqualFold(string(p.ProcessorType), "CPU") {
			return p
		}
	}
	return nil
}

// countCPUProcessors returns the number of processors with ProcessorType "CPU".
func countCPUProcessors(processors []*redfish.Processor) int {
	n := 0
	for _, p := range processors {
		if p != nil && strings.EqualFold(string(p.ProcessorType), "CPU") {
			n++
		}
	}
	return n
}

func (c *Collector) GetProcessorInfo() ([]models.ProcessorInfo, error) {
	systems, err := c.client.Service.Systems()
	if err != nil {
		return nil, err
	}
	if len(systems) == 0 {
		return nil, nil
	}
	system := systems[0]
	var partial []error
	processors, err := system.Processors()
	if !keepPartial("processor", err, &partial) {
		return nil, err
	}
	var info []models.ProcessorInfo
	for _, p := range processors {
		var model string
		if p.Model != "" {
			model = p.Model
		} else {
			model = p.ProcessorID.EffectiveFamily
		}
		totalEnabled := p.TotalEnabledCores
		if totalEnabled == 0 {
			totalEnabled = p.TotalCores
		}
		pcieID := ""
		if dev, err := p.PCIeDevice(); err == nil && dev != nil {
			pcieID = dev.ID
		}
		info = append(info, models.ProcessorInfo{
			ID:                         p.ID,
			Name:                       p.Name,
			Socket:                     p.Socket,
			ProcessorType:              string(p.ProcessorType),
			ProcessorArchitecture:      string(p.ProcessorArchitecture),
			InstructionSet:             string(p.InstructionSet),
			Manufacturer:               p.Manufacturer,
			Model:                      model,
			PartNumber:                 p.PartNumber,
			ProcessorSerialNumber:      p.SerialNumber,
			BaseSpeedMHz:               p.BaseSpeedMHz,
			MaxSpeedMHz:                int(p.MaxSpeedMHz),
			TDPWatts:                   processorTDPWatts(p),
			ProcessorMemory:            processorMemoryLabel(p),
			TotalCores:                 p.TotalCores,
			TotalThreads:               p.TotalThreads,
			TotalEnabledCores:          totalEnabled,
			ProcessorIDVendorId:        p.ProcessorID.VendorID,
			ProcessorIDEffectiveFamily: p.ProcessorID.EffectiveFamily,
			ProcessorIDEffectiveModel:  p.ProcessorID.EffectiveModel,
			ProcessorIDStep:            p.ProcessorID.Step,
			Health:                     statusStr(string(p.Status.Health)),
			State:                      statusStr(string(p.Status.State)),
			HealthRollup:               statusStr(string(p.Status.HealthRollup)),
			PCIeDeviceID:               pcieID,
		})
	}
	return info, errors.Join(partial...)
}

func (c *Collector) GetProcessorSummary() (*models.ProcessorSummary, error) {
	systems, err := c.client.Service.Systems()
	if err != nil || len(systems) == 0 {
		return nil, err
	}
	system := systems[0]

	processors, _ := system.Processors()
	var totalCores, totalThreads, maxSpeed, socketCount int
	var procType, architecture, model string

	firstCPU := firstCPUProcessor(processors)
	if firstCPU != nil {
		socketCount = countCPUProcessors(processors)
		totalCores = firstCPU.TotalCores * socketCount
		totalThreads = firstCPU.TotalThreads * socketCount
		maxSpeed = int(firstCPU.MaxSpeedMHz)
		procType = string(firstCPU.ProcessorType)
		architecture = string(firstCPU.ProcessorArchitecture)
		if system.ProcessorSummary.Model != "" {
			model = system.ProcessorSummary.Model
		} else {
			model = firstCPU.Model
		}
	}
	if socketCount == 0 && system.ProcessorSummary.Count > 0 {
		socketCount = system.ProcessorSummary.Count
	}

	logicalCount := system.ProcessorSummary.LogicalProcessorCount
	if logicalCount == 0 && firstCPU != nil && countCPUProcessors(processors) > 0 {
		logicalCount = firstCPU.TotalThreads * countCPUProcessors(processors)
	}

	return &models.ProcessorSummary{
		Count:                 socketCount,
		LogicalProcessorCount: logicalCount,
		TotalCores:            totalCores,
		TotalThreads:          totalThreads,
		Model:                 model,
		Architecture:          architecture,
		MaxSpeedMHz:           maxSpeed,
		ProcessorType:         procType,
		Health:                statusStr(string(system.ProcessorSummary.Status.Health)),
		State:                 statusStr(string(system.ProcessorSummary.Status.State)),
		HealthRollup:          statusStr(string(system.ProcessorSummary.Status.HealthRollup)),
	}, nil
}

// SetProcessorMetrics sets processor and processor summary gauges (used by collect.go).
func (c *Collector) SetProcessorMetrics(processors []models.ProcessorInfo, procSummary *models.ProcessorSummary) {
	for _, proc := range processors {
		speedMHz := proc.MaxSpeedMHz
		if proc.BaseSpeedMHz > 0 {
			speedMHz = proc.BaseSpeedMHz
		}
		c.setGauge("redfish_processor_info", map[string]string{
			"id":                            proc.ID,
			"name":                          proc.Name,
			"socket":                        proc.Socket,
			"type":                          proc.ProcessorType,
			"architecture":                  proc.ProcessorArchitecture,
			"instruction_set":               proc.InstructionSet,
			"manufacturer":                  proc.Manufacturer,
			"model":                         proc.Model,
			"part_number":                   proc.PartNumber,
			"processor_serial_number":       proc.ProcessorSerialNumber,
			"base_speed_mhz":                fmt.Sprintf("%d", proc.BaseSpeedMHz),
			"speed_mhz":                     fmt.Sprintf("%d", speedMHz),
			"tdp_watts":                     optionalIntLabel(proc.TDPWatts),
			"processor_memory":              proc.ProcessorMemory,
			"total_cores":                   fmt.Sprintf("%d", proc.TotalCores),
			"total_threads":                 fmt.Sprintf("%d", proc.TotalThreads),
			"total_enabled_cores":           fmt.Sprintf("%d", proc.TotalEnabledCores),
			"max_speed_mhz":                 fmt.Sprintf("%d", proc.MaxSpeedMHz),
			"processor_id_vendor_id":        proc.ProcessorIDVendorId,
			"processor_id_effective_family": proc.ProcessorIDEffectiveFamily,
			"processor_id_effective_model":  proc.ProcessorIDEffectiveModel,
			"processor_id_step":             proc.ProcessorIDStep,
			"pcie_device_id":                proc.PCIeDeviceID,
		}, 1)
		c.setGauge("redfish_processor_health_info", map[string]string{
			"id":     proc.ID,
			"health": proc.Health,
		}, 1)
		c.setGauge("redfish_processor_state_info", map[string]string{
			"id":    proc.ID,
			"state": proc.State,
		}, 1)
	}
	if procSummary != nil {
		c.setGauge("redfish_processor_summary_info", map[string]string{
			"model":                   procSummary.Model,
			"architecture":            procSummary.Architecture,
			"socket_count":            fmt.Sprintf("%d", procSummary.Count),
			"total_cores":             fmt.Sprintf("%d", procSummary.TotalCores),
			"total_threads":           fmt.Sprintf("%d", procSummary.TotalThreads),
			"logical_processor_count": fmt.Sprintf("%d", procSummary.LogicalProcessorCount),
			"max_speed_mhz":           fmt.Sprintf("%d", procSummary.MaxSpeedMHz),
			"type":                    procSummary.ProcessorType,
		}, 1)
		c.setGauge("redfish_processor_summary_health_info", map[string]string{
			"health": procSummary.Health,
		}, 1)
		c.setGauge("redfish_processor_summary_state_info", map[string]string{
			"state": procSummary.State,
		}, 1)
	}
}

// collectProcessorRuntimeMetrics emits ProcessorMetrics and EnvironmentMetrics gauges.
func (c *Collector) collectProcessorRuntimeMetrics() {
	systems, err := c.client.Service.Systems()
	if err != nil || len(systems) == 0 {
		return
	}
	processors, err := systems[0].Processors()
	if err != nil && !isPartial(err) {
		return
	}
	for _, p := range processors {
		if p == nil {
			continue
		}
		labels := map[string]string{
			"id":   p.ID,
			"type": string(p.ProcessorType),
		}
		if metrics, err := p.Metrics(); err == nil && metrics != nil {
			if metrics.OperatingSpeedMHz != 0 {
				c.setGauge("redfish_processor_operating_speed_mhz", labels, float64(metrics.OperatingSpeedMHz))
			}
			if metrics.BandwidthPercent != 0 {
				c.setGauge("redfish_processor_bandwidth_percent", labels, metrics.BandwidthPercent)
			}
			// TemperatureCelsius / ConsumedPowerWatt exist on some BMCs but not in gofish v0.20.
			if cli := p.GetClient(); cli != nil && metrics.ODataID != "" {
				if body, ok := getRedfishResourceJSON(cli, metrics.ODataID); ok {
					var extra struct {
						TemperatureCelsius *float64 `json:"TemperatureCelsius"`
						ConsumedPowerWatt  *float64 `json:"ConsumedPowerWatt"`
					}
					if err := json.Unmarshal(body, &extra); err == nil {
						if extra.TemperatureCelsius != nil && *extra.TemperatureCelsius != 0 {
							c.setGauge("redfish_processor_temperature_celsius", labels, *extra.TemperatureCelsius)
						}
						if extra.ConsumedPowerWatt != nil && *extra.ConsumedPowerWatt != 0 {
							c.setGauge("redfish_processor_consumed_power_watts", labels, *extra.ConsumedPowerWatt)
						}
					}
				}
			}
		}
		limit := 0.0
		if env, err := p.EnvironmentMetrics(); err == nil && env != nil {
			if env.PowerWatts.Reading != 0 {
				c.setGauge("redfish_processor_power_watts", labels, float64(env.PowerWatts.Reading))
			}
			if env.PowerLimitWatts.SetPoint > 0 {
				limit = env.PowerLimitWatts.SetPoint
			}
		}
		// Scalar PowerLimitWatts still present on some processor documents.
		if limit == 0 {
			if cli := p.GetClient(); cli != nil && p.ODataID != "" {
				if body, ok := getRedfishResourceJSON(cli, p.ODataID); ok {
					var doc struct {
						PowerLimitWatts *float64 `json:"PowerLimitWatts"`
					}
					if err := json.Unmarshal(body, &doc); err == nil && doc.PowerLimitWatts != nil && *doc.PowerLimitWatts > 0 {
						limit = *doc.PowerLimitWatts
					}
				}
			}
		}
		if limit > 0 {
			c.setGauge("redfish_processor_power_limit_watts", labels, limit)
		}
	}
}

// processorTDPWatts returns the processor's TDP, or nil when the BMC does not report one.
// gofish decodes a missing TDPWatts as 0, so a zero is checked against the raw document:
// only an explicit TDPWatts value counts as reported. The retained document is
// authoritative when present; a GET is issued only when it is unavailable.
func processorTDPWatts(p *redfish.Processor) *int {
	if p.TDPWatts != 0 {
		v := p.TDPWatts
		return &v
	}
	if raw := processorRawJSON(p); raw != nil {
		return tdpWattsFromJSON(raw)
	}
	cli := p.GetClient()
	if cli == nil || p.ODataID == "" {
		return nil
	}
	body, ok := getRedfishResourceJSON(cli, p.ODataID)
	if !ok {
		return nil
	}
	return tdpWattsFromJSON(body)
}

// tdpWattsFromJSON reads an explicit TDPWatts from a Processor document.
func tdpWattsFromJSON(body []byte) *int {
	var doc struct {
		TDPWatts *int `json:"TDPWatts"`
	}
	if err := json.Unmarshal(body, &doc); err != nil {
		return nil
	}
	return doc.TDPWatts
}

// optionalIntLabel formats an optional integer label: "" when the value is unknown.
func optionalIntLabel(v *int) string {
	if v == nil {
		return ""
	}
	return strconv.Itoa(*v)
}
