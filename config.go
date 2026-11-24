// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Asama Inc.

package main

import (
	"os"
	"strings"

	"github.com/asama-ai-labs/redfish-inventory-exporter/internal/credentials"
	"gopkg.in/yaml.v3"
)

type Config struct {
	Hosts       map[string]HostConfig `yaml:"hosts"`
	Default     HostConfig            `yaml:"default,omitempty"`
	LogLevel    string                `yaml:"loglevel,omitempty"`
	Credentials yaml.Node             `yaml:"credentials"`
}

type HostConfig struct {
	Username string `yaml:"username"`
	Password string `yaml:"password"`
}

// HostCredentials resolves credentials for a scrape target (BMC host:port or IP).
// Order (vmagent-style YAML):
//  1. hosts[target], when that entry has a password
//  2. hosts["default"], when that entry has a password
//  3. top-level default
func (c *Config) HostCredentials(target string) HostConfig {
	if c == nil {
		return HostConfig{}
	}
	if c.Hosts != nil {
		if hc, ok := c.Hosts[target]; ok && strings.TrimSpace(hc.Password) != "" {
			return hc
		}
		if target != credentials.DefaultName {
			if hc, ok := c.Hosts[credentials.DefaultName]; ok && strings.TrimSpace(hc.Password) != "" {
				return hc
			}
		}
	}
	return c.Default
}

// HasPassword reports whether the config file holds a BMC password.
func (c *Config) HasPassword() bool {
	if c == nil {
		return false
	}
	if strings.TrimSpace(c.Default.Password) != "" {
		return true
	}
	for _, hc := range c.Hosts {
		if strings.TrimSpace(hc.Password) != "" {
			return true
		}
	}
	return false
}

func LoadConfig(filename string) (*Config, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, err
	}
	return ParseConfig(data)
}

func ParseConfig(data []byte) (*Config, error) {
	var config Config
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, err
	}
	if err := credentials.ValidateDocument(&config.Credentials); err != nil {
		return nil, err
	}
	return &config, nil
}
