// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Asama Inc.

// Package redfishlogs implements Redfish log tailing and OTLP mapping.
// Checkpoint selection logic is mirrored from collector-contrib redfishlogreceiver.
package redfishlogs

import (
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/stmcginnis/gofish/redfish"
)

func sortLogEntries(entries []*redfish.LogEntry) {
	sort.Slice(entries, func(i, j int) bool {
		return logEntryOrderLess(entries[i], entries[j])
	})
}

func logEntryOrderLess(a, b *redfish.LogEntry) bool {
	ta, aOK := parseRedfishTime(a.Created)
	tb, bOK := parseRedfishTime(b.Created)
	if aOK && bOK && !ta.Equal(tb) {
		return ta.Before(tb)
	}
	if aOK != bOK {
		return !aOK
	}
	return compareIDs(a.ID, b.ID) < 0
}

func compareIDs(a, b string) int {
	na, okA := parseNumericID(a)
	nb, okB := parseNumericID(b)
	if okA && okB {
		if na < nb {
			return -1
		}
		if na > nb {
			return 1
		}
		return 0
	}
	return strings.Compare(a, b)
}

func parseNumericID(s string) (uint64, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, false
	}
	if strings.HasPrefix(strings.ToLower(s), "0x") {
		v, err := strconv.ParseUint(s[2:], 16, 64)
		return v, err == nil
	}
	v, err := strconv.ParseUint(s, 10, 64)
	return v, err == nil
}

func maxCreatedTime(entries []*redfish.LogEntry) (time.Time, bool) {
	var maxT time.Time
	ok := false
	for _, e := range entries {
		t, valid := parseRedfishTime(e.Created)
		if !valid {
			continue
		}
		if !ok || t.After(maxT) {
			maxT = t
			ok = true
		}
	}
	return maxT, ok
}

func maxNumericID(entries []*redfish.LogEntry) int64 {
	var max int64 = -1
	for _, e := range entries {
		n, ok := parseNumericID(e.ID)
		if !ok {
			continue
		}
		v := int64(n)
		if max < 0 || v > max {
			max = v
		}
	}
	return max
}

// ShouldResetCheckpoint detects log clears / wraps (ported from redfishlogreceiver).
func ShouldResetCheckpoint(cp *CheckpointState, sorted []*redfish.LogEntry, skew time.Duration) bool {
	if cp == nil || len(sorted) == 0 {
		return false
	}
	maxT, okMax := maxCreatedTime(sorted)
	tcp, okCp := parseRedfishTime(cp.Created)
	if okCp && okMax && tcp.After(maxT.Add(skew)) {
		return true
	}
	maxN := maxNumericID(sorted)
	cpN, cpNumOK := parseNumericID(cp.EntryID)
	if cpNumOK && maxN >= 0 && uint64(maxN) < cpN {
		return true
	}
	return false
}

// SelectEntriesToEmit returns entries to send and the new checkpoint (ported from redfishlogreceiver).
func SelectEntriesToEmit(cp *CheckpointState, sorted []*redfish.LogEntry, initialN int, reset bool) ([]*redfish.LogEntry, *CheckpointState) {
	if len(sorted) == 0 {
		return nil, nil
	}
	if cp == nil || reset {
		tail := sorted
		if len(tail) > initialN {
			tail = tail[len(tail)-initialN:]
		}
		ck := checkpointFromEmitted(tail, len(sorted))
		return tail, ck
	}
	var out []*redfish.LogEntry
	for _, e := range sorted {
		if logEntryAfterCheckpoint(e, cp) {
			out = append(out, e)
		}
	}
	if len(out) == 0 {
		return nil, nil
	}
	ck := checkpointFromEmitted(out, len(sorted))
	return out, ck
}

func logEntryAfterCheckpoint(e *redfish.LogEntry, cp *CheckpointState) bool {
	te, okE := parseRedfishTime(e.Created)
	tcp, okC := parseRedfishTime(cp.Created)
	if !okC {
		return true
	}
	if !okE {
		return true
	}
	if te.After(tcp) {
		return true
	}
	if te.Equal(tcp) && compareIDs(e.ID, cp.EntryID) > 0 {
		return true
	}
	return false
}

func checkpointFromEmitted(emitted []*redfish.LogEntry, totalCount int) *CheckpointState {
	if len(emitted) == 0 {
		return nil
	}
	last := emitted[len(emitted)-1]
	return &CheckpointState{
		Created:        last.Created,
		EntryID:        last.ID,
		LastEntryCount: totalCount,
	}
}

// ParseRedfishTime parses common Redfish timestamp formats.
func ParseRedfishTime(s string) (time.Time, bool) {
	return parseRedfishTime(s)
}

func parseRedfishTime(s string) (time.Time, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, false
	}
	layouts := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02T15:04:05Z07:00",
		"2006-01-02T15:04:05",
	}
	for _, layout := range layouts {
		if t, err := time.Parse(layout, s); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}
