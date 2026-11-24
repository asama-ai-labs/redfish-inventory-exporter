// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Asama Inc.

package models

import "github.com/asama-ai-labs/redfish-inventory-exporter/redpoolext"

// SystemInfo holds system identification and power state.
type SystemInfo struct {
	Model        string
	Manufacturer string
	SerialNumber string
	HostName     string
	PowerState   string
	UUID         string
	SubModel     string
	Health       string
	HealthRollup string
	State        string
	// OEMRollups are Oem.Redpool *RollupStatus strings (e.g. Dell SELRollupStatus).
	OEMRollups map[string]string
	// EstimatedAirflowCFM / EstimatedExhaustTempC from Oem.Redpool when present.
	EstimatedAirflowCFM   *float64
	EstimatedExhaustTempC *float64
	// HPE Oem.Redpool promotions (PCAPartNumber, PostState, FanRedundancy, AggregateServerHealth).
	PCAPartNumber         string
	PostState             string
	FanRedundancy         string
	AggregateServerHealth string
}

// ProcessorInfo holds per-processor inventory and status.
type ProcessorInfo struct {
	ID                         string
	Name                       string
	Socket                     string
	ProcessorType              string
	ProcessorArchitecture      string
	InstructionSet             string
	Manufacturer               string
	Model                      string
	PartNumber                 string
	ProcessorSerialNumber      string
	BaseSpeedMHz               int
	MaxSpeedMHz                int
	TDPWatts                   *int // nil when the BMC does not report a TDP
	ProcessorMemory            string
	TotalCores                 int
	TotalThreads               int
	TotalEnabledCores          int
	ProcessorIDVendorId        string
	ProcessorIDEffectiveFamily string
	ProcessorIDEffectiveModel  string
	ProcessorIDStep            string
	Health                     string
	State                      string
	HealthRollup               string
	PCIeDeviceID               string
}

// MemoryInfo holds system memory summary and device list.
type MemoryInfo struct {
	TotalSystemMemoryGiB float32
	MemoryDevices        []MemoryDevice
}

// MemoryDevice holds per-DIMM inventory and status.
type MemoryDevice struct {
	ID                string
	CapacityMiB       int
	OperatingSpeedMHz int
	MaxRatedSpeedMHz  int
	Manufacturer      string
	PartNumber        string
	SerialNumber      string
	MemoryType        string
	MemoryDeviceType  string
	ErrorCorrection   string
	Health            string
	State             string
	HealthRollup      string
	DeviceLocator     string
	BaseModuleType    string
	RankCount         int
	BusWidthBits      int
	DataWidthBits     int
	// LocSocket / LocMemoryController / LocChannel / LocSlot are nil when the BMC
	// omitted the corresponding JSON property (or omitted MemoryLocation); use
	// non-nil pointers for explicit values including numeric zero.
	LocSocket           *int
	LocMemoryController *int
	LocChannel          *int
	LocSlot             *int
	MemoryMedia         string
	Enabled             string // "true", "false", or "" if unknown / omitted by BMC.
	Redpool             *redpoolext.RedpoolMemoryExt
}

// StorageInfo holds storage subsystem inventory: drives, volumes, and controllers.
type StorageInfo struct {
	Drives      []Drive
	Volumes     []StorageVolume
	Controllers []StorageController
	Batteries   []StorageBattery
}

// Drive holds per-drive inventory and status.
type Drive struct {
	ID            string
	StorageID     string
	Model         string
	SerialNumber  string
	CapacityBytes int64
	MediaType     string
	Health        string
	State         string
	HealthRollup  string

	Manufacturer                  string
	Protocol                      string
	NegotiatedSpeedGbps           float32
	CapableSpeedGbps              float32
	PartNumber                    string
	Revision                      string
	EncryptionAbility             string
	EncryptionStatus              string
	FailurePredicted              bool
	PredictedMediaLifeLeftPercent float32
	BlockSizeBytes                int
	WriteCacheEnabled             bool
	HotPluggable                  bool
	StatusIndicator               string
	Multipath                     bool
	VolumeIDs                     string
	Identifiers                   string
	Location                      string
	DriveFormFactor               string
	// RaidStatus / PredictiveFailureState from Oem.Redpool (DellPhysicalDisk OEM).
	RaidStatus             string
	PredictiveFailureState string
	// CPUAffinity is Oem.Redpool.CPUAffinity (Dell Links.Oem.Dell.CPUAffinity processor ids).
	CPUAffinity string
	// AvailableSparePercent from Oem.Redpool when present (SSD spare capacity).
	AvailableSparePercent *float64
}

