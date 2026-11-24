// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Asama Inc.

package collectors

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/asama-ai-labs/redfish-inventory-exporter/models"
	"github.com/asama-ai-labs/redfish-inventory-exporter/redpoolext"
	"github.com/stmcginnis/gofish"
	"github.com/stmcginnis/gofish/common"
	"github.com/stmcginnis/gofish/redfish"
)

func (c *Collector) GetStorageInfo() (*models.StorageInfo, error) {
	systems, err := c.client.Service.Systems()
	if err != nil || len(systems) == 0 {
		return nil, err
	}
	system := systems[0]

	var partial []error
	storageList, err := system.Storage()
	if !keepPartial("storage", err, &partial) {
		return nil, err
	}

	var info models.StorageInfo
	for _, s := range storageList {
		driveVolIDs := map[string][]string{}

		volumes, volErr := s.Volumes()
		if keepPartial("storage volumes", volErr, &partial) {
			for _, v := range volumes {
				volID := v.ID
				if volID != "" {
					if members, err := v.Drives(); err == nil {
						for _, d := range members {
							if d != nil && d.ID != "" {
								driveVolIDs[d.ID] = append(driveVolIDs[d.ID], volID)
							}
						}
					}
				}
				vname := v.DisplayName
				if vname == "" {
					vname = v.Name
				}
				info.Volumes = append(info.Volumes, models.StorageVolume{
					ID:                 v.ID,
					StorageID:          s.ID,
					Name:               vname,
					RAIDType:           string(v.RAIDType),
					VolumeType:         string(v.VolumeType),
					CapacityBytes:      v.CapacityBytes,
					BlockSizeBytes:     v.BlockSizeBytes,
					StripSizeBytes:     v.StripSizeBytes,
					OptimumIOSizeBytes: v.OptimumIOSizeBytes,
					ReadCachePolicy:    string(v.ReadCachePolicy),
					WriteCachePolicy:   string(v.WriteCachePolicy),
					WriteCacheState:    string(v.WriteCacheState),
					Encrypted:          v.Encrypted,
					EncryptionTypes:    joinEncryptionTypes(v.EncryptionTypes),
					IsBootCapable:      v.IsBootCapable,
					LogicalUnitNumber:  v.LogicalUnitNumber,
					ProvisioningPolicy: string(v.ProvisioningPolicy),
					IOPerfModeEnabled:  v.IOPerfModeEnabled,
					Health:             statusStr(string(v.Status.Health)),
					State:              statusStr(string(v.Status.State)),
					HealthRollup:       statusStr(string(v.Status.HealthRollup)),
				})
			}
		}

		drives, err := s.Drives()
		if !keepPartial("storage drives", err, &partial) {
			drives = nil
		}
		for _, d := range drives {
			if d == nil {
				continue
			}
			// Skip BMC stub drives (broken URI → empty id / Unknown / 0 capacity).
			if d.ID == "" && d.CapacityBytes == 0 &&
				(string(d.Status.Health) == "" || string(d.Status.Health) == "OK" || statusStr(string(d.Status.Health)) == "Unknown") &&
				d.Model == "" && d.SerialNumber == "" {
				continue
			}
			volIDs := append([]string(nil), driveVolIDs[d.ID]...)
			sort.Strings(volIDs)
			volJoin := strings.Join(volIDs, ",")
			spare := driveRedpoolFloat(d, "AvailableSparePercent")
			drive := models.Drive{
				ID:                            d.ID,
				StorageID:                     s.ID,
				Model:                         d.Model,
				SerialNumber:                  d.SerialNumber,
				CapacityBytes:                 d.CapacityBytes,
				MediaType:                     string(d.MediaType),
				Health:                        statusStr(string(d.Status.Health)),
				State:                         statusStr(string(d.Status.State)),
				HealthRollup:                  statusStr(string(d.Status.HealthRollup)),
				Manufacturer:                  d.Manufacturer,
				Protocol:                      string(d.Protocol),
				NegotiatedSpeedGbps:           d.NegotiatedSpeedGbs,
				CapableSpeedGbps:              d.CapableSpeedGbs,
				PartNumber:                    d.PartNumber,
				Revision:                      d.Revision,
				EncryptionAbility:             string(d.EncryptionAbility),
				EncryptionStatus:              string(d.EncryptionStatus),
				FailurePredicted:              d.FailurePredicted,
				PredictedMediaLifeLeftPercent: d.PredictedMediaLifeLeftPercent,
				BlockSizeBytes:                d.BlockSizeBytes,
				WriteCacheEnabled:             d.WriteCacheEnabled,
				HotPluggable:                  driveHotPluggableFromJSON(d),
				StatusIndicator:               string(d.StatusIndicator),
				Multipath:                     d.Multipath,
				VolumeIDs:                     volJoin,
				Identifiers:                   formatIdentifiers(d.Identifiers),
				Location:                      driveLocationSummary(d),
				DriveFormFactor:               resolveDriveFormFactor(d),
				RaidStatus:                    driveRedpoolString(d, "RaidStatus"),
				PredictiveFailureState:        driveRedpoolString(d, "PredictiveFailureState"),
				CPUAffinity:                   driveRedpoolString(d, "CPUAffinity"),
				AvailableSparePercent:         spare,
			}
			info.Drives = append(info.Drives, drive)
		}

		ctrls, batteries := collectStorageControllers(s)
		if oem := batteriesFromStorageOEMJSON(s, c.client); len(oem) > 0 && len(batteries) == 0 {
			batteries = oem
		}
		info.Controllers = append(info.Controllers, ctrls...)
		info.Batteries = append(info.Batteries, batteries...)
	}
	return &info, errors.Join(partial...)
}

