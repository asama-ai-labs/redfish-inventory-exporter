// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Asama Inc.

package collectors

import (
	"encoding/json"
	"testing"

	"github.com/stmcginnis/gofish/redfish"
)

func TestPortDocumentFields(t *testing.T) {
	body := []byte(`{
		"@odata.type": "#Port.v1_20_0.Port",
		"Id": "NIC.Integrated.1-1",
		"PortId": "1",
		"LinkNetworkTechnology": "Ethernet",
		"CurrentSpeedGbps": 10,
		"LinkStatus": "LinkUp",
		"Ethernet": {"AssociatedMACAddresses": ["00:1B:44:AA:BB:CC"]}
	}`)
	var port redfish.Port
	if err := json.Unmarshal(body, &port); err != nil {
		t.Fatal(err)
	}
	info := networkPortInfoFromPort("System.Embedded.1", "NIC.Integrated.1", &port)
	if info.ID != "NIC.Integrated.1-1" || info.AdapterID != "NIC.Integrated.1" || info.ChassisID != "System.Embedded.1" {
		t.Fatalf("identity: %+v", info)
	}
	if info.PhysicalPortNumber != "1" {
		t.Fatalf("port id %q", info.PhysicalPortNumber)
	}
	if info.ActiveLinkTechnology != "Ethernet" {
		t.Fatalf("technology %q", info.ActiveLinkTechnology)
	}
	if info.MACAddress != "00:1B:44:AA:BB:CC" {
		t.Fatalf("mac %q", info.MACAddress)
	}
	if info.CurrentLinkSpeedMbps != 10000 {
		t.Fatalf("speed %d", info.CurrentLinkSpeedMbps)
	}
	if info.LinkStatus != "LinkUp" {
		t.Fatalf("link status %q", info.LinkStatus)
	}
}

func TestStandardLinkStatus(t *testing.T) {
	cases := map[string]string{
		"Up":       "LinkUp",
		"Down":     "LinkDown",
		"LinkUp":   "LinkUp",
		"LinkDown": "LinkDown",
		"NoLink":   "NoLink",
		"Starting": "Starting",
		"Training": "Training",
		"Weird":    "",
		"":         "",
	}
	for in, want := range cases {
		if got := standardLinkStatus(in); got != want {
			t.Fatalf("%q -> %q, want %q", in, got, want)
		}
	}
}
