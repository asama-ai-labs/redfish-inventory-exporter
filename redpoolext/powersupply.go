// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Asama Inc.

package redpoolext

import "encoding/json"

// RedpoolPowerSupplyExt is the JSON shape of PowerSupply.Oem.Redpool after Redpool normalization
// (e.g. Dell PSU manufacture fields from Assembly ProductionDate).
type RedpoolPowerSupplyExt struct {
	ManufactureWeek        *int   `json:"ManufactureWeek,omitempty"`
	ManufactureYear        *int   `json:"ManufactureYear,omitempty"`
	ManufactureDate        string `json:"ManufactureDate,omitempty"` // YYYY-MM-DD when Redpool parsed Assembly date
	FruPartNumber          string `json:"FruPartNumber,omitempty"`
	EngineeringChangeLevel string `json:"EngineeringChangeLevel,omitempty"`
}

// ParseRedpoolPowerSupplyExtFromJSON extracts Oem.Redpool from one PowerSupply element JSON
// (embedded in Chassis Power.PowerSupplies[] or a standalone PowerSupply resource).
func ParseRedpoolPowerSupplyExtFromJSON(body []byte) (RedpoolPowerSupplyExt, bool) {
	var top struct {
		Oem json.RawMessage `json:"Oem"`
	}
	if err := json.Unmarshal(body, &top); err != nil {
		return RedpoolPowerSupplyExt{}, false
	}
	if len(top.Oem) == 0 || string(top.Oem) == "null" {
		return RedpoolPowerSupplyExt{}, false
	}
	var oem struct {
		Redpool *RedpoolPowerSupplyExt `json:"Redpool"`
	}
	if err := json.Unmarshal(top.Oem, &oem); err != nil || oem.Redpool == nil {
		return RedpoolPowerSupplyExt{}, false
	}
	return *oem.Redpool, true
}
