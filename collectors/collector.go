// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Asama Inc.

package collectors

import (
	"bytes"
	"io"
	"strconv"
	"strings"
	"unicode"

	"github.com/VictoriaMetrics/metrics"
	"github.com/stmcginnis/gofish"
)

// pciIDToInt parses a Redfish PCIeFunction PCI id string (VendorId, DeviceId, etc.).
// BMCs disagree on encoding: some send "0x10DE", some "10DE", some decimal "4318" for the
// same 16-bit id. Interpreting a decimal string with hex parsing yields wrong values
// (e.g. "4318" as hex → 17176 instead of 4318). Match redpool normalizers/systems/pci_devices/hpe._pci_id_to_int.
func pciIDToInt(raw string) int {
	s := strings.TrimSpace(raw)
	if s == "" {
		return 0
	}
	if strings.HasPrefix(strings.ToLower(s), "0x") {
		v, err := strconv.ParseUint(s, 0, 32)
		if err != nil {
			return 0
		}
		return int(v & 0xFFFF)
	}
	allDecimal := true
	for _, r := range s {
		if !unicode.IsDigit(r) {
			allDecimal = false
			break
		}
	}
	if allDecimal {
		v, err := strconv.ParseUint(s, 10, 32)
		if err != nil {
			return 0
		}
		return int(v & 0xFFFF)
	}
	v, err := strconv.ParseUint(s, 16, 32)
	if err != nil {
		return 0
	}
	return int(v & 0xFFFF)
}

// statusStr returns the Redfish status string or "Unknown" if empty.
func statusStr(s string) string {
	if s == "" {
		return "Unknown"
	}
	return s
}

// Collector holds the gofish client and per-scrape VictoriaMetrics metrics.Set.
// Vendor-specific normalization (e.g. serial number) is done by redpool normalizers; this collector uses standard Redfish only.
type Collector struct {
	client                   *gofish.APIClient
	set                      *metrics.Set
	disabledCollectors       map[string]bool
	exposePrometheusMetadata bool
	hasRedfishMetrics        bool
	chassisSensorCoverage    map[string]*chassisSensorCoverage
	systemAirflowEmitted     bool
}

// NewCollector creates a Collector with an empty metric set for one scrape.
// disabled is the set of collector names to skip (e.g. from --disable-collectors=bios,bmc).
// When exposePrometheusMetadata is true, /redfish responses include # HELP / # TYPE (see main --expose-prometheus-metadata)
// and documented HELP for health/state-style metrics.
func NewCollector(client *gofish.APIClient, disabled map[string]bool, exposePrometheusMetadata bool) *Collector {
	if disabled == nil {
		disabled = make(map[string]bool)
	}
	c := &Collector{
		client:                   client,
		disabledCollectors:       disabled,
		set:                      metrics.NewSet(),
		exposePrometheusMetadata: exposePrometheusMetadata,
	}
	initMetrics(c)
	return c
}

// Disabled returns true if the named collector is disabled (e.g. bios, bmc, chassis, firmware_inventory, memory, network, pcie, power, processor, storage, thermal).
func (c *Collector) Disabled(name string) bool {
	return c.disabledCollectors[name]
}

// setGauge sets a gauge identified by metric name and label map (Prometheus exposition shape).
func (c *Collector) setGauge(metric string, labels map[string]string, v float64) {
	// The exporter's own scrape stats also start with "redfish_"; they do not count as
	// Redfish data, or an empty scrape would look successful.
	if strings.HasPrefix(metric, "redfish_") && !strings.HasPrefix(metric, exporterNamespace+"_") {
		c.hasRedfishMetrics = true
	}
	name := metric + formatPromLabels(labels)
	c.set.GetOrCreateGauge(name, nil).Set(v)
}

// HasRedfishMetrics reports whether this scrape set at least one redfish_* gauge
// (excludes redfish_inventory_exporter_* scrape stats and go/process metrics).
func (c *Collector) HasRedfishMetrics() bool {
	return c.hasRedfishMetrics
}

// WritePrometheus writes this scrape's metrics plus process metrics in Prometheus text format.
func (c *Collector) WritePrometheus(w io.Writer) {
	if c.exposePrometheusMetadata {
		var buf bytes.Buffer
		c.set.WritePrometheus(&buf)
		var enriched bytes.Buffer
		if err := enrichPrometheusHelp(bytes.NewReader(buf.Bytes()), &enriched); err != nil {
			_, _ = w.Write(buf.Bytes())
		} else {
			_, _ = w.Write(enriched.Bytes())
		}
		metrics.WriteProcessMetrics(w)
		return
	}
	c.set.WritePrometheus(w)
	metrics.WriteProcessMetrics(w)
}

// Reset clears all metrics in this scrape's set.
func (c *Collector) Reset() {
	c.hasRedfishMetrics = false
	c.chassisSensorCoverage = nil
	c.systemAirflowEmitted = false
	c.set.UnregisterAllMetrics()
}