func collectStorageControllers(s *redfish.Storage) ([]models.StorageController, []models.StorageBattery) {
	var out []models.StorageController
	var batteries []models.StorageBattery
	storageURI := strings.TrimSuffix(s.ODataID, "/")
	cli := s.GetClient()
	// Prefer Controllers collection (non-deprecated) so Links.Batteries from
	// normalization are reachable; embedded StorageControllers often omit them.
	ctrls, err := s.Controllers()
	if err == nil && len(ctrls) > 0 {
		for i, sc := range ctrls {
			if sc == nil {
				continue
			}
			ctrl := redfishStorageControllerToModel(s.ID, i, sc)
			out = append(out, ctrl)
			batteries = append(batteries, collectStorageControllerBatteries(s.ID, ctrl.ID, sc, storageURI, cli)...)
		}
		return out, batteries
	}
	if len(s.StorageControllers) > 0 {
		for i := range s.StorageControllers {
			ctrl := redfishStorageControllerToModel(s.ID, i, &s.StorageControllers[i])
			out = append(out, ctrl)
			batteries = append(batteries, collectStorageControllerBatteries(s.ID, ctrl.ID, &s.StorageControllers[i], storageURI, cli)...)
		}
	}
	return out, batteries
}

func redfishStorageControllerToModel(storageID string, index int, sc *redfish.StorageController) models.StorageController {
	id := sc.ID
	if id == "" {
		id = sc.Name
	}
	if id == "" {
		id = fmt.Sprintf("controller-%d", index)
	}
	ctrl := models.StorageController{
		StorageID:                    storageID,
		ID:                           id,
		Name:                         sc.Name,
		Model:                        sc.Model,
		Manufacturer:                 sc.Manufacturer,
		FirmwareVersion:              sc.FirmwareVersion,
		SerialNumber:                 sc.SerialNumber,
		PartNumber:                   sc.PartNumber,
		SpeedGbps:                    sc.SpeedGbps,
		SupportedDeviceProtocols:     joinProtocols(sc.SupportedDeviceProtocols),
		SupportedControllerProtocols: joinProtocols(sc.SupportedControllerProtocols),
		SupportedRAIDTypes:           joinRAIDTypes(sc.SupportedRAIDTypes),
		TotalCacheSizeMiB:            sc.CacheSummary.TotalCacheSizeMiB,
		PersistentCacheSizeMiB:       sc.CacheSummary.PersistentCacheSizeMiB,
		AssetTag:                     sc.AssetTag,
		Identifiers:                  formatIdentifiers(sc.Identifiers),
		Health:                       statusStr(string(sc.Status.Health)),
		State:                        statusStr(string(sc.Status.State)),
		HealthRollup:                 statusStr(string(sc.Status.HealthRollup)),
	}
	if cli := sc.GetClient(); cli != nil && sc.ODataID != "" {
		if body, ok := getRedfishResourceJSON(cli, sc.ODataID); ok {
			rp := redpoolext.ParseRedpoolStringFields(body,
				"FruPartNumber", "ManufactureDate", "EngineeringChangeLevel")
			ctrl.FruPartNumber = rp["FruPartNumber"]
			ctrl.ManufactureDate = rp["ManufactureDate"]
			ctrl.EngineeringChangeLevel = rp["EngineeringChangeLevel"]
			if ctrl.PartNumber == "" && ctrl.FruPartNumber != "" {
				ctrl.PartNumber = ctrl.FruPartNumber
			}
		}
	}
	return ctrl
}

