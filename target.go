// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Asama Inc.

package main

import (
	"fmt"
	"net"
	"net/netip"
	"strings"
)

// splitTarget returns the host of a ?target= value. A target is host or host:port
// (an IP, a DNS name or a bracketed IPv6 address) and nothing else: a path, query,
// userinfo or space would change where the exporter connects.
func splitTarget(target string) (string, error) {
	if target == "" || strings.ContainsAny(target, "/?#@\\ \t\r\n") {
		return "", fmt.Errorf("target must be host or host:port")
	}
	if host, port, err := net.SplitHostPort(target); err == nil {
		if host == "" || port == "" {
			return "", fmt.Errorf("target must be host or host:port")
		}
		return host, nil
	}
	if strings.HasPrefix(target, "[") && strings.HasSuffix(target, "]") {
		return target[1 : len(target)-1], nil
	}
	if strings.Contains(target, ":") {
		if _, err := netip.ParseAddr(target); err != nil {
			return "", fmt.Errorf("target must be host or host:port")
		}
	}
	return target, nil
}

// targetPolicy is an optional allowlist of BMC hosts. An empty policy allows every
// host. Entries are IPs, CIDR ranges or host names; a host name is never resolved,
// so a name cannot be matched against a range.
type targetPolicy struct {
	prefixes []netip.Prefix
	names    map[string]bool
}

func parseTargetPolicy(csv string) (*targetPolicy, error) {
	p := &targetPolicy{names: map[string]bool{}}
	for _, entry := range strings.Split(csv, ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		if prefix, err := netip.ParsePrefix(entry); err == nil {
			p.prefixes = append(p.prefixes, prefix.Masked())
			continue
		}
		if addr, err := netip.ParseAddr(entry); err == nil {
			p.prefixes = append(p.prefixes, netip.PrefixFrom(addr, addr.BitLen()))
			continue
		}
		if strings.ContainsAny(entry, "*?[/") {
			return nil, fmt.Errorf("target allowlist entry %q: use an IP, a CIDR range or an exact host name", entry)
		}
		p.names[strings.ToLower(entry)] = true
	}
	return p, nil
}

func (p *targetPolicy) empty() bool {
	return p == nil || (len(p.prefixes) == 0 && len(p.names) == 0)
}

func (p *targetPolicy) allows(host string) bool {
	if p.empty() {
		return true
	}
	if addr, err := netip.ParseAddr(host); err == nil {
		addr = addr.Unmap()
		for _, prefix := range p.prefixes {
			if prefix.Contains(addr) {
				return true
			}
		}
		return false
	}
	return p.names[strings.ToLower(host)]
}
