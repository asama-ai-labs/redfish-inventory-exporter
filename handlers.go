// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Asama Inc.

package main

import (
	"fmt"
	"net/http"
	"time"

	"github.com/asama-ai-labs/redfish-inventory-exporter/collectors"
)

// Handlers wires the collector to HTTP.
type Handlers struct {
	collector *collectors.Collector
}

// NewHandlers returns handlers backed by the given collector.
func NewHandlers(collector *collectors.Collector) *Handlers {
	return &Handlers{collector: collector}
}

// MetricsHandler runs one Redfish scrape and exposes Prometheus text (Redfish gauges +
// process metrics via VictoriaMetrics/metrics). Optional # HELP / # TYPE lines: main flag --expose-prometheus-metadata.
// Scrape stats use label target=<BMC host:port from ?target=> so each /redfish response only describes that BMC.
// Returns HTTP 500 when collection fails or yields no redfish_* metrics, so scrapers (e.g. vmagent)
// mark the target down. The body is exposition text holding only the scrape stats, with
// redfish_inventory_exporter_up 0, and the error is in X-Redfish-Collector-Error.
func (h *Handlers) MetricsHandler(w http.ResponseWriter, r *http.Request, target string) {
	start := time.Now()
	h.collector.Reset()
	err := h.collector.CollectMetrics(target)
	if err == nil && !h.collector.HasRedfishMetrics() {
		err = fmt.Errorf("no redfish metrics collected")
	}
	if err != nil {
		// Drop any partial Redfish gauges: a failed scrape reports only that it failed.
		h.collector.Reset()
	}
	h.collector.RecordScrapeMetrics(target, start, err)

	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	if err != nil {
		w.Header().Set("X-Redfish-Collector-Error", err.Error())
		w.WriteHeader(http.StatusInternalServerError)
	}
	h.collector.WritePrometheus(w)
}
