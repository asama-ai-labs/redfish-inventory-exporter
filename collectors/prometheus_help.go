// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Asama Inc.

package collectors

import (
	"bufio"
	"fmt"
	"io"
	"strings"
)

// escapePrometheusHelp escapes a Prometheus HELP doc string (backslashes and newlines).
func escapePrometheusHelp(s string) string {
	var b strings.Builder
	b.Grow(len(s) + 8)
	for _, r := range s {
		switch r {
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// prometheusFamilyHelp returns HELP text for every metric family when --expose-prometheus-metadata is on.
// VictoriaMetrics emits "# HELP <family>" with no description; we replace those lines.
func prometheusFamilyHelp(family string) string {
	// Special cases: multiple enum-like labels or wording that does not fit *_health_info / *_state_info.
	switch family {
	case "redfish_chassis_power_state_info":
		return helpRedfishChassisPowerState
	case "redfish_system_info":
		return helpRedfishSystemInfo
	case "redfish_system_health_info":
		return helpRedfishSystemHealth
	case "redfish_system_state_info":
		return helpRedfishSystemState
	case "redfish_system_fan_redundancy_info":
		return helpRedfishSystemFanRedundancy
	case "redfish_storage_drive_health_info":
		return helpRedfishStorageDriveHealth
	case "redfish_storage_drive_state_info":
		return helpRedfishStorageDriveState
	case "redfish_network":
		return helpRedfishEthernetLinkStatus
	case "redfish_network_port_state_info":
		return helpRedfishNetworkPortStateAndLink
	case "redfish_chassis_intrusion_state":
		return helpRedfishIntrusionSensor
	case "redfish_network_adapter_info":
		return helpRedfishNetworkAdapterInfo
	case "redfish_powersupply_redundancy_info":
		return helpRedfishRedundancyMode
	}
	if s := promMetricDoc[family]; s != "" {
		return s
	}
	switch {
	case strings.HasSuffix(family, "_health_info") || family == "redfish_pcie_device_health":
		return "Gauge 1 with Redfish health as a label. " + helpRedfishResourceHealth
	case strings.HasSuffix(family, "_state_info") || family == "redfish_pcie_device_state":
		return "Gauge 1 with Redfish state as a label. " + helpRedfishResourceState
	default:
		if strings.HasPrefix(family, "redfish_inventory_exporter_") {
			return "redfish-inventory-exporter operational metric " + family + "."
		}
		if strings.HasPrefix(family, "redfish_") {
			return "Redfish-derived gauge " + family + "; labels identify the BMC resource or sensor reading."
		}
		return ""
	}
}

const (
	helpRedfishResourceHealth = "Redfish Resource.Health on the health label. Enum: OK, Warning, Critical. The exporter uses Unknown when the BMC omits health."

	helpRedfishResourceState = "Redfish Resource.State on the state label. Enum: Enabled, Disabled, StandbyOffline, StandbySpare, InTest, Starting, Absent, UnavailableOffline, Deferring, Quiesced, Updating, Qualified. Unknown when omitted."

	helpRedfishChassisPowerState = "Redfish Chassis.PowerState on power_state. Enum: On, Off, PausedSleeping, PoweringOn, PoweringOff. Unknown when omitted."

	helpRedfishSystemInfo = "ComputerSystem inventory (constant 1): serial_number, model, sub_model, manufacturer, hostname, uuid, pca_part_number (Oem.Redpool when present). Health and operational state are on redfish_system_health_info and redfish_system_state_info. Fan redundancy is on redfish_system_fan_redundancy_info."

	helpRedfishSystemHealth = "Gauge 1 with Redfish health as a label. " + helpRedfishResourceHealth + " health_rollup is Resource.HealthRollup. aggregate_server_health is Oem.Redpool AggregateServerHealth when present (e.g. HPE)."

	helpRedfishSystemState = "Gauge 1 with Redfish state as a label. " + helpRedfishResourceState + " power_state is ComputerSystem.PowerState (On, Off, Paused, PoweringOn, PoweringOff). post_state is Oem.Redpool when present (e.g. HPE)."

	helpRedfishSystemFanRedundancy = "ComputerSystem fan redundancy status (constant 1). fan_redundancy is Oem.Redpool FanRedundancy when present (e.g. HPE AggregateHealthStatus.FanRedundancy)."

	helpRedfishStorageDriveHealth = "Gauge 1 with Redfish health as a label. " + helpRedfishResourceHealth + " predictive_failure is Oem.Redpool PredictiveFailureState when present (e.g. Dell SmartAlertAbsent). Boolean FailurePredicted is redfish_storage_drive_failure_predicted. status_indicator is Drive.StatusIndicator when present and different from health (e.g. Rebuild, Fail, InACriticalArray)."

	helpRedfishStorageDriveState = "Gauge 1 with Redfish state as a label. " + helpRedfishResourceState + " raid_status is Oem.Redpool RaidStatus when present (e.g. Dell Online, Ready, Offline, Failed, Foreign, Non-RAID)."

	helpRedfishEthernetLinkStatus = "EthernetInterface inventory (constant 1). link_status is Redfish LinkStatus. Enum: LinkUp, LinkDown, LinkNegotiating, NoLink. Vendors may emit other strings."

	helpRedfishNetworkPortStateAndLink = "Network adapter port state (constant 1). state is Resource.State (see Resource.State enum on redfish_*_state_info metrics; Unknown when omitted). link_status is Port.LinkStatus: LinkUp, LinkDown, NoLink, Starting, Training. Empty when omitted."

	helpRedfishIntrusionSensor = "Chassis PhysicalSecurity.IntrusionSensor. Enum: Normal, HardwareIntrusion, TamperingDetected. Unknown when omitted."

	helpRedfishNetworkAdapterInfo = "Chassis NetworkAdapter inventory (constant 1). Health and state are on redfish_network_adapter_health_info and redfish_network_adapter_state_info."

	helpRedfishRedundancyMode = "Power subsystem Redundancy group (constant 1). mode is Redfish Redundancy.Mode. Enum: Failover, N+m, Sharing, Sparing, NotRedundant. Unknown when omitted."
)

// promMetricDoc is HELP for informational and numeric Redfish gauges (not generic *_health_info / *_state_info).
var promMetricDoc = map[string]string{
	"redfish_inventory_exporter_scrape_duration_seconds":           "Wall time in seconds for the last /redfish scrape of this target.",
	"redfish_inventory_exporter_up":                                "1 if Redfish collection for this target succeeded, 0 if CollectMetrics returned an error (HTTP may still be 200).",
	"redfish_inventory_exporter_collector_success":                 "1 when the collector subsystem read everything it asked for during the last scrape, 0 when any request failed. A 0 can still come with partial metrics: members that were read are kept. Use label collector (bios, chassis, memory, network, pcie, power, processor, sensors, storage, thermal). Series absent for subsystems disabled via --disable-collectors.",
	"redfish_inventory_exporter_scrape_collector_duration_seconds": "Wall time in seconds per collector subsystem during the last scrape; use label collector (bios, bmc, chassis, firmware_inventory, memory, network, pcie, power, processor, sensors, storage, system, thermal). Series absent for subsystems disabled via --disable-collectors.",

	"redfish_firmware_inventory_info": "UpdateService FirmwareInventory member (constant 1): version, software_id, and placement labels (related_kind, related_ref, related_scope, related_location) per component. Health and state are on redfish_firmware_inventory_health_info and redfish_firmware_inventory_state_info.",

	"redfish_bios_attributes": "One gauge per BIOS attribute from ComputerSystem.Bios (attribute_name / attribute_value labels).",

	"redfish_bmc_manager_info": "BMC Manager resource (constant 1) with id, name, model, manufacturer, firmware_version, uuid labels (uuid is Manager.UUID; empty when omitted).",

	"redfish_chassis_info": "Chassis resource (constant 1) with id, model, sku, manufacturer, device_serial_number, asset_tag, chassis_type, contained_by, and FRU/engineering_change_level from Assembly when present.",

	"redfish_multinode_chassis_info": "Multi-node chassis identity from Oem.Redpool (constant 1): system/chassis part numbers, self id, twin type. Emitted only when at least one multinode label is present.",

	"redfish_chassis_multinode_info": "Multi-node blade inventory from Oem.Redpool.MultiNodeNodes (constant 1): node_id, board/firmware versions, power_status.",

	"redfish_chassis_multinode_dc_output_power_watts": "Multi-node blade DC output power in watts.",

	"redfish_chassis_multinode_output_current_amps": "Multi-node blade output current in amps.",

	"redfish_chassis_multinode_temperature_celsius": "Multi-node blade temperature sensor reading in degrees Celsius.",

	"redfish_system_rollup_health_info": "Vendor OEM subsystem rollup health (constant 1) from Oem.Redpool *RollupStatus (e.g. Dell SELRollupStatus).",

	"redfish_system_estimated_airflow_cfm": "Estimated system airflow in CFM from Oem.Redpool.EstimatedSystemAirflowCFM when present.",

	"redfish_system_estimated_exhaust_temperature_celsius": "Estimated exhaust temperature in degrees Celsius from Oem.Redpool when present.",

	"redfish_memory": "Memory module (DIMM) inventory (constant 1) with capacity, type, locator, speed, FRU from Assembly when present, and related labels.",

	"redfish_memory_summary_info": "System-wide memory summary (constant 1): total capacity, device_count, etc.",

	"redfish_processor_info": "Processor socket inventory (constant 1) with architecture, speeds, tdp_watts, processor_memory cache capacities, cores, threads, identification labels.",

	"redfish_processor_summary_info": "Aggregated processor summary for the system (constant 1): socket_count, total_cores, total_threads, max_speed_mhz, etc.",

	"redfish_pcie_device_info": "PCIe device / function inventory (constant 1) with vendor/device class, IDs, location, firmware, and PCIeInterface (pcie_type/max_pcie_type, lanes_in_use/max_lanes) labels.",

	"redfish_storage_drive_info":                    "Storage drive inventory (constant 1): model, serial, media, protocol, capacity, encryption, location, cpu_affinity, volume_id, etc. Health and predictive failure are on redfish_storage_drive_health_info; state and raid_status are on redfish_storage_drive_state_info.",
	"redfish_storage_drive_available_spare_percent": "Drive AvailableSparePercent from Oem.Redpool when the BMC exposes SSD spare capacity.",

	"redfish_storage_drive_failure_predicted": "1 if the drive reports FailurePredicted, else 0.",

	"redfish_storage_drive_predicted_media_life_percent": "Vendor-reported remaining media life percent for the drive (0–100 when present).",

	"redfish_storage_drive_write_cache_enabled": "1 if host write cache is enabled on the drive, else 0.",

	"redfish_storage_volume_info": "Storage volume (logical drive) inventory (constant 1): RAID type, capacity, cache policies, LUN, encryption, etc.",

	"redfish_storage_controller_info": "Storage controller inventory (constant 1): model, firmware, protocols, cache sizes, supported RAID types, identifier (DurableNameFormat:DurableName), and FRU/manufacture/engineering_change_level from Assembly when present.",

	"redfish_storage_battery_info": "Storage-controller battery inventory (constant 1): model, serial, firmware, spare part, and charge state labels.",

	"redfish_storage_battery_capacity_actual_watt_hours": "Storage-controller battery actual maximum capacity in watt-hours when reported.",

	"redfish_storage_battery_capacity_rated_watt_hours": "Storage-controller battery rated maximum capacity in watt-hours when reported.",

	"redfish_storage_battery_capacity_actual_amp_hours": "Storage-controller battery actual maximum capacity in amp-hours when reported.",

	"redfish_storage_battery_capacity_rated_amp_hours": "Storage-controller battery rated maximum capacity in amp-hours when reported.",

	"redfish_storage_battery_max_charge_rate_amps": "Storage-controller battery maximum charge rate in amps when reported.",

	"redfish_storage_battery_max_discharge_rate_amps": "Storage-controller battery maximum discharge rate in amps when reported.",

	"redfish_storage_battery_max_charge_voltage": "Storage-controller battery maximum charge voltage when reported.",

	"redfish_storage_battery_state_of_health_percent": "Battery state of health percent when reported (Redfish StateOfHealthPercent.Reading).",

	"redfish_storage_battery_charge_level_percent": "Battery charge level percent when reported (OEM or state-of-health fallback).",

	"redfish_storage_battery_maximum_cap_watts": "Vendor-reported maximum battery power capability in watts (e.g. HPE Smart Storage MaximumCapWatts), not energy capacity.",

	"redfish_storage_battery_remaining_charge_time_seconds": "Estimated seconds remaining until charge completes when reported by the BMC.",

	"redfish_powersupply_total_consumed_watts": "Chassis power control: power consumed in watts (PowerControl.PowerConsumedWatts or average from PowerMetrics).",

	"redfish_powersupply_info": "Power supply unit inventory (constant 1): manufacturer, model, part/serial, type, firmware, hot_pluggable, manufacture labels, and engineering_change_level from Assembly when present.",

	"redfish_powersupply_power_limit_watts": "Chassis PowerControl.PowerLimit.LimitInWatts when configured.",

	"redfish_powersupply_power_upper_critical_watts": "Upper-critical power threshold (watts) for the system power control, promoted from Chassis Sensors when PowerControl does not expose it.",

	"redfish_powersupply_min_consumed_watts": "PowerMetrics.MinConsumedWatts over the reported IntervalInMin window.",

	"redfish_powersupply_max_consumed_watts": "PowerMetrics.MaxConsumedWatts over the reported IntervalInMin window.",

	"redfish_powersupply_average_consumed_watts": "PowerMetrics.AverageConsumedWatts over the reported IntervalInMin window.",

	"redfish_powersupply_input_volts": "Measured line input voltage (volts) for the named PSU.",

	"redfish_powersupply_input_watts": "Measured input power (watts) for the named PSU when reported.",

	"redfish_powersupply_output_watts": "DC output power (watts) for the named PSU (LastPowerOutputWatts or PowerOutputWatts).",

	"redfish_powersupply_efficiency_percent": "PSU efficiency percent when reported by the BMC.",

	"redfish_powersupply_power_capacity_watts": "Rated or reported power capacity (watts) for the named PSU.",

	"redfish_powersupply_redundancy_member_info": "Constant 1 per PSU membership in a redundancy group (chassis_id, redundancy_id, psu_name).",

	"redfish_power_voltage_output": "Power subsystem voltage sensor reading in volts.",

	"redfish_power_voltage_info": "Constant 1 per voltage rail sensor (chassis_id, name).",

	"redfish_power_voltage_lower_threshold_critical": "Voltage rail lower critical threshold in volts.",

	"redfish_power_voltage_lower_threshold_noncritical": "Voltage rail lower non-critical threshold in volts.",

	"redfish_power_voltage_lower_threshold_fatal": "Voltage rail lower fatal threshold in volts.",

	"redfish_power_voltage_upper_threshold_critical": "Voltage rail upper critical threshold in volts.",

	"redfish_power_voltage_upper_threshold_noncritical": "Voltage rail upper non-critical threshold in volts.",

	"redfish_power_voltage_upper_threshold_fatal": "Voltage rail upper fatal threshold in volts.",

	"redfish_power_voltage_min_reading_range": "Voltage rail MinReadingRange measurement floor in volts.",

	"redfish_power_voltage_max_reading_range": "Voltage rail MaxReadingRange measurement ceiling in volts.",

	"redfish_thermal_summary_info": "Thermal subsystem summary (constant 1) for the chassis thermal resource URL.",

	"redfish_thermal_fan_info": "Constant 1 per fan with chassis_id, fan_name, physical_context, and related_ref (Thermal RelatedItem join) for identification; speed/threshold metrics use chassis_id and fan_name only.",

	"redfish_thermal_fan_speed_rpm": "Fan speed reading in RPM when ReadingUnits are not percent.",

	"redfish_thermal_fan_speed_percent": "Fan speed as percent: Thermal.Fans when ReadingUnits are percent, Oem.Redpool.FanPWM on a ThermalSubsystem Fan, or RPM divided by MaxReadingRange when both are available.",

	"redfish_thermal_fan_lower_threshold_critical": "Fan lower critical threshold in the same units as the fan reading (RPM or percent).",

	"redfish_thermal_fan_lower_threshold_noncritical": "Fan lower non-critical threshold in the same units as the fan reading (RPM or percent).",

	"redfish_thermal_fan_lower_threshold_fatal": "Fan lower fatal threshold in the same units as the fan reading (RPM or percent).",

	"redfish_thermal_fan_upper_threshold_critical": "Fan upper critical threshold in the same units as the fan reading (RPM or percent).",

	"redfish_thermal_fan_upper_threshold_noncritical": "Fan upper non-critical threshold in the same units as the fan reading (RPM or percent).",

	"redfish_thermal_fan_upper_threshold_fatal": "Fan upper fatal threshold in the same units as the fan reading (RPM or percent).",

	"redfish_thermal_fan_min_reading_range": "Fan MinReadingRange measurement floor in the same units as the fan reading.",

	"redfish_thermal_fan_max_reading_range": "Fan MaxReadingRange measurement ceiling in the same units as the fan reading.",

	"redfish_thermal_sensor_info": "Constant 1 per temperature sensor with chassis_id, sensor_name, physical_context, and related_ref (Thermal RelatedItem join); value/threshold metrics use chassis_id and sensor_name only.",

	"redfish_thermal_sensor_temperature": "Temperature sensor reading in degrees Celsius.",

	"redfish_thermal_sensor_lower_threshold_critical": "Temperature sensor lower critical threshold in degrees Celsius.",

	"redfish_thermal_sensor_lower_threshold_noncritical": "Temperature sensor lower non-critical threshold in degrees Celsius.",

	"redfish_thermal_sensor_lower_threshold_fatal": "Temperature sensor lower fatal threshold in degrees Celsius.",

	"redfish_thermal_sensor_upper_threshold_critical": "Temperature sensor upper critical threshold in degrees Celsius.",

	"redfish_thermal_sensor_upper_threshold_noncritical": "Temperature sensor upper non-critical threshold in degrees Celsius.",

	"redfish_thermal_sensor_upper_threshold_fatal": "Temperature sensor upper fatal threshold in degrees Celsius.",

	"redfish_thermal_sensor_min_reading_range": "Temperature sensor MinReadingRangeTemp measurement floor in degrees Celsius.",

	"redfish_thermal_sensor_max_reading_range": "Temperature sensor MaxReadingRangeTemp measurement ceiling in degrees Celsius.",

	"redfish_chassis_sensor_info": "Chassis Sensors collection member (constant 1): sensor_id, reading_type, physical_context.",

	"redfish_chassis_sensor_reading": "Chassis Sensor.Reading numeric value in ReadingUnits.",

	"redfish_chassis_sensor_min_reading_range": "Chassis Sensor.ReadingRangeMin measurement floor.",

	"redfish_chassis_sensor_max_reading_range": "Chassis Sensor.ReadingRangeMax measurement ceiling.",

	"redfish_chassis_sensor_upper_threshold_critical": "Chassis Sensor Thresholds.UpperCritical.Reading.",

	"redfish_chassis_sensor_upper_threshold_noncritical": "Chassis Sensor Thresholds.UpperCaution.Reading.",

	"redfish_chassis_sensor_upper_threshold_fatal": "Chassis Sensor Thresholds.UpperFatal.Reading.",

	"redfish_chassis_sensor_lower_threshold_critical": "Chassis Sensor Thresholds.LowerCritical.Reading.",

	"redfish_chassis_sensor_lower_threshold_noncritical": "Chassis Sensor Thresholds.LowerCaution.Reading.",

	"redfish_chassis_sensor_lower_threshold_fatal": "Chassis Sensor Thresholds.LowerFatal.Reading.",

	"redfish_network_port_info": "Chassis NetworkPort inventory (constant 1): adapter_id, port number, link technology.",

	"redfish_thermal_average_fan_pwm":                 "Chassis-wide average fan PWM duty cycle (percent) from Oem.Redpool.AverageFanPWM on Chassis EnvironmentMetrics.",
	"redfish_chassis_environment_power_watts":         "Chassis EnvironmentMetrics.PowerWatts.Reading with data_source_uri join to Sensors.",
	"redfish_chassis_environment_temperature_celsius": "Chassis EnvironmentMetrics.TemperatureCelsius.Reading with data_source_uri join to Sensors.",

	"redfish_memory_alarm_trip": "MemoryMetrics HealthData.AlarmTrips boolean as 0/1 for trip_type.",

	"redfish_processor_operating_speed_mhz": "ProcessorMetrics.OperatingSpeedMHz current operating clock.",

	"redfish_processor_temperature_celsius": "ProcessorMetrics.TemperatureCelsius when the BMC exposes it.",

	"redfish_processor_bandwidth_percent": "ProcessorMetrics.BandwidthPercent utilization.",

	"redfish_processor_consumed_power_watts": "ProcessorMetrics.ConsumedPowerWatt when the BMC exposes it.",

	"redfish_processor_power_watts": "Processor EnvironmentMetrics.PowerWatts.Reading.",

	"redfish_processor_power_limit_watts": "Processor EnvironmentMetrics.PowerLimitWatts.SetPoint (OEM peak limits copied by Redpool), or a scalar PowerLimitWatts when still present.",

	"redfish_storage_drive_temperature_celsius": "Drive temperature in degrees Celsius from Oem when present.",

	"redfish_storage_drive_uncorrected_read_errors": "Drive uncorrected read error count from OEM SmartStorage fields.",

	"redfish_storage_drive_uncorrected_write_errors": "Drive uncorrected write error count from OEM SmartStorage fields.",
}

// enrichPrometheusHelp reads Prometheus text from r and writes it to w, replacing
// bare "# HELP <family>" lines (no doc string) with documented HELP when known.
func enrichPrometheusHelp(r io.Reader, w io.Writer) error {
	sc := bufio.NewScanner(r)
	// Large metric lines with many labels
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "# HELP ") {
			fields := strings.Fields(line)
			if len(fields) == 3 && fields[1] == "HELP" {
				family := fields[2]
				if doc := prometheusFamilyHelp(family); doc != "" {
					line = fmt.Sprintf("# HELP %s %s", family, escapePrometheusHelp(doc))
				}
			}
		}
		if _, err := fmt.Fprintln(w, line); err != nil {
			return err
		}
	}
	return sc.Err()
}
