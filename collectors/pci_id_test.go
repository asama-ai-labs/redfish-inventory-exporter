// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Asama Inc.

package collectors

import (
	"strconv"
	"strings"
	"testing"
)

func TestPciIDToInt(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in   string
		want int
	}{
		{"", 0},
		{"   ", 0},
		{"0x10DE", 4318},
		{"0X10de", 4318},
		{"10DE", 4318},
		{"10de", 4318},
		// HPE / some iLO builds emit decimal strings for the same 16-bit id as canonical hex 0x10DE.
		{"4318", 4318},
		// Old bug: "4318" parsed only as hex → 17176.
		{"0x4318", 17176},
		{"  4318  ", 4318},
		{"65535", 65535},
		{"0", 0},
		{"0000", 0},
		{"invalid", 0},
	}
	for i, tt := range tests {
		t.Run(strconv.Itoa(i)+"_"+strings.ReplaceAll(tt.in, " ", "_"), func(t *testing.T) {
			t.Parallel()
			got := pciIDToInt(tt.in)
			if got != tt.want {
				t.Fatalf("pciIDToInt(%q) = %d, want %d", tt.in, got, tt.want)
			}
		})
	}
}
