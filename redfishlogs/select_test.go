// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Asama Inc.

package redfishlogs

import (
	"strconv"
	"testing"
	"time"

	"github.com/stmcginnis/gofish/common"
	"github.com/stmcginnis/gofish/redfish"
	logspb "go.opentelemetry.io/proto/otlp/logs/v1"
)

func TestSelectEntriesToEmit_incremental(t *testing.T) {
	cp := &CheckpointState{Created: "2020-01-01T00:00:00Z", EntryID: "1"}
	sorted := []*redfish.LogEntry{
		{Entity: common.Entity{ID: "1"}, Created: "2020-01-01T00:00:00Z", Message: "a"},
		{Entity: common.Entity{ID: "2"}, Created: "2020-01-02T00:00:00Z", Message: "b"},
	}
	out, newCk := SelectEntriesToEmit(cp, sorted, 500, false)
	if len(out) != 1 || out[0].ID != "2" {
		t.Fatalf("unexpected out: %#v", out)
	}
	if newCk == nil || newCk.EntryID != "2" {
		t.Fatalf("unexpected ck: %#v", newCk)
	}
}

func TestShouldResetCheckpoint_numericRegression(t *testing.T) {
	cp := &CheckpointState{Created: "2020-01-01T00:00:00Z", EntryID: "50"}
	sorted := []*redfish.LogEntry{
		{Entity: common.Entity{ID: "1"}, Created: "2020-01-02T00:00:00Z"},
	}
	if !ShouldResetCheckpoint(cp, sorted, time.Minute) {
		t.Fatal("expected reset when max id < checkpoint id")
	}
}

func TestShouldResetCheckpoint_clockSkew(t *testing.T) {
	cp := &CheckpointState{Created: "2025-01-01T00:00:00Z", EntryID: "1"}
	sorted := []*redfish.LogEntry{
		{Entity: common.Entity{ID: "2"}, Created: "2024-01-01T00:00:00Z"},
	}
	if !ShouldResetCheckpoint(cp, sorted, time.Minute) {
		t.Fatal("expected reset when checkpoint time is after max entry time + skew")
	}
}

func TestSelectEntriesToEmit_initialTail(t *testing.T) {
	sorted := make([]*redfish.LogEntry, 0, 10)
	for i := 1; i <= 10; i++ {
		sorted = append(sorted, &redfish.LogEntry{
			Entity:  common.Entity{ID: strconv.Itoa(i)},
			Created: "2020-01-01T00:00:00Z",
		})
	}
	out, ck := SelectEntriesToEmit(nil, sorted, 3, false)
	if len(out) != 3 {
		t.Fatalf("tail len: %d", len(out))
	}
	if ck == nil || ck.LastEntryCount != 10 {
		t.Fatalf("ck: %#v", ck)
	}
}

func TestBuildResourceLogs_credentialKey(t *testing.T) {
	rl := buildResourceLogs(
		"https://10.0.0.1",
		"10.0.0.1",
		"host1",
		"SN1",
		&redfish.Manager{Entity: common.Entity{ID: "1", Name: "mgr"}},
		&redfish.LogService{Entity: common.Entity{ID: "SEL", ODataID: "/LogServices/SEL"}},
		nil,
	)
	attrs := map[string]string{}
	for _, a := range rl.Resource.Attributes {
		attrs[a.Key] = a.GetValue().GetStringValue()
	}
	if attrs["bmc.credential_key"] != "10.0.0.1" {
		t.Fatalf("bmc.credential_key: %q", attrs["bmc.credential_key"])
	}
	if attrs["service.name"] != "SEL" {
		t.Fatalf("service.name: %q", attrs["service.name"])
	}
	if attrs["host.name"] != "host1" {
		t.Fatalf("host.name: %q", attrs["host.name"])
	}
	if attrs["serial.number"] != "SN1" {
		t.Fatalf("serial.number: %q", attrs["serial.number"])
	}
	if attrs["host.serial_number"] != "SN1" {
		t.Fatalf("host.serial_number: %q", attrs["host.serial_number"])
	}
}

func TestLogServiceName_prefersName(t *testing.T) {
	ls := &redfish.LogService{Entity: common.Entity{ID: "SEL", Name: "System Event Log"}}
	if got := logServiceName(ls); got != "System Event Log" {
		t.Fatalf("got %q", got)
	}
}

func TestCountLogRecords(t *testing.T) {
	rl := buildResourceLogs(
		"https://10.0.0.1",
		"10.0.0.1",
		"host1",
		"SN1",
		&redfish.Manager{Entity: common.Entity{ID: "1", Name: "mgr"}},
		&redfish.LogService{Entity: common.Entity{ID: "SEL", ODataID: "/LogServices/SEL"}},
		[]*redfish.LogEntry{
			{Entity: common.Entity{ID: "1"}, Message: "a"},
			{Entity: common.Entity{ID: "2"}, Message: "b"},
		},
	)
	if got := CountLogRecords([]*logspb.ResourceLogs{rl}); got != 2 {
		t.Fatalf("count: %d", got)
	}
}
