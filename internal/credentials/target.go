// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Asama Inc.

package credentials

import (
	"errors"
	"net"
	"net/url"
	"regexp"
	"strings"
	"unicode"
)

const (
	ipPlaceholder       = "{{ip}}"
	hostnamePlaceholder = "{{hostname}}"
	serialPlaceholder   = "{{serialnumber}}"
)

// Name is the set of values substituted into an item template.
type Name struct {
	IP           string
	Hostname     string
	SerialNumber string
}

// FieldError means a placeholder value is missing or cannot be used in an item title.
// The message does not include the value.
type FieldError struct {
	Field   string
	Invalid bool
}

func (e *FieldError) Error() string {
	if e == nil {
		return "item template value is invalid"
	}
	switch e.Field {
	case "hostname", "serialnumber":
		if e.Invalid {
			return e.Field + " is not a valid item name"
		}
		return e.Field + " is required by the item template"
	default:
		return "item template value is invalid"
	}
}

// ErrInvalidTarget means the scrape target is not an IP address.
var ErrInvalidTarget = errors.New("target must be an IP address")

// NormalizeTargetIP strips the scheme, userinfo, brackets, path, and port.
// The result is a canonical IP. DNS names are rejected.
func NormalizeTargetIP(target string) (string, error) {
	target = strings.TrimSpace(target)
	if target == "" {
		return "", ErrInvalidTarget
	}
	host := target
	if strings.Contains(target, "://") {
		parsed, err := url.Parse(target)
		if err != nil || parsed.Host == "" {
			return "", ErrInvalidTarget
		}
		host = parsed.Host
	} else if i := strings.IndexAny(target, "/?"); i >= 0 {
		host = target[:i]
	}
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	host = strings.Trim(host, "[]")
	ip := net.ParseIP(host)
	if ip == nil {
		return "", ErrInvalidTarget
	}
	return ip.String(), nil
}

var placeholderPattern = regexp.MustCompile(`\{\{[^{}]*\}\}`)

// ValidateItemTemplate accepts literal text plus {{ip}}, {{hostname}}, and {{serialnumber}}.
func ValidateItemTemplate(tmpl string) error {
	if err := checkTemplate(tmpl); err != nil {
		return err
	}
	if _, err := substitute(tmpl, Name{IP: "0.0.0.0", Hostname: "host", SerialNumber: "serial"}, true); err != nil {
		return errors.New("1password item template is invalid")
	}
	return nil
}

// RenderItemName substitutes the placeholders that appear in tmpl.
// A placeholder that is absent does not require its value.
func RenderItemName(tmpl string, name Name) (string, error) {
	if err := checkTemplate(tmpl); err != nil {
		return "", err
	}
	return substitute(tmpl, name, true)
}

// RenderDefaultItemName replaces every placeholder with "default".
func RenderDefaultItemName(tmpl string) (string, error) {
	if err := checkTemplate(tmpl); err != nil {
		return "", err
	}
	return substitute(tmpl, Name{IP: DefaultName, Hostname: DefaultName, SerialNumber: DefaultName}, false)
}

func checkTemplate(tmpl string) error {
	if strings.TrimSpace(tmpl) == "" {
		return errors.New("1password item template is empty")
	}
	if strings.Count(tmpl, "{{") != strings.Count(tmpl, "}}") {
		return errors.New("1password item template has an invalid placeholder")
	}
	for _, match := range placeholderPattern.FindAllString(tmpl, -1) {
		switch match {
		case ipPlaceholder, hostnamePlaceholder, serialPlaceholder:
		default:
			return errors.New("1password item template has an unsupported placeholder")
		}
	}
	stripped := tmpl
	for _, token := range []string{ipPlaceholder, hostnamePlaceholder, serialPlaceholder} {
		stripped = strings.ReplaceAll(stripped, token, "")
	}
	if strings.ContainsAny(stripped, "{}") {
		return errors.New("1password item template has an invalid placeholder")
	}
	return nil
}

func substitute(tmpl string, name Name, strictIP bool) (string, error) {
	values := map[string]string{
		ipPlaceholder:       name.IP,
		hostnamePlaceholder: name.Hostname,
		serialPlaceholder:   name.SerialNumber,
	}
	rendered := tmpl
	for _, token := range []string{serialPlaceholder, hostnamePlaceholder, ipPlaceholder} {
		if !strings.Contains(tmpl, token) {
			continue
		}
		value := strings.TrimSpace(values[token])
		if token == ipPlaceholder && strictIP && net.ParseIP(value) == nil {
			return "", ErrInvalidTarget
		}
		if value == "" {
			return "", &FieldError{Field: placeholderField(token)}
		}
		if err := ValidateSegment(value); err != nil {
			return "", &FieldError{Field: placeholderField(token), Invalid: true}
		}
		rendered = strings.ReplaceAll(rendered, token, value)
	}
	rendered = strings.TrimSpace(rendered)
	if err := ValidateSegment(rendered); err != nil {
		return "", errors.New("1password rendered item name is invalid")
	}
	return rendered, nil
}

func placeholderField(token string) string {
	switch token {
	case hostnamePlaceholder:
		return "hostname"
	case serialPlaceholder:
		return "serialnumber"
	default:
		return "ip"
	}
}

// ValidateItemTitle accepts a 1Password item title, including spaces.
// Slashes and other reference separators are still rejected.
func ValidateItemTitle(value string) error {
	if strings.TrimSpace(value) == "" || strings.TrimSpace(value) != value {
		return errors.New("invalid item title")
	}
	if strings.ContainsAny(value, "/\\\t\r\n?#{}") {
		return errors.New("invalid item title")
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return errors.New("invalid item title")
		}
	}
	return nil
}

// ValidateSegment rejects an empty or unsafe secret-reference path segment.
func ValidateSegment(value string) error {
	if value == "" || strings.TrimSpace(value) != value {
		return errors.New("invalid secret reference segment")
	}
	if strings.ContainsAny(value, "/\\ \t\r\n?#{}") {
		return errors.New("invalid secret reference segment")
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return errors.New("invalid secret reference segment")
		}
	}
	return nil
}