func collectStorageControllerBatteries(storageID, controllerID string, sc *redfish.StorageController, storageURI string, cli common.Client) []models.StorageBattery {
	var out []models.StorageBattery
	if sc == nil {
		return out
	}
	if cli == nil {
		cli = sc.GetClient()
	}
	batteries, err := sc.Batteries()
	if err != nil || len(batteries) == 0 {
		candidates := []string{}
		if sc.ODataID != "" {
			candidates = append(candidates, strings.TrimSuffix(sc.ODataID, "/")+"/Batteries")
		}
		if storageURI != "" && controllerID != "" {
			candidates = append(candidates, storageURI+"/Controllers/"+controllerID+"/Batteries")
		}
		for _, coll := range candidates {
			if listed := listBatteriesAt(cli, coll); len(listed) > 0 {
				batteries = listed
				err = nil
				break
			}
		}
	}
	if err != nil {
		return out
	}
	for i, b := range batteries {
		if b == nil {
			continue
		}
		id := b.ID
		if id == "" {
			id = b.Name
		}
		if id == "" {
			id = fmt.Sprintf("battery-%d", i)
		}
		m := models.StorageBattery{
			StorageID:               storageID,
			ControllerID:            controllerID,
			ID:                      id,
			Name:                    b.Name,
			Model:                   b.Model,
			Manufacturer:            b.Manufacturer,
			SerialNumber:            b.SerialNumber,
			FirmwareVersion:         b.FirmwareVersion,
			PartNumber:              b.PartNumber,
			SparePartNumber:         b.SparePartNumber,
			ChargeState:             string(b.ChargeState),
			CapacityActualWattHours: b.CapacityActualWattHours,
			CapacityRatedWattHours:  b.CapacityRatedWattHours,
			CapacityActualAmpHours:  b.CapacityActualAmpHours,
			CapacityRatedAmpHours:   b.CapacityRatedAmpHours,
			MaxChargeRateAmps:       b.MaxChargeRateAmps,
			MaxDischargeRateAmps:    b.MaxDischargeRateAmps,
			MaxChargeVoltage:        b.MaxChargeVoltage,
			StateOfHealthReading:    float64(b.StateOfHealthPercent.Reading),
			Health:                  statusStr(string(b.Status.Health)),
			State:                   statusStr(string(b.Status.State)),
			HealthRollup:            statusStr(string(b.Status.HealthRollup)),
		}
		redpoolEnrichStorageBatteryFromBatteryResource(cli, b.ODataID, &m)
		if m.ChargeLevelPercent == 0 && m.StateOfHealthReading > 0 {
			m.ChargeLevelPercent = m.StateOfHealthReading
		}
		out = append(out, m)
	}
	return out
}

func listBatteriesAt(cli common.Client, collectionURI string) []*redfish.Battery {
	if cli == nil || strings.TrimSpace(collectionURI) == "" {
		return nil
	}
	listed, err := redfish.ListReferencedBatterys(cli, collectionURI)
	if err != nil || len(listed) == 0 {
		return nil
	}
	return listed
}

// batteriesFromStorageOEMJSON synthesizes storage-controller battery metrics when the BMC
// only exposes battery status under Storage Oem and Links.Batteries is absent.
// Prefers Oem.Redpool.ControllerBattery (Redpool-normalized); also accepts Oem.*.DellControllerBattery.
func batteriesFromStorageOEMJSON(s *redfish.Storage, api *gofish.APIClient) []models.StorageBattery {
	if s == nil || s.ODataID == "" {
		return nil
	}
	cli := s.GetClient()
	if cli == nil && api != nil {
		cli = api
	}
	body, ok := getRedfishResourceJSON(cli, s.ODataID)
	if !ok {
		return nil
	}
	var doc struct {
		ID  string                     `json:"Id"`
		Oem map[string]json.RawMessage `json:"Oem"`
	}
	if err := json.Unmarshal(body, &doc); err != nil || doc.Oem == nil {
		return nil
	}
	storageID := doc.ID
	if storageID == "" {
		storageID = s.ID
	}
	parseBattery := func(batRaw json.RawMessage) *models.StorageBattery {
		var b struct {
			ID            string `json:"Id"`
			Name          string `json:"Name"`
			PrimaryStatus string `json:"PrimaryStatus"`
			RAIDState     string `json:"RAIDState"`
			FQDD          string `json:"FQDD"`
		}
		if err := json.Unmarshal(batRaw, &b); err != nil {
			return nil
		}
		if b.Name == "" && b.PrimaryStatus == "" && b.ID == "" {
			return nil
		}
		id := b.ID
		if id == "" {
			id = b.FQDD
		}
		if id == "" {
			id = "1"
		}
		health := b.PrimaryStatus
		if health == "" {
			health = "Unknown"
		}
		state := "Enabled"
		if b.RAIDState != "" {
			state = b.RAIDState
		}
		return &models.StorageBattery{
			StorageID:    storageID,
			ControllerID: storageID,
			ID:           id,
			Name:         b.Name,
			Manufacturer: "Dell",
			Health:       statusStr(health),
			State:        statusStr(state),
		}
	}
	if rpRaw, ok := doc.Oem["Redpool"]; ok {
		var rp struct {
			ControllerBattery json.RawMessage `json:"ControllerBattery"`
		}
		if err := json.Unmarshal(rpRaw, &rp); err == nil && len(rp.ControllerBattery) > 0 {
			if bat := parseBattery(rp.ControllerBattery); bat != nil {
				return []models.StorageBattery{*bat}
			}
		}
	}
	for _, raw := range doc.Oem {
		var block map[string]json.RawMessage
		if err := json.Unmarshal(raw, &block); err != nil {
			continue
		}
		batRaw, ok := block["DellControllerBattery"]
		if !ok || len(batRaw) == 0 || string(batRaw) == "null" {
			continue
		}
		if bat := parseBattery(batRaw); bat != nil {
			return []models.StorageBattery{*bat}
		}
	}
	return nil
}

