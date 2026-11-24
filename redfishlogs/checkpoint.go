// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Asama Inc.

package redfishlogs

// CheckpointState matches the redfishlogreceiver checkpoint JSON shape.
type CheckpointState struct {
	Created        string `json:"created"`
	EntryID        string `json:"entry_id"`
	LastEntryCount int    `json:"last_entry_count,omitempty"`
}
