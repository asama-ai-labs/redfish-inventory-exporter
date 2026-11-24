// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Asama Inc.

package collectors

import (
	"fmt"
	"log"

	"github.com/asama-ai-labs/redfish-inventory-exporter/models"
)

func (c *Collector) GetBiosAttributes() (*models.BiosAttributes, error) {
	systems, err := c.client.Service.Systems()
	if err != nil || len(systems) == 0 {
		return nil, err
	}
	system := systems[0]

	bios, err := system.Bios()
	if err != nil {
		return nil, err
	}

	if bios == nil {
		log.Printf("GetBiosAttributes: system has no Bios link (system.Bios() returned nil); check ComputerSystem JSON has Bios as object {\"@odata.id\": \"...\"}")
		return &models.BiosAttributes{
			Attributes: make(map[string]models.BiosAttribute),
		}, nil
	}

	attributes := &models.BiosAttributes{
		Attributes: make(map[string]models.BiosAttribute),
	}

	for name, value := range bios.Attributes {
		var valueStr string
		switch v := value.(type) {
		case string:
			valueStr = v
		case int:
			valueStr = fmt.Sprintf("%d", v)
		case float64:
			valueStr = fmt.Sprintf("%.2f", v)
		case bool:
			valueStr = fmt.Sprintf("%t", v)
		case nil:
			valueStr = ""
		default:
			valueStr = fmt.Sprintf("%v", v)
		}

		attributes.Attributes[name] = models.BiosAttribute{
			Name:  name,
			Value: valueStr,
		}
	}

	return attributes, nil
}

// SetBiosMetrics sets BIOS attribute gauges (used by collect.go).
func (c *Collector) SetBiosMetrics(biosAttrs *models.BiosAttributes) {
	if biosAttrs != nil {
		for name, attr := range biosAttrs.Attributes {
			c.setGauge("redfish_bios_attributes", map[string]string{
				"attribute_name":  name,
				"attribute_value": attr.Value,
			}, 1)
		}
	}
}