// redpoolEnrichStorageBatteryFromBatteryResource reads full Battery JSON (gofish drops arbitrary Oem)
// and copies HPE Smart Storage telemetry from Oem.Redpool.HpeSmartStorage when present.
func redpoolEnrichStorageBatteryFromBatteryResource(client common.Client, uri string, m *models.StorageBattery) {
	if client == nil || uri == "" || m == nil {
		return
	}
	resp, err := client.Get(uri)
	if err != nil || resp == nil {
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return
	}
	var doc map[string]json.RawMessage
	if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil {
		return
	}
	applyRedpoolBatteryOEMFromDoc(doc, m)
}

func applyRedpoolBatteryOEMFromDoc(doc map[string]json.RawMessage, m *models.StorageBattery) {
	oemRaw, ok := doc["Oem"]
	if !ok || len(oemRaw) == 0 {
		return
	}
	var oem map[string]json.RawMessage
	if err := json.Unmarshal(oemRaw, &oem); err != nil {
		return
	}
	rpRaw, ok := oem["Redpool"]
	if !ok || len(rpRaw) == 0 {
		return
	}
	var redpoolWrap map[string]json.RawMessage
	if err := json.Unmarshal(rpRaw, &redpoolWrap); err != nil {
		return
	}
	hpeRaw, ok := redpoolWrap["HpeSmartStorage"]
	if !ok || len(hpeRaw) == 0 {
		return
	}
	var raw struct {
		ChargeLevelPercent         *float64 `json:"ChargeLevelPercent"`
		MaximumCapWatts            *float64 `json:"MaximumCapWatts"`
		RemainingChargeTimeSeconds *float64 `json:"RemainingChargeTimeSeconds"`
	}
	if err := json.Unmarshal(hpeRaw, &raw); err != nil {
		return
	}
	if raw.ChargeLevelPercent != nil {
		m.ChargeLevelPercent = *raw.ChargeLevelPercent
	}
	if raw.MaximumCapWatts != nil {
		m.MaximumCapWatts = *raw.MaximumCapWatts
	}
	if raw.RemainingChargeTimeSeconds != nil {
		m.RemainingChargeTimeSeconds = *raw.RemainingChargeTimeSeconds
	}
}

func joinProtocols(p []common.Protocol) string {
	if len(p) == 0 {
		return ""
	}
	parts := make([]string, 0, len(p))
	for _, x := range p {
		parts = append(parts, string(x))
	}
	return strings.Join(parts, ",")
}

func joinRAIDTypes(t []redfish.RAIDType) string {
	if len(t) == 0 {
		return ""
	}
	parts := make([]string, 0, len(t))
	for _, x := range t {
		parts = append(parts, string(x))
	}
	return strings.Join(parts, ",")
}

func joinEncryptionTypes(t []redfish.EncryptionTypes) string {
	if len(t) == 0 {
		return ""
	}
	parts := make([]string, 0, len(t))
	for _, x := range t {
		parts = append(parts, string(x))
	}
	return strings.Join(parts, ",")
}

// driveHotPluggableFromJSON reads HotPluggable (or HPE HotplugCapable) from raw Drive JSON
// because gofish's Drive struct does not expose HotPluggable.
func driveHotPluggableFromJSON(d *redfish.Drive) bool {
	if d == nil || d.ODataID == "" {
		return false
	}
	cli := d.GetClient()
	if cli == nil {
		return false
	}
	body, ok := getRedfishResourceJSON(cli, d.ODataID)
	if !ok {
		return false
	}
	var doc struct {
		HotPluggable   *bool `json:"HotPluggable"`
		HotplugCapable *bool `json:"HotplugCapable"`
		Oem            struct {
			Hpe struct {
				HotplugCapable *bool `json:"HotplugCapable"`
				HotPluggable   *bool `json:"HotPluggable"`
			} `json:"Hpe"`
		} `json:"Oem"`
	}
	if err := json.Unmarshal(body, &doc); err != nil {
		return false
	}
	if doc.HotPluggable != nil {
		return *doc.HotPluggable
	}
	if doc.HotplugCapable != nil {
		return *doc.HotplugCapable
	}
	if doc.Oem.Hpe.HotPluggable != nil {
		return *doc.Oem.Hpe.HotPluggable
	}
	if doc.Oem.Hpe.HotplugCapable != nil {
		return *doc.Oem.Hpe.HotplugCapable
	}
	return false
}

