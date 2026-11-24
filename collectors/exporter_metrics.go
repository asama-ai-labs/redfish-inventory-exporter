// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Asama Inc.

package collectors

import (
	"log"
	"time"
)

// Namespace for exporter-level metrics (scrape stats, same names as before).
const exporterNamespace = "redfish_inventory_exporter"

// RecordScrapeMetrics records total scrape time and up for this request.
func (c *Collector) RecordScrapeMetrics(target string, start time.Time, err error) {
	d := time.Since(start).Seconds()
	c.setGauge(exporterNamespace+"_scrape_duration_seconds", map[string]string{"target": target}, d)
	if err != nil {
		c.setGauge(exporterNamespace+"_up", map[string]string{"target": target}, 0)
	} else {
		c.setGauge(exporterNamespace+"_up", map[string]string{"target": target}, 1)
	}
}

// RecordCollectorSuccess records whether one named collector subsystem read everything it
// asked for during this scrape: 1 when it did, 0 when any request failed. A 0 can still
// come with partial metrics, because members that were read are kept.
// Metric: redfish_inventory_exporter_collector_success{target,collector}.
func (c *Collector) RecordCollectorSuccess(target, collector string, err error) {
	v := 1.0
	if err != nil {
		v = 0
		log.Printf("collector %s for %s is degraded: %v", collector, target, err)
	}
	c.setGauge(exporterNamespace+"_collector_success", map[string]string{
		"target":    target,
		"collector": collector,
	}, v)
}

// RecordCollectorDuration records wall time for one named collector subsystem during this scrape.
// Single metric: redfish_inventory_exporter_scrape_collector_duration_seconds{target,collector}.
func (c *Collector) RecordCollectorDuration(target, collector string, start time.Time) {
	c.setGauge(exporterNamespace+"_scrape_collector_duration_seconds", map[string]string{
		"target":    target,
		"collector": collector,
	}, time.Since(start).Seconds())
}
