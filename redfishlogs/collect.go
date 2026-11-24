// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Asama Inc.

package redfishlogs

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/stmcginnis/gofish"
	"github.com/stmcginnis/gofish/redfish"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	logspb "go.opentelemetry.io/proto/otlp/logs/v1"
	resourcepb "go.opentelemetry.io/proto/otlp/resource/v1"
)

// scopeName is the OTLP instrumentation scope (maps to ClickHouse ScopeName).
const scopeName = "Bmc logs"

// Logger receives non-fatal collect warnings and informational events.
type Logger interface {
	Warn(msg string, fields map[string]any)
	Info(msg string, fields map[string]any)
}

// CollectConfig controls log tailing and checkpointing.
type CollectConfig struct {
	InitialLastN      int
	LogResetClockSkew time.Duration
}

// CollectFromClient scrapes Manager LogService entries using an existing gofish session.
// redfishEndpoint is used for fingerprints and resource attributes (e.g. https://10.0.0.1).
func CollectFromClient(
	ctx context.Context,
	client *gofish.APIClient,
	redfishEndpoint string,
	targetKey string,
	hostName string,
	serialNumber string,
	cfg CollectConfig,
	store *FileStore,
	logger Logger,
) ([]*logspb.ResourceLogs, map[string]*CheckpointState, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}

	initialN := cfg.InitialLastN
	if initialN < 1 {
		initialN = 500
	}
	skew := cfg.LogResetClockSkew
	if skew <= 0 {
		skew = 5 * time.Minute
	}

	managers, err := client.Service.Managers()
	if err != nil {
		return nil, nil, fmt.Errorf("managers: %w", err)
	}

	var out []*logspb.ResourceLogs
	updates := make(map[string]*CheckpointState)

	for _, mgr := range managers {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		logSvcs, err := mgr.LogServices()
		if err != nil {
			if logger != nil {
				logger.Warn("redfish log services", map[string]any{
					"target":  targetKey,
					"manager": mgr.ID,
					"error":   err.Error(),
				})
			}
			continue
		}
		for _, ls := range logSvcs {
			if err := ctx.Err(); err != nil {
				return nil, nil, err
			}
			entries, err := ls.Entries()
			if err != nil {
				if logger != nil {
					logger.Warn("redfish log entries", map[string]any{
						"target":      targetKey,
						"log_service": ls.ODataID,
						"error":       err.Error(),
					})
				}
				continue
			}
			ckKey := CheckpointKey(targetKey, ls.ODataID)
			var cp *CheckpointState
			if store != nil {
				cp = store.Get(ckKey)
			}

			sorted := append([]*redfish.LogEntry(nil), entries...)
			sortLogEntries(sorted)

			reset := ShouldResetCheckpoint(cp, sorted, skew)
			if reset && logger != nil {
				logger.Info("redfish log reset detected", map[string]any{
					"target":      targetKey,
					"log_service": ls.ODataID,
				})
			}
			toEmit, newCk := SelectEntriesToEmit(cp, sorted, initialN, reset)
			if len(toEmit) == 0 {
				continue
			}

			rl := buildResourceLogs(redfishEndpoint, targetKey, hostName, serialNumber, mgr, ls, toEmit)
			out = append(out, rl)
			if newCk != nil {
				updates[ckKey] = newCk
			}
		}
	}

	return out, updates, nil
}

// CountLogRecords returns the total log records across ResourceLogs batches.
func CountLogRecords(resourceLogs []*logspb.ResourceLogs) int {
	n := 0
	for _, rl := range resourceLogs {
		for _, sl := range rl.GetScopeLogs() {
			n += len(sl.GetLogRecords())
		}
	}
	return n
}