// driveOemTelemetryFromJSON reads temperature / uncorrected error counters from Drive Oem.
func driveOemTelemetryFromJSON(d *redfish.Drive) (temp *float64, readErrs, writeErrs *float64) {
	if d == nil || d.ODataID == "" {
		return nil, nil, nil
	}
	cli := d.GetClient()
	if cli == nil {
		return nil, nil, nil
	}
	body, ok := getRedfishResourceJSON(cli, d.ODataID)
	if !ok {
		return nil, nil, nil
	}
	var doc struct {
		Oem struct {
			Redpool struct {
				HpeSmartStorage struct {
					TemperatureCelsius     *float64 `json:"TemperatureCelsius"`
					UncorrectedReadErrors  *float64 `json:"UncorrectedReadErrors"`
					UncorrectedWriteErrors *float64 `json:"UncorrectedWriteErrors"`
				} `json:"HpeSmartStorage"`
			} `json:"Redpool"`
			Lenovo struct {
				Temperature *float64 `json:"Temperature"`
			} `json:"Lenovo"`
		} `json:"Oem"`
	}
	if err := json.Unmarshal(body, &doc); err != nil {
		return nil, nil, nil
	}
	if doc.Oem.Redpool.HpeSmartStorage.TemperatureCelsius != nil {
		temp = doc.Oem.Redpool.HpeSmartStorage.TemperatureCelsius
	} else if doc.Oem.Lenovo.Temperature != nil {
		temp = doc.Oem.Lenovo.Temperature
	}
	readErrs = doc.Oem.Redpool.HpeSmartStorage.UncorrectedReadErrors
	writeErrs = doc.Oem.Redpool.HpeSmartStorage.UncorrectedWriteErrors
	return temp, readErrs, writeErrs
}

func formatIdentifiers(ids []common.Identifier) string {
	if len(ids) == 0 {
		return ""
	}
	parts := make([]string, 0, len(ids))
	for _, id := range ids {
		if id.DurableName == "" && id.DurableNameFormat == "" {
			continue
		}
		parts = append(parts, fmt.Sprintf("%s:%s", id.DurableNameFormat, id.DurableName))
	}
	return strings.Join(parts, "|")
}

func driveLocationSummary(d *redfish.Drive) string {
	if s := locationString(d.PhysicalLocation); s != "" {
		return s
	}
	var b strings.Builder
	for _, loc := range d.Location {
		if s := locationString(loc); s != "" {
			if b.Len() > 0 {
				b.WriteString("; ")
			}
			b.WriteString(s)
		}
	}
	return b.String()
}

func locationString(loc common.Location) string {
	if loc.PartLocation.ServiceLabel != "" {
		return loc.PartLocation.ServiceLabel
	}
	if loc.Info != "" {
		return loc.Info
	}
	// Dell (and others) often only set PartLocation.LocationType + LocationOrdinalValue
	// (e.g. Slot=1) without ServiceLabel/Info.
	if lt := string(loc.PartLocation.LocationType); lt != "" {
		return fmt.Sprintf("%s=%d", lt, loc.PartLocation.LocationOrdinalValue)
	}
	if loc.PartLocation.LocationOrdinalValue != 0 {
		return fmt.Sprintf("Slot=%d", loc.PartLocation.LocationOrdinalValue)
	}
	pl := loc.Placement
	if pl.Rack != "" || pl.Row != "" {
		return strings.TrimSpace(fmt.Sprintf("%s %s offset=%d", pl.Row, pl.Rack, pl.RackOffset))
	}
	return ""
}

func formatFloat32(f float32) string {
	return strconv.FormatFloat(float64(f), 'g', -1, 32)
}

func formatFloat64(f float64) string {
	return strconv.FormatFloat(f, 'g', -1, 64)
}

// resolveDriveFormFactor prefers standard Redfish DriveFormFactor, then Dell OEM
// Oem.Dell.DellPhysicalDisk.DriveFormFactor (common on iDRAC when the standard field is absent).
func resolveDriveFormFactor(d *redfish.Drive) string {
	if d == nil {
		return ""
	}
	if s := string(d.DriveFormFactor); s != "" {
		return s
	}
	return dellOEMDriveFormFactor(d.Oem)
}

// driveRedpoolString reads a string field from Oem.Redpool (e.g. RaidStatus).
func driveRedpoolString(d *redfish.Drive, key string) string {
	if d == nil || len(d.Oem) == 0 {
		return ""
	}
	var root map[string]json.RawMessage
	if err := json.Unmarshal(d.Oem, &root); err != nil {
		return ""
	}
	rpRaw, ok := root["Redpool"]
	if !ok || len(rpRaw) == 0 {
		return ""
	}
	var rp map[string]json.RawMessage
	if err := json.Unmarshal(rpRaw, &rp); err != nil {
		return ""
	}
	valRaw, ok := rp[key]
	if !ok || len(valRaw) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(valRaw, &s); err != nil {
		return ""
	}
	return s
}

