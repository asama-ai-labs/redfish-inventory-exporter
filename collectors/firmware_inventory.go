// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Asama Inc.

package collectors

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	"github.com/stmcginnis/gofish/common"
)

type firmwareInventoryRedpool struct {
	InventorySlot    string `json:"inventory_slot"`
	InstallationDate string `json:"installation_date"`
	DeviceContext    string `json:"device_context"`
	RelatedItemURI   string `json:"related_item_uri"`
	RelatedKind      string `json:"related_kind"`
	RelatedRef       string `json:"related_ref"`
	RelatedScope     string `json:"related_scope"`
	RelatedLocation  string `json:"related_location"`
	ComponentID      string `json:"component_id"`
}

type firmwareInventoryDoc struct {
	ID                     string          `json:"Id"`
	Name                   string          `json:"Name"`
	Description            string          `json:"Description"`
	Version                string          `json:"Version"`
	VersionString          string          `json:"VersionString"`
	SoftwareID             string          `json:"SoftwareId"`
	Manufacturer           string          `json:"Manufacturer"`
	ReleaseDate            string          `json:"ReleaseDate"`
	LowestSupportedVersion string          `json:"LowestSupportedVersion"`
	Updateable             *bool           `json:"Updateable"`
	WriteProtected         *bool           `json:"WriteProtected"`
	Status                 json.RawMessage `json:"Status"`
	Oem                    json.RawMessage `json:"Oem"`
}

type firmwareInventoryStatus struct {
	Health       string `json:"Health"`
	State        string `json:"State"`
	HealthRollup string `json:"HealthRollup"`
}

func (c *Collector) GetFirmwareInventory() ([]firmwareInventoryDoc, error) {
	if c.client == nil {
		return nil, nil
	}
	uris, err := c.firmwareInventoryMemberURIs()
	if err != nil || len(uris) == 0 {
		return nil, err
	}
	out := make([]firmwareInventoryDoc, 0, len(uris))
	for _, uri := range uris {
		doc, err := fetchFirmwareInventoryDoc(c.client, uri)
		if err != nil || doc == nil {
			continue
		}
		out = append(out, *doc)
	}
	return out, nil
}

func firmwareInventoryCollectionURI(updateServiceRaw []byte) string {
	if len(updateServiceRaw) == 0 {
		return ""
	}
	var us struct {
		FirmwareInventory struct {
			ODataID string `json:"@odata.id"`
		} `json:"FirmwareInventory"`
		SoftwareInventory struct {
			ODataID string `json:"@odata.id"`
		} `json:"SoftwareInventory"`
	}
	if err := json.Unmarshal(updateServiceRaw, &us); err != nil {
		return ""
	}
	if us.FirmwareInventory.ODataID != "" {
		return us.FirmwareInventory.ODataID
	}
	return us.SoftwareInventory.ODataID
}

func (c *Collector) firmwareInventoryMemberURIs() ([]string, error) {
	updateService, err := c.client.Service.UpdateService()
	if err != nil {
		return nil, err
	}
	if updateService == nil {
		return nil, nil
	}
	seen := map[string]bool{}
	for _, uri := range []string{
		firmwareInventoryCollectionURI(updateService.RawData),
		"/redfish/v1/UpdateService/FirmwareInventory",
		"/redfish/v1/UpdateService/SoftwareInventory",
	} {
		if uri == "" || seen[uri] {
			continue
		}
		seen[uri] = true
		uris, err := fetchInventoryCollectionMemberURIs(c.client, uri)
		if err == nil && len(uris) > 0 {
			return uris, nil
		}
	}
	return nil, nil
}

