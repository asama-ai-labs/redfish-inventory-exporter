// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Asama Inc.

package collectors

import (
	"errors"
	"fmt"
	"log"
	"time"
)

// CollectMetrics gathers all Redfish data and sets Prometheus gauges.
func (c *Collector) CollectMetrics(target string) error {
	log.Printf("Starting metrics collection for %s...", target)

	t0 := time.Now()
	sysInfo, err := c.GetSystemInfo()
	if err != nil {
		c.RecordCollectorDuration(target, "system", t0)
		log.Printf("Error getting system info: %v", err)
		return err
	}
	if sysInfo == nil {
		c.RecordCollectorDuration(target, "system", t0)
		err := fmt.Errorf("no system found")
		log.Printf("Error getting system info: %v", err)
		return err
	}
	c.Reset()
	// After Reset, which clears every gauge, so the duration is still in the output.
	c.RecordCollectorDuration(target, "system", t0)

	c.SetSystemMetrics(sysInfo)

	if !c.Disabled("processor") {
		t1 := time.Now()
		processors, err := c.GetProcessorInfo()
		if err == nil || isPartial(err) {
			procSummary, _ := c.GetProcessorSummary()
			c.SetProcessorMetrics(processors, procSummary)
			c.collectProcessorRuntimeMetrics()
		}
		c.RecordCollectorSuccess(target, "processor", err)
		c.RecordCollectorDuration(target, "processor", t1)
	}

	if !c.Disabled("memory") {
		t1 := time.Now()
		memInfo, err := c.GetMemoryInfo()
		if err == nil || isPartial(err) {
			memSummary, _ := c.GetMemorySummary()
			c.SetMemoryMetrics(memInfo, memSummary)
			c.collectMemoryAlarmTrips()
		}
		c.RecordCollectorSuccess(target, "memory", err)
		c.RecordCollectorDuration(target, "memory", t1)
	}

	if !c.Disabled("pcie") {
		t1 := time.Now()
		pcieInfo, err := c.GetPCIeInfo()
		if err == nil || isPartial(err) {
			c.SetPCIeMetrics(pcieInfo)
		}
		c.RecordCollectorSuccess(target, "pcie", err)
		c.RecordCollectorDuration(target, "pcie", t1)
	}

	if !c.Disabled("bmc") {
		t1 := time.Now()
		bmcInfo, err := c.GetBMCManagerInfo()
		if err == nil {
			c.SetBMCMetrics(bmcInfo)
		}
		c.RecordCollectorSuccess(target, "bmc", err)
		c.RecordCollectorDuration(target, "bmc", t1)
	}

	if !c.Disabled("storage") {
		t1 := time.Now()
		storageInfo, err := c.GetStorageInfo()
		if err == nil || isPartial(err) {
			c.SetStorageMetrics(storageInfo)
		}
		c.RecordCollectorSuccess(target, "storage", err)
		c.RecordCollectorDuration(target, "storage", t1)
	}

	if !c.Disabled("network") {
		t1 := time.Now()
		networkInfo, err := c.GetNetworkInfo()
		if err == nil || isPartial(err) {
			c.SetNetworkMetrics(networkInfo)
		}
		naInv, naErr := c.GetNetworkAdapterPortInventory()
		if naErr == nil || isPartial(naErr) {
			c.SetNetworkAdapterPortMetrics(naInv)
		}
		c.RecordCollectorSuccess(target, "network", errors.Join(err, naErr))
		c.RecordCollectorDuration(target, "network", t1)
	}

	if !c.Disabled("chassis") {
		t1 := time.Now()
		chassisResponse, err := c.GetChassisInfo()
		if err == nil || isPartial(err) {
			c.SetChassisMetrics(chassisResponse)
		}
		c.RecordCollectorSuccess(target, "chassis", err)
		c.RecordCollectorDuration(target, "chassis", t1)
	}

	if !c.Disabled("thermal") {
		t1 := time.Now()
		chassis, err := c.client.Service.Chassis()
		var partial []error
		if keepPartial("thermal", err, &partial) {
			for _, ch := range chassis {
				c.collectChassisThermal(ch)
			}
		}
		c.RecordCollectorSuccess(target, "thermal", err)
		c.RecordCollectorDuration(target, "thermal", t1)
	}

	if !c.Disabled("power") {
		t1 := time.Now()
		chassis, err := c.client.Service.Chassis()
		var partial []error
		if keepPartial("power", err, &partial) {
			for _, ch := range chassis {
				c.collectChassisPower(ch)
			}
			for _, ch := range chassis {
				c.flushPendingEnvironmentPower(ch.ID)
			}
		}
		c.RecordCollectorSuccess(target, "power", err)
		c.RecordCollectorDuration(target, "power", t1)
	} else if !c.Disabled("thermal") {
		chassis, err := c.client.Service.Chassis()
		var partial []error
		if keepPartial("power", err, &partial) {
			for _, ch := range chassis {
				c.flushPendingEnvironmentPower(ch.ID)
			}
		}
	}

	if !c.Disabled("sensors") {
		t1 := time.Now()
		chassis, err := c.client.Service.Chassis()
		var partial []error
		if keepPartial("sensors", err, &partial) {
			for _, ch := range chassis {
				c.collectChassisSensors(ch)
			}
		}
		c.RecordCollectorSuccess(target, "sensors", err)
		c.RecordCollectorDuration(target, "sensors", t1)
	}

	if !c.Disabled("bios") {
		t1 := time.Now()
		biosAttrs, err := c.GetBiosAttributes()
		if err == nil {
			c.SetBiosMetrics(biosAttrs)
		} else {
			log.Printf("GetBiosAttributes failed (no redfish_bios_attributes): %v", err)
		}
		c.RecordCollectorSuccess(target, "bios", err)
		c.RecordCollectorDuration(target, "bios", t1)
	}

	if !c.Disabled("firmware_inventory") {
		t1 := time.Now()
		c.collectFirmwareInventory()
		c.RecordCollectorDuration(target, "firmware_inventory", t1)
	}

	log.Println("Finished metrics collection")
	return nil
}
