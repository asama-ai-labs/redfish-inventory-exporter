// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Asama Inc.

package collectors

import (
	"encoding/json"
	"testing"

	"github.com/stmcginnis/gofish/redfish"
)

func TestProcessorMemoryLabelFromStoredJSON(t *testing.T) {
	payload := []byte(`{
		"Id": "1",
		"TDPWatts": 205,
		"ProcessorMemory": [
			{"MemoryType": "L1Cache", "CapacityMiB": 2, "SpeedMHz": null},
			{"MemoryType": "L2Cache", "CapacityMiB": 40, "SpeedMHz": null},
			{"MemoryType": "L3Cache", "CapacityMiB": 0, "SpeedMHz": null}
		]
	}`)
	var p redfish.Processor
	if err := json.Unmarshal(payload, &p); err != nil {
		t.Fatal(err)
	}
	if len(p.ProcessorMemory) != 0 {
		t.Fatalf("gofish populated ProcessorMemory: %+v", p.ProcessorMemory)
	}
	if got := processorMemoryLabel(&p); got != "L1Cache:2|L2Cache:40" {
		t.Fatalf("got %q", got)
	}
}

func TestProcessorMemoryLabelEmptyWithoutBody(t *testing.T) {
	if got := processorMemoryLabel(nil); got != "" {
		t.Fatalf("nil processor: %q", got)
	}
	if got := processorMemoryLabel(&redfish.Processor{}); got != "" {
		t.Fatalf("empty processor: %q", got)
	}
}
