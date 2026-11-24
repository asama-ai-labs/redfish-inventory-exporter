// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Asama Inc.

package redpoolext

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
)

// ParseRedpoolStringFields extracts named string fields from Oem.Redpool on a resource JSON body.
func ParseRedpoolStringFields(body []byte, keys ...string) map[string]string {
	out := make(map[string]string, len(keys))
	if len(body) == 0 || len(keys) == 0 {
		return out
	}
	var doc struct {
		Oem struct {
			Redpool map[string]interface{} `json:"Redpool"`
		} `json:"Oem"`
	}
	if err := json.Unmarshal(body, &doc); err != nil || doc.Oem.Redpool == nil {
		return out
	}
	for _, key := range keys {
		if v, ok := doc.Oem.Redpool[key]; ok && v != nil {
			s := strings.TrimSpace(strings.TrimSpace(fmt.Sprint(v)))
			if s != "" {
				out[key] = s
			}
		}
	}
	return out
}

// ParseRedpoolBoolField reads a boolean Oem.Redpool field (true/false/1/0).
func ParseRedpoolBoolField(body []byte, key string) (bool, bool) {
	if len(body) == 0 || key == "" {
		return false, false
	}
	var doc struct {
		Oem struct {
			Redpool map[string]interface{} `json:"Redpool"`
		} `json:"Oem"`
	}
	if err := json.Unmarshal(body, &doc); err != nil || doc.Oem.Redpool == nil {
		return false, false
	}
	v, ok := doc.Oem.Redpool[key]
	if !ok || v == nil {
		return false, false
	}
	switch t := v.(type) {
	case bool:
		return t, true
	case string:
		s := strings.TrimSpace(strings.ToLower(t))
		if s == "true" || s == "1" {
			return true, true
		}
		if s == "false" || s == "0" {
			return false, true
		}
	case float64:
		return t != 0, true
	case json.Number:
		i, err := t.Int64()
		if err == nil {
			return i != 0, true
		}
	}
	return false, false
}

// ParseRedpoolIntField reads an integer Oem.Redpool field.
func ParseRedpoolIntField(body []byte, key string) (int, bool) {
	if len(body) == 0 || key == "" {
		return 0, false
	}
	var doc struct {
		Oem struct {
			Redpool map[string]interface{} `json:"Redpool"`
		} `json:"Oem"`
	}
	if err := json.Unmarshal(body, &doc); err != nil || doc.Oem.Redpool == nil {
		return 0, false
	}
	v, ok := doc.Oem.Redpool[key]
	if !ok || v == nil {
		return 0, false
	}
	switch t := v.(type) {
	case float64:
		return int(t), true
	case int:
		return t, true
	case int64:
		return int(t), true
	case json.Number:
		i, err := t.Int64()
		if err == nil {
			return int(i), true
		}
	case string:
		i, err := strconv.Atoi(strings.TrimSpace(t))
		if err == nil {
			return i, true
		}
	}
	return 0, false
}

// ParseRedpoolFloatField reads a numeric Oem.Redpool field.
func ParseRedpoolFloatField(body []byte, key string) (float64, bool) {
	if len(body) == 0 || key == "" {
		return 0, false
	}
	var doc struct {
		Oem struct {
			Redpool map[string]interface{} `json:"Redpool"`
		} `json:"Oem"`
	}
	if err := json.Unmarshal(body, &doc); err != nil || doc.Oem.Redpool == nil {
		return 0, false
	}
	v, ok := doc.Oem.Redpool[key]
	if !ok || v == nil {
		return 0, false
	}
	switch t := v.(type) {
	case float64:
		return finiteFloat(t)
	case int:
		return finiteFloat(float64(t))
	case int64:
		return finiteFloat(float64(t))
	case json.Number:
		f, err := t.Float64()
		if err == nil {
			return finiteFloat(f)
		}
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(t), 64)
		if err == nil {
			return finiteFloat(f)
		}
	}
	return 0, false
}

func finiteFloat(f float64) (float64, bool) {
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return 0, false
	}
	return f, true
}
