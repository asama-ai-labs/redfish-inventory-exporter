// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Asama Inc.

package collectors

import (
	"encoding/json"
	"testing"

	"github.com/stmcginnis/gofish/redfish"
)

func TestTDPWattsFromJSON(t *testing.T) {
	if got := tdpWattsFromJSON([]byte(`{"Id":"CPU1"}`)); got != nil {
		t.Fatalf("missing TDPWatts = %d, want nil", *got)
	}
	if got := tdpWattsFromJSON([]byte(`{"TDPWatts":250}`)); got == nil || *got != 250 {
		t.Fatalf("TDPWatts 250 = %v, want 250", got)
	}
	if got := tdpWattsFromJSON([]byte(`{"TDPWatts":0}`)); got == nil || *got != 0 {
		t.Fatalf("explicit TDPWatts 0 = %v, want 0", got)
	}
}

func TestOptionalIntLabel(t *testing.T) {
	if got := optionalIntLabel(nil); got != "" {
		t.Fatalf("nil label = %q, want empty", got)
	}
	v := 350
	if got := optionalIntLabel(&v); got != "350" {
		t.Fatalf("350 label = %q", got)
	}
}

func TestProcessorTDPWattsUsesRetainedDocument(t *testing.T) {
	load := func(body string) *redfish.Processor {
		var p redfish.Processor
		if err := json.Unmarshal([]byte(body), &p); err != nil {
			t.Fatal(err)
		}
		p.ODataID = "/redfish/v1/Systems/1/Processors/CPU1"
		return &p
	}
	// No client is set, so any fallback GET would fail.
	if got := processorTDPWatts(load(`{"Id":"CPU1","TDPWatts":0}`)); got == nil || *got != 0 {
		t.Fatalf("explicit TDPWatts 0 = %v, want 0", got)
	}
	if got := processorTDPWatts(load(`{"Id":"CPU1"}`)); got != nil {
		t.Fatalf("omitted TDPWatts = %d, want nil", *got)
	}
}