// StorageVolume is a logical volume / virtual disk under a Storage resource.
type StorageVolume struct {
	ID                 string
	StorageID          string
	Name               string
	RAIDType           string
	VolumeType         string
	CapacityBytes      int
	BlockSizeBytes     int
	StripSizeBytes     int
	OptimumIOSizeBytes int
	ReadCachePolicy    string
	WriteCachePolicy   string
	WriteCacheState    string
	Encrypted          bool
	EncryptionTypes    string
	IsBootCapable      bool
	LogicalUnitNumber  int
	ProvisioningPolicy string
	IOPerfModeEnabled  bool
	Health             string
	State              string
	HealthRollup       string
}

// StorageController is a RAID/HBA/NVMe controller (embedded or linked collection member).
type StorageController struct {
	StorageID                    string
	ID                           string
	Name                         string
	Model                        string
	Manufacturer                 string
	FirmwareVersion              string
	SerialNumber                 string
	PartNumber                   string
	FruPartNumber                string
	ManufactureDate              string
	EngineeringChangeLevel       string
	SpeedGbps                    float32
	SupportedDeviceProtocols     string
	SupportedControllerProtocols string
	SupportedRAIDTypes           string
	TotalCacheSizeMiB            int
	PersistentCacheSizeMiB       int
	AssetTag                     string
	Identifiers                  string
	Health                       string
	State                        string
	HealthRollup                 string
}

// StorageBattery is a Battery linked from a StorageController.
type StorageBattery struct {
	StorageID               string
	ControllerID            string
	ID                      string
	Name                    string
	Model                   string
	Manufacturer            string
	SerialNumber            string
	FirmwareVersion         string
	PartNumber              string
	SparePartNumber         string
	ChargeState             string
	CapacityActualWattHours float64
	CapacityRatedWattHours  float64
	CapacityActualAmpHours  float64
	CapacityRatedAmpHours   float64
	MaxChargeRateAmps       float64
	MaxDischargeRateAmps    float64
	MaxChargeVoltage        float64
	// StateOfHealthReading is Battery.StateOfHealthPercent.Reading when the BMC provides it.
	StateOfHealthReading float64
	// ChargeLevelPercent / MaximumCapWatts / RemainingChargeTimeSeconds are filled from
	// Oem.Redpool.HpeSmartStorage.Raw when present (Redpool normalization for OEM telemetry).
	ChargeLevelPercent         float64
	MaximumCapWatts            float64
	RemainingChargeTimeSeconds float64
	Health                     string
	State                      string
	HealthRollup               string
}

// NetworkInfo holds BMC/server addresses and interface list.
type NetworkInfo struct {
	BMCAddress    IPAddresses
	ServerAddress IPAddresses
	Interfaces    []NetworkInterface
}

// IPAddresses holds IPv4/IPv6 slices.
type IPAddresses struct {
	IPv4 []string
	IPv6 []string
}

// NetworkInterface holds per-interface inventory and status.
type NetworkInterface struct {
	ID               string
	Name             string
	Description      string
	MacAddress       string
	SpeedMbps        int
	MTUSize          int
	AutoNeg          bool
	FullDuplex       bool
	InterfaceEnabled bool
	LinkStatus       string
	Status           string
	Health           string
	State            string
	HealthRollup     string
	PermanentMACAddr string
	HostName         string
	FQDN             string
}

// NetworkAdapterPortInventory holds Chassis NetworkAdapter resources and their Ports (the non-deprecated port collection).
type NetworkAdapterPortInventory struct {
	Adapters []ChassisNetworkAdapterInfo
	Ports    []ChassisNetworkPortInfo
}

// ChassisNetworkAdapterInfo is inventory for one NetworkAdapter under a Chassis.
type ChassisNetworkAdapterInfo struct {
	ChassisID              string
	ID                     string
	Name                   string
	Manufacturer           string
	Model                  string
	SerialNumber           string
	PartNumber             string
	SKU                    string
	FirmwareVersion        string
	UUID                   string
	FruPartNumber          string
	ManufactureDate        string
	EngineeringChangeLevel string
	Health                 string
	State                  string
	HealthRollup           string
}

// ChassisNetworkPortInfo is one physical Port under a NetworkAdapter.
type ChassisNetworkPortInfo struct {
	ChassisID            string
	AdapterID            string
	ID                   string
	Name                 string
	PhysicalPortNumber   string
	LinkStatus           string
	CurrentLinkSpeedMbps int
	ActiveLinkTechnology string
	MACAddress           string
	Health               string
	State                string
}

