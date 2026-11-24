// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Asama Inc.

package redfishlogs

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
)

// FileStore persists checkpoints as a single JSON object keyed by storageKey.
// SetMany rewrites the entire file atomically on each successful export batch.
// This is simple and durable but serializes writes; suitable for low hundreds of BMCs.
type FileStore struct {
	mu    sync.Mutex
	path  string
	cache map[string]*CheckpointState
}

// NewFileStore loads existing checkpoints from path, or starts empty if missing.
func NewFileStore(path string) (*FileStore, error) {
	s := &FileStore{path: path, cache: make(map[string]*CheckpointState)}
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return s, nil
		}
		return nil, fmt.Errorf("redfishlogs: read checkpoint file: %w", err)
	}
	if len(strings.TrimSpace(string(b))) == 0 {
		return s, nil
	}
	if err := json.Unmarshal(b, &s.cache); err != nil {
		return nil, fmt.Errorf("redfishlogs: parse checkpoint file: %w", err)
	}
	return s, nil
}

// Get returns a copy of the checkpoint for key, or nil if absent.
func (s *FileStore) Get(key string) *CheckpointState {
	s.mu.Lock()
	defer s.mu.Unlock()
	if v, ok := s.cache[key]; ok && v != nil {
		cp := *v
		return &cp
	}
	return nil
}

// SetMany merges updates into the cache and writes the file atomically.
func (s *FileStore) SetMany(updates map[string]*CheckpointState) error {
	if len(updates) == 0 {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for k, v := range updates {
		if v == nil {
			delete(s.cache, k)
			continue
		}
		cp := *v
		s.cache[k] = &cp
	}
	data, err := json.MarshalIndent(s.cache, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

// CheckpointKey builds a stable key for a target + log service OData ID.
func CheckpointKey(targetKey, logServiceODataID string) string {
	safe := strings.ReplaceAll(strings.TrimPrefix(logServiceODataID, "/"), "/", "_")
	if len(safe) > 200 {
		safe = safe[:200]
	}
	return fmt.Sprintf("redfishlog/%s/%s", targetKey, safe)
}