func fetchInventoryCollectionMemberURIs(client common.Client, collectionURI string) ([]string, error) {
	resp, err := client.Get(collectionURI)
	if err != nil || resp == nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("firmware inventory collection status %d", resp.StatusCode)
	}
	var coll struct {
		Members []struct {
			ODataID string `json:"@odata.id"`
		} `json:"Members"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&coll); err != nil {
		return nil, err
	}
	uris := make([]string, 0, len(coll.Members))
	for _, m := range coll.Members {
		if m.ODataID != "" {
			uris = append(uris, m.ODataID)
		}
	}
	return uris, nil
}

func fetchFirmwareInventoryDoc(client common.Client, uri string) (*firmwareInventoryDoc, error) {
	if client == nil || uri == "" {
		return nil, fmt.Errorf("missing client or uri")
	}
	resp, err := client.Get(uri)
	if err != nil || resp == nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status %d", resp.StatusCode)
	}
	var doc firmwareInventoryDoc
	if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil {
		return nil, err
	}
	return &doc, nil
}

func parseFirmwareInventoryStatus(raw json.RawMessage) firmwareInventoryStatus {
	var st firmwareInventoryStatus
	if len(raw) == 0 {
		return st
	}
	_ = json.Unmarshal(raw, &st)
	return st
}

func parseFirmwareInventoryRedpool(oemRaw json.RawMessage) firmwareInventoryRedpool {
	var out firmwareInventoryRedpool
	if len(oemRaw) == 0 {
		return out
	}
	var oem map[string]json.RawMessage
	if err := json.Unmarshal(oemRaw, &oem); err != nil {
		return out
	}
	rpRaw, ok := oem["Redpool"]
	if !ok || len(rpRaw) == 0 {
		return out
	}
	var rp map[string]json.RawMessage
	if err := json.Unmarshal(rpRaw, &rp); err != nil {
		return out
	}
	fiRaw, ok := rp["FirmwareInventory"]
	if !ok || len(fiRaw) == 0 {
		return out
	}
	_ = json.Unmarshal(fiRaw, &out)
	return out
}

func dellComponentIDFromOem(oemRaw json.RawMessage) string {
	if len(oemRaw) == 0 {
		return ""
	}
	var oem map[string]json.RawMessage
	if err := json.Unmarshal(oemRaw, &oem); err != nil {
		return ""
	}
	dellRaw, ok := oem["Dell"]
	if !ok {
		return ""
	}
	var dell map[string]json.RawMessage
	if err := json.Unmarshal(dellRaw, &dell); err != nil {
		return ""
	}
	swRaw, ok := dell["DellSoftwareInventory"]
	if !ok {
		return ""
	}
	var sw struct {
		ComponentID interface{} `json:"ComponentID"`
	}
	if err := json.Unmarshal(swRaw, &sw); err != nil {
		return ""
	}
	switch v := sw.ComponentID.(type) {
	case string:
		return v
	case float64:
		return strconv.FormatInt(int64(v), 10)
	case int:
		return strconv.Itoa(v)
	default:
		return ""
	}
}

func boolLabel(v *bool) string {
	if v == nil {
		return ""
	}
	if *v {
		return "true"
	}
	return "false"
}

func effectiveFirmwareVersion(version, versionString string) string {
	if versionString != "" {
		return versionString
	}
	return version
}

func (c *Collector) SetFirmwareInventoryMetrics(docs []firmwareInventoryDoc) {
	for _, doc := range docs {
		st := parseFirmwareInventoryStatus(doc.Status)
		rp := parseFirmwareInventoryRedpool(doc.Oem)

		softwareID := doc.SoftwareID
		if softwareID == "" {
			softwareID = rp.ComponentID
		}
		if softwareID == "" {
			softwareID = dellComponentIDFromOem(doc.Oem)
		}

		inventoryID := doc.ID
		c.setGauge("redfish_firmware_inventory_info", map[string]string{
			"inventory_id":             inventoryID,
			"name":                     doc.Name,
			"description":              doc.Description,
			"version":                  effectiveFirmwareVersion(doc.Version, doc.VersionString),
			"software_id":              softwareID,
			"manufacturer":             doc.Manufacturer,
			"release_date":             doc.ReleaseDate,
			"lowest_supported_version": doc.LowestSupportedVersion,
			"updateable":               boolLabel(doc.Updateable),
			"write_protected":          boolLabel(doc.WriteProtected),
			"inventory_slot":           rp.InventorySlot,
			"installation_date":        rp.InstallationDate,
			"device_context":           rp.DeviceContext,
			"related_kind":             rp.RelatedKind,
			"related_ref":              rp.RelatedRef,
			"related_scope":            rp.RelatedScope,
			"related_location":         rp.RelatedLocation,
		}, 1)
		c.setGauge("redfish_firmware_inventory_health_info", map[string]string{
			"inventory_id":  inventoryID,
			"health":        statusStr(st.Health),
			"health_rollup": statusStr(st.HealthRollup),
		}, 1)
		c.setGauge("redfish_firmware_inventory_state_info", map[string]string{
			"inventory_id": inventoryID,
			"state":        statusStr(st.State),
		}, 1)
	}
}

// collectFirmwareInventory is invoked from CollectMetrics.
func (c *Collector) collectFirmwareInventory() {
	if inv, err := c.GetFirmwareInventory(); err == nil {
		c.SetFirmwareInventoryMetrics(inv)
	}
}