func buildResourceLogs(
	redfishEndpoint, targetKey, hostName, serialNumber string,
	mgr *redfish.Manager,
	ls *redfish.LogService,
	entries []*redfish.LogEntry,
) *logspb.ResourceLogs {
	res := &resourcepb.Resource{
		Attributes: []*commonpb.KeyValue{
			kvStr("service.name", logServiceName(ls)),
			kvStr("service.namespace", "redfish"),
			kvStr("bmc.target", targetKey),
			kvStr("bmc.credential_key", targetKey),
			kvStr("bmc.endpoint", redfishEndpoint),
			kvStr("host.name", hostName),
			kvStr("host.serial_number", serialNumber),
			kvStr("serial.number", serialNumber),
			kvStr("redfish.manager.id", mgr.ID),
			kvStr("redfish.manager.name", mgr.Name),
			kvStr("redfish.log_service.id", ls.ID),
			kvStr("redfish.log_service.odata_id", ls.ODataID),
		},
	}

	now := uint64(time.Now().UnixNano())
	sl := &logspb.ScopeLogs{
		Scope: &commonpb.InstrumentationScope{
			Name:    scopeName,
			Version: "0.0.0",
		},
		LogRecords: make([]*logspb.LogRecord, 0, len(entries)),
	}

	for _, ent := range entries {
		sl.LogRecords = append(sl.LogRecords, buildLogRecord(redfishEndpoint, ls.ODataID, ent, now))
	}

	return &logspb.ResourceLogs{
		Resource: res,
		ScopeLogs: []*logspb.ScopeLogs{
			sl,
		},
	}
}

func buildLogRecord(redfishEndpoint, logSvcOData string, ent *redfish.LogEntry, observedNano uint64) *logspb.LogRecord {
	ts := entryTimestamp(ent)
	body := strings.TrimSpace(ent.Message)
	if body == "" {
		body = strings.TrimSpace(ent.Description)
	}
	if body == "" {
		body = fmt.Sprintf("%s %s", ent.EntryType, ent.Severity)
	}

	sevNum, sevText := mapSeverityProto(ent.Severity)

	fp := entryFingerprint(redfishEndpoint, logSvcOData, ent.ID, ent.Created)
	attrs := []*commonpb.KeyValue{
		kvStr("redfish.entry_fingerprint", fp),
		kvStr("redfish.log_entry.id", ent.ID),
		kvStr("redfish.log_entry.created", ent.Created),
		kvStr("redfish.log_entry.type", string(ent.EntryType)),
	}
	if ent.EventID != "" {
		attrs = append(attrs, kvStr("redfish.log_entry.event_id", ent.EventID))
	}
	if ent.GeneratorID != "" {
		attrs = append(attrs, kvStr("redfish.log_entry.generator_id", ent.GeneratorID))
	}

	return &logspb.LogRecord{
		TimeUnixNano:         uint64(ts.UnixNano()),
		ObservedTimeUnixNano: observedNano,
		SeverityNumber:       sevNum,
		SeverityText:         sevText,
		Body: &commonpb.AnyValue{
			Value: &commonpb.AnyValue_StringValue{StringValue: body},
		},
		Attributes: attrs,
	}
}

func entryTimestamp(ent *redfish.LogEntry) time.Time {
	if ent.EventTimestamp != "" {
		if t, ok := parseRedfishTime(ent.EventTimestamp); ok {
			return t
		}
	}
	if t, ok := parseRedfishTime(ent.Created); ok {
		return t
	}
	return time.Now()
}

func mapSeverityProto(s redfish.EventSeverity) (logspb.SeverityNumber, string) {
	switch s {
	case redfish.CriticalEventSeverity:
		return logspb.SeverityNumber_SEVERITY_NUMBER_FATAL, string(s)
	case redfish.WarningEventSeverity:
		return logspb.SeverityNumber_SEVERITY_NUMBER_WARN, string(s)
	case redfish.OKEventSeverity:
		return logspb.SeverityNumber_SEVERITY_NUMBER_INFO, string(s)
	default:
		if s == "" {
			return logspb.SeverityNumber_SEVERITY_NUMBER_UNSPECIFIED, ""
		}
		return logspb.SeverityNumber_SEVERITY_NUMBER_INFO, string(s)
	}
}

func entryFingerprint(endpoint, logSvcOData, id, created string) string {
	sum := sha256.Sum256([]byte(endpoint + "\x00" + logSvcOData + "\x00" + id + "\x00" + created))
	return hex.EncodeToString(sum[:])
}

func kvStr(k, v string) *commonpb.KeyValue {
	return &commonpb.KeyValue{
		Key: k,
		Value: &commonpb.AnyValue{
			Value: &commonpb.AnyValue_StringValue{StringValue: v},
		},
	}
}

// logServiceName returns the Redfish LogService name for service.name (ClickHouse ServiceName).
func logServiceName(ls *redfish.LogService) string {
	if ls == nil {
		return "unknown"
	}
	if name := strings.TrimSpace(ls.Name); name != "" {
		return name
	}
	if id := strings.TrimSpace(ls.ID); id != "" {
		return id
	}
	return "unknown"
}