// driveRedpoolFloat reads a numeric Oem.Redpool field (e.g. AvailableSparePercent).
func driveRedpoolFloat(d *redfish.Drive, key string) *float64 {
	if d == nil || len(d.Oem) == 0 {
		return nil
	}
	var root map[string]json.RawMessage
	if err := json.Unmarshal(d.Oem, &root); err != nil {
		return nil
	}
	rpRaw, ok := root["Redpool"]
	if !ok || len(rpRaw) == 0 {
		return nil
	}
	var rp map[string]json.RawMessage
	if err := json.Unmarshal(rpRaw, &rp); err != nil {
		return nil
	}
	valRaw, ok := rp[key]
	if !ok || len(valRaw) == 0 {
		return nil
	}
	var f float64
	if err := json.Unmarshal(valRaw, &f); err != nil {
		return nil
	}
	return &f
}

func dellOEMDriveFormFactor(oem json.RawMessage) string {
	if len(oem) == 0 {
		return ""
	}
	var root map[string]json.RawMessage
	if err := json.Unmarshal(oem, &root); err != nil {
		return ""
	}
	dellRaw, ok := root["Dell"]
	if !ok || len(dellRaw) == 0 {
		return ""
	}
	var dell map[string]json.RawMessage
	if err := json.Unmarshal(dellRaw, &dell); err != nil {
		return ""
	}
	pdiskRaw, ok := dell["DellPhysicalDisk"]
	if !ok || len(pdiskRaw) == 0 {
		return ""
	}
	var pdisk struct {
		DriveFormFactor string `json:"DriveFormFactor"`
	}
	if err := json.Unmarshal(pdiskRaw, &pdisk); err != nil {
		return ""
	}
	return pdisk.DriveFormFactor
}

// driveStatusIndicatorHealthLabel returns Drive.StatusIndicator for health_info only
// when it adds information beyond Resource.Health (e.g. Rebuild vs OK).
func driveStatusIndicatorHealthLabel(health, statusIndicator string) string {
	si := strings.TrimSpace(statusIndicator)
	if si == "" {
		return ""
	}
	h := strings.TrimSpace(health)
	if h != "" && strings.EqualFold(si, h) {
		return ""
	}
	return si
}

func driveHealthInfoLabels(drive models.Drive) map[string]string {
	labels := map[string]string{
		"id":                 drive.ID,
		"storage_id":         drive.StorageID,
		"health":             drive.Health,
		"predictive_failure": drive.PredictiveFailureState,
	}
	if si := driveStatusIndicatorHealthLabel(drive.Health, drive.StatusIndicator); si != "" {
		labels["status_indicator"] = si
	}
	return labels
}

