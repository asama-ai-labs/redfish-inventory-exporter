// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Asama Inc.

package redpoolext

import "testing"

func TestParseRedpoolFloatField(t *testing.T) {
	body := []byte(`{"Oem":{"Redpool":{"AverageFanPWM":15.25,"FanPWM":15}}}`)
	if v, ok := ParseRedpoolFloatField(body, "AverageFanPWM"); !ok || v != 15.25 {
		t.Fatalf("AverageFanPWM = %v %v", v, ok)
	}
	if v, ok := ParseRedpoolFloatField(body, "FanPWM"); !ok || v != 15 {
		t.Fatalf("FanPWM = %v %v", v, ok)
	}
	if _, ok := ParseRedpoolFloatField(body, "Missing"); ok {
		t.Fatal("missing key reported present")
	}
	if _, ok := ParseRedpoolFloatField([]byte(`{"Oem":{"Dell":{"AverageFanPWM":1}}}`), "AverageFanPWM"); ok {
		t.Fatal("vendor OEM reported as redpool")
	}
	for _, raw := range []string{"NaN", "Inf", "-Inf"} {
		body := []byte(`{"Oem":{"Redpool":{"FanPWM":"` + raw + `"}}}`)
		if _, ok := ParseRedpoolFloatField(body, "FanPWM"); ok {
			t.Fatalf("%s reported present", raw)
		}
	}
}
