// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Asama Inc.

package redfishlogs

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFileStore_persistence(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "checkpoints.json")

	s1, err := NewFileStore(path)
	if err != nil {
		t.Fatal(err)
	}
	key := CheckpointKey("10.0.0.1", "/redfish/v1/Managers/1/LogServices/SEL")
	cp := &CheckpointState{Created: "2020-01-01T00:00:00Z", EntryID: "9", LastEntryCount: 100}
	if err := s1.SetMany(map[string]*CheckpointState{key: cp}); err != nil {
		t.Fatal(err)
	}

	s2, err := NewFileStore(path)
	if err != nil {
		t.Fatal(err)
	}
	got := s2.Get(key)
	if got == nil || got.EntryID != "9" || got.LastEntryCount != 100 {
		t.Fatalf("got %#v", got)
	}
}

func TestFileStore_atomicWriteNoTempLeft(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "checkpoints.json")

	s, err := NewFileStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetMany(map[string]*CheckpointState{
		"redfishlog/t/k": {Created: "2020-01-01T00:00:00Z", EntryID: "1"},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Fatal("expected no leftover temp file")
	}
}
