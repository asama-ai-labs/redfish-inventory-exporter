// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Asama Inc.

package collectors

import "testing"

func TestThermalMemberSkipped(t *testing.T) {
	tests := []struct {
		state string
		want  bool
	}{
		{state: "Absent", want: true},
		{state: "Disabled", want: true},
		{state: "Enabled", want: false},
		{state: "StandbyOffline", want: false},
		{state: "", want: false},
	}
	for _, tt := range tests {
		if got := thermalMemberSkipped(tt.state); got != tt.want {
			t.Errorf("thermalMemberSkipped(%q) = %v, want %v", tt.state, got, tt.want)
		}
	}
}
