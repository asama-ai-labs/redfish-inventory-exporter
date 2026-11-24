// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Asama Inc.

// Package redpoolext holds optional Redfish payload shapes produced by Redpool (Oem.Redpool, etc.).
// Keep this package small and vendor-neutral so it can be moved to a shared module later.
package redpoolext

import "encoding/json"

// RedpoolMemoryExt is the JSON shape of Memory.Oem.Redpool after Redpool normalization.
type RedpoolMemoryExt struct {
	ManufactureWeek *int   `json:"ManufactureWeek,omitempty"`
	ManufactureYear *int   `json:"ManufactureYear,omitempty"`
	ManufactureDate string `json:"ManufactureDate,omitempty"` // YYYY-MM-DD when Redpool parsed OEM date
	FruPartNumber   string `json:"FruPartNumber,omitempty"`
	EccBits         *int   `json:"EccBits,omitempty"`
}

// ParseRedpoolMemoryExtFromJSON extracts Oem.Redpool from a Memory resource JSON body.
func ParseRedpoolMemoryExtFromJSON(body []byte) (RedpoolMemoryExt, bool) {
	var top struct {
		Oem json.RawMessage `json:"Oem"`
	}
	if err := json.Unmarshal(body, &top); err != nil {
		return RedpoolMemoryExt{}, false
	}
	if len(top.Oem) == 0 || string(top.Oem) == "null" {
		return RedpoolMemoryExt{}, false
	}
	var oem struct {
		Redpool *RedpoolMemoryExt `json:"Redpool"`
	}
	if err := json.Unmarshal(top.Oem, &oem); err != nil || oem.Redpool == nil {
		return RedpoolMemoryExt{}, false
	}
	return *oem.Redpool, true
}
