// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Asama Inc.

package collectors

import "testing"

func TestJoinChildIDsIsOrderIndependent(t *testing.T) {
	a := []string{"HSBackplane2", "Baseboard", "PwrSupply1FRU"}
	b := []string{"PwrSupply1FRU", "HSBackplane2", "Baseboard"}
	want := "Baseboard,HSBackplane2,PwrSupply1FRU"
	if got := joinChildIDs(a); got != want {
		t.Fatalf("joinChildIDs(a) = %q, want %q", got, want)
	}
	if got := joinChildIDs(b); got != want {
		t.Fatalf("joinChildIDs(b) = %q, want %q", got, want)
	}
}

func TestJoinChildIDsDoesNotReorderItsInput(t *testing.T) {
	ids := []string{"B", "A"}
	joinChildIDs(ids)
	if ids[0] != "B" || ids[1] != "A" {
		t.Fatalf("input reordered: %v", ids)
	}
}