// PCIeInfo holds PCIe device list.
type PCIeInfo struct {
	Devices []PCIeDevice
}

// PCIeDevice holds per-device PCIe inventory and status.
type PCIeDevice struct {
	ID                string
	DeviceID          int
	SubsystemDeviceID int
	SubsystemVendorID int
	VendorID          int
	ClassCode         string
	DeviceClass       string
	Name              string
	FunctionCount     int
	FirmwareVersion   string
	Manufacturer      string
	Model             string
	StatusHealth      string
	StatusState       string
	Location          string
	// PCIeInterface negotiated/max generation and lane width (Redfish PCIeDevice.PCIeInterface).
	PCIeType    string
	MaxPCIeType string
	LanesInUse  int
	MaxLanes    int
}

// NetworkPort holds port number, MAC, link status.
type NetworkPort struct {
	PortNumber string
	MACAddress string
	LinkStatus string
}

// BMCManagerInfo holds manager inventory and status.
type BMCManagerInfo struct {
	ID               string
	Name             string
	Model            string
	Manufacturer     string
	FirmwareVersion  string
	UUID             string
	BMCManagerState  string
	BMCManagerHealth string
}

// ProcessorSummary holds system-level processor summary.
type ProcessorSummary struct {
	Count                 int
	LogicalProcessorCount int
	TotalCores            int
	TotalThreads          int
	Model                 string
	Architecture          string
	MaxSpeedMHz           int
	ProcessorType         string
	Health                string
	State                 string
	HealthRollup          string
}

// MemorySummary holds system-level memory summary.
type MemorySummary struct {
	TotalSystemMemoryGiB float32
	TotalSystemMemoryMiB int
	MemoryDeviceCount    int
	MemoryMirroring      string
	OperatingSpeedMHz    int
	MaxOperatingSpeedMHz int
	MemoryType           string
	ErrorCorrection      string
	Health               string
	State                string
	HealthRollup         string
}

// ChassisInfo holds per-chassis inventory and status.
type ChassisInfo struct {
	ID                     string
	Model                  string
	SKU                    string
	Manufacturer           string
	SerialNumber           string
	AssetTag               string
	PartNumber             string
	FruPartNumber          string
	EngineeringChangeLevel string
	ManufactureDate        string
	MultiNodeSystemPart    string
	MultiNodeSystemSerial  string
	MultiNodeChassisPart   string
	MultiNodeSelfID        string
	MultiNodeTwinType      string
	MultiNodeNodes         []MultiNodeInfo
	ChassisType            string
	Description            string
	IndicatorLED           string
	PowerState             string
	Status                 Status
	ContainedBy            string
	Contains               string
	// IntrusionSensor is Chassis.PhysicalSecurity.IntrusionSensor when the BMC exposes it.
	IntrusionSensor string
}

// MultiNodeInfo is one blade/node under a multi-node chassis (Oem.Redpool.MultiNodeNodes).
type MultiNodeInfo struct {
	ID                string
	PowerStatus       string
	Health            string
	State             string
	BoardPartNumber   string
	BoardSerialNumber string
	BIOSVersion       string
	CPLDVersion       string
	BMCVersion        string
	DCOutputPower     *float64
	OutputCurrent     *float64
	Temperatures      []MultiNodeTemperature
}

// MultiNodeTemperature is a named temp reading under a multi-node blade.
type MultiNodeTemperature struct {
	Name           string
	ReadingCelsius float64
}

// Status holds Health, State, HealthRollup.
type Status struct {
	State        string
	Health       string
	HealthRollup string
}

// ChassisResponse holds chassis list.
type ChassisResponse struct {
	Chassis []ChassisInfo
}

// PSUInfo holds power supply unit inventory and utilisation.
type PSUInfo struct {
	ChassisID         string
	ID                string
	Name              string
	MemberID          string
	Manufacturer      string
	Model             string
	PartNumber        string
	SerialNumber      string
	PowerSupplyType   string
	PowerCapacityW    float32
	FirmwareVersion   string
	HotPluggable      bool
	IndicatorLED      string
	Health            string
	State             string
	HealthRollup      string
	LineInputVoltage  float32
	PowerInputWatts   float32
	PowerOutputWatts  float32
	EfficiencyPercent float32
}

// BiosAttributes holds attribute map.
type BiosAttributes struct {
	Attributes map[string]BiosAttribute
}

// BiosAttribute holds name/value.
type BiosAttribute struct {
	Name  string
	Value string
}