// SetStorageMetrics sets storage drive, volume, and controller gauges (used by collect.go).
func (c *Collector) SetStorageMetrics(storageInfo *models.StorageInfo) {
	if storageInfo == nil {
		return
	}
	for _, drive := range storageInfo.Drives {
		c.setGauge("redfish_storage_drive_info", map[string]string{
			"id":                    drive.ID,
			"storage_id":            drive.StorageID,
			"model":                 drive.Model,
			"device_serial_number":  drive.SerialNumber,
			"media_type":            drive.MediaType,
			"capacity_bytes":        strconv.FormatInt(drive.CapacityBytes, 10),
			"manufacturer":          drive.Manufacturer,
			"protocol":              drive.Protocol,
			"negotiated_speed_gbps": formatFloat32(drive.NegotiatedSpeedGbps),
			"capable_speed_gbps":    formatFloat32(drive.CapableSpeedGbps),
			"part_number":           drive.PartNumber,
			"revision":              drive.Revision,
			"encryption_ability":    drive.EncryptionAbility,
			"encryption_status":     drive.EncryptionStatus,
			"block_size_bytes":      strconv.Itoa(drive.BlockSizeBytes),
			"multipath":             strconv.FormatBool(drive.Multipath),
			"volume_id":             drive.VolumeIDs,
			"identifier":            drive.Identifiers,
			"location":              drive.Location,
			"drive_form_factor":     drive.DriveFormFactor,
			"hot_pluggable":         strconv.FormatBool(drive.HotPluggable),
			"cpu_affinity":          drive.CPUAffinity,
		}, 1)
		if drive.AvailableSparePercent != nil {
			c.setGauge("redfish_storage_drive_available_spare_percent", map[string]string{
				"id":         drive.ID,
				"storage_id": drive.StorageID,
			}, *drive.AvailableSparePercent)
		}
		fpVal := float64(0)
		if drive.FailurePredicted {
			fpVal = 1
		}
		c.setGauge("redfish_storage_drive_failure_predicted", map[string]string{
			"id":         drive.ID,
			"storage_id": drive.StorageID,
		}, fpVal)
		wceVal := float64(0)
		if drive.WriteCacheEnabled {
			wceVal = 1
		}
		c.setGauge("redfish_storage_drive_write_cache_enabled", map[string]string{
			"id":         drive.ID,
			"storage_id": drive.StorageID,
		}, wceVal)
		c.setGauge("redfish_storage_drive_predicted_media_life_percent", map[string]string{
			"id":         drive.ID,
			"storage_id": drive.StorageID,
		}, float64(drive.PredictedMediaLifeLeftPercent))
		c.setGauge("redfish_storage_drive_health_info", driveHealthInfoLabels(drive), 1)
		c.setGauge("redfish_storage_drive_state_info", map[string]string{
			"id":          drive.ID,
			"storage_id":  drive.StorageID,
			"state":       drive.State,
			"raid_status": drive.RaidStatus,
		}, 1)
	}

	for _, vol := range storageInfo.Volumes {
		c.setGauge("redfish_storage_volume_info", map[string]string{
			"id":                    vol.ID,
			"storage_id":            vol.StorageID,
			"name":                  vol.Name,
			"raid_type":             vol.RAIDType,
			"volume_type":           vol.VolumeType,
			"capacity_bytes":        strconv.Itoa(vol.CapacityBytes),
			"block_size_bytes":      strconv.Itoa(vol.BlockSizeBytes),
			"strip_size_bytes":      strconv.Itoa(vol.StripSizeBytes),
			"optimum_io_size_bytes": strconv.Itoa(vol.OptimumIOSizeBytes),
			"read_cache_policy":     vol.ReadCachePolicy,
			"write_cache_policy":    vol.WriteCachePolicy,
			"write_cache_state":     vol.WriteCacheState,
			"encrypted":             strconv.FormatBool(vol.Encrypted),
			"encryption_types":      vol.EncryptionTypes,
			"boot_capable":          strconv.FormatBool(vol.IsBootCapable),
			"lun":                   strconv.Itoa(vol.LogicalUnitNumber),
			"provisioning_policy":   vol.ProvisioningPolicy,
			"io_perf_mode_enabled":  strconv.FormatBool(vol.IOPerfModeEnabled),
		}, 1)
		c.setGauge("redfish_storage_volume_health_info", map[string]string{
			"id":         vol.ID,
			"storage_id": vol.StorageID,
			"health":     vol.Health,
		}, 1)
		c.setGauge("redfish_storage_volume_state_info", map[string]string{
			"id":         vol.ID,
			"storage_id": vol.StorageID,
			"state":      vol.State,
		}, 1)
	}

	for _, ctrl := range storageInfo.Controllers {
		c.setGauge("redfish_storage_controller_info", map[string]string{
			"storage_id":                     ctrl.StorageID,
			"controller_id":                  ctrl.ID,
			"name":                           ctrl.Name,
			"model":                          ctrl.Model,
			"manufacturer":                   ctrl.Manufacturer,
			"firmware_version":               ctrl.FirmwareVersion,
			"controller_serial_number":       ctrl.SerialNumber,
			"part_number":                    ctrl.PartNumber,
			"speed_gbps":                     formatFloat32(ctrl.SpeedGbps),
			"supported_device_protocols":     ctrl.SupportedDeviceProtocols,
			"supported_controller_protocols": ctrl.SupportedControllerProtocols,
			"supported_raid_types":           ctrl.SupportedRAIDTypes,
			"total_cache_size_mib":           strconv.Itoa(ctrl.TotalCacheSizeMiB),
			"persistent_cache_size_mib":      strconv.Itoa(ctrl.PersistentCacheSizeMiB),
			"asset_tag":                      ctrl.AssetTag,
			"identifier":                     ctrl.Identifiers,
			"fru_part_number":                ctrl.FruPartNumber,
			"manufacture_date":               ctrl.ManufactureDate,
			"engineering_change_level":       ctrl.EngineeringChangeLevel,
		}, 1)
		c.setGauge("redfish_storage_controller_health_info", map[string]string{
			"storage_id":    ctrl.StorageID,
			"controller_id": ctrl.ID,
			"health":        ctrl.Health,
		}, 1)
		c.setGauge("redfish_storage_controller_state_info", map[string]string{
			"storage_id":    ctrl.StorageID,
			"controller_id": ctrl.ID,
			"state":         ctrl.State,
		}, 1)
	}

	for _, battery := range storageInfo.Batteries {
		c.setGauge("redfish_storage_battery_info", map[string]string{
			"storage_id":        battery.StorageID,
			"controller_id":     battery.ControllerID,
			"battery_id":        battery.ID,
			"name":              battery.Name,
			"model":             battery.Model,
			"manufacturer":      battery.Manufacturer,
			"serial_number":     battery.SerialNumber,
			"firmware_version":  battery.FirmwareVersion,
			"part_number":       battery.PartNumber,
			"spare_part_number": battery.SparePartNumber,
			"charge_state":      battery.ChargeState,
		}, 1)
		c.setGauge("redfish_storage_battery_capacity_actual_watt_hours", map[string]string{
			"storage_id":    battery.StorageID,
			"controller_id": battery.ControllerID,
			"battery_id":    battery.ID,
		}, battery.CapacityActualWattHours)
		c.setGauge("redfish_storage_battery_capacity_rated_watt_hours", map[string]string{
			"storage_id":    battery.StorageID,
			"controller_id": battery.ControllerID,
			"battery_id":    battery.ID,
		}, battery.CapacityRatedWattHours)
		c.setGauge("redfish_storage_battery_capacity_actual_amp_hours", map[string]string{
			"storage_id":    battery.StorageID,
			"controller_id": battery.ControllerID,
			"battery_id":    battery.ID,
		}, battery.CapacityActualAmpHours)
		c.setGauge("redfish_storage_battery_capacity_rated_amp_hours", map[string]string{
			"storage_id":    battery.StorageID,
			"controller_id": battery.ControllerID,
			"battery_id":    battery.ID,
		}, battery.CapacityRatedAmpHours)
		c.setGauge("redfish_storage_battery_max_charge_rate_amps", map[string]string{
			"storage_id":    battery.StorageID,
			"controller_id": battery.ControllerID,
			"battery_id":    battery.ID,
		}, battery.MaxChargeRateAmps)
		c.setGauge("redfish_storage_battery_max_discharge_rate_amps", map[string]string{
			"storage_id":    battery.StorageID,
			"controller_id": battery.ControllerID,
			"battery_id":    battery.ID,
		}, battery.MaxDischargeRateAmps)
		c.setGauge("redfish_storage_battery_max_charge_voltage", map[string]string{
			"storage_id":    battery.StorageID,
			"controller_id": battery.ControllerID,
			"battery_id":    battery.ID,
		}, battery.MaxChargeVoltage)
		c.setGauge("redfish_storage_battery_state_of_health_percent", map[string]string{
			"storage_id":    battery.StorageID,
			"controller_id": battery.ControllerID,
			"battery_id":    battery.ID,
		}, battery.StateOfHealthReading)
		c.setGauge("redfish_storage_battery_charge_level_percent", map[string]string{
			"storage_id":    battery.StorageID,
			"controller_id": battery.ControllerID,
			"battery_id":    battery.ID,
		}, battery.ChargeLevelPercent)
		c.setGauge("redfish_storage_battery_maximum_cap_watts", map[string]string{
			"storage_id":    battery.StorageID,
			"controller_id": battery.ControllerID,
			"battery_id":    battery.ID,
		}, battery.MaximumCapWatts)
		c.setGauge("redfish_storage_battery_remaining_charge_time_seconds", map[string]string{
			"storage_id":    battery.StorageID,
			"controller_id": battery.ControllerID,
			"battery_id":    battery.ID,
		}, battery.RemainingChargeTimeSeconds)
		c.setGauge("redfish_storage_battery_health_info", map[string]string{
			"storage_id":    battery.StorageID,
			"controller_id": battery.ControllerID,
			"battery_id":    battery.ID,
			"health":        battery.Health,
		}, 1)
		c.setGauge("redfish_storage_battery_state_info", map[string]string{
			"storage_id":    battery.StorageID,
			"controller_id": battery.ControllerID,
			"battery_id":    battery.ID,
			"state":         battery.State,
		}, 1)
	}

	c.collectDriveOemTelemetry()
}

// collectDriveOemTelemetry emits drive temperature / uncorrected error gauges from Oem JSON.
func (c *Collector) collectDriveOemTelemetry() {
	if c.client == nil || c.client.Service == nil {
		return
	}
	systems, err := c.client.Service.Systems()
	if err != nil || len(systems) == 0 {
		return
	}
	storage, err := systems[0].Storage()
	if err != nil && !isPartial(err) {
		return
	}
	for _, s := range storage {
		if s == nil {
			continue
		}
		drives, err := s.Drives()
		if err != nil && !isPartial(err) {
			continue
		}
		for _, d := range drives {
			if d == nil {
				continue
			}
			temp, readErrs, writeErrs := driveOemTelemetryFromJSON(d)
			labels := map[string]string{
				"id":         d.ID,
				"storage_id": s.ID,
			}
			if temp != nil {
				c.setGauge("redfish_storage_drive_temperature_celsius", labels, *temp)
			}
			if readErrs != nil {
				c.setGauge("redfish_storage_drive_uncorrected_read_errors", labels, *readErrs)
			}
			if writeErrs != nil {
				c.setGauge("redfish_storage_drive_uncorrected_write_errors", labels, *writeErrs)
			}
		}
	}
}
