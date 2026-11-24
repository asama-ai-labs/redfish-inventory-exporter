// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Asama Inc.

package main

import (
	"fmt"
	"net/url"
	"strings"
)

const redfishQueryHelp = "expected GET /redfish?target=<bmc>&hostname=<host>&serialnumber=<sn>"

// ValidateRedfishQuery ensures required /redfish query parameters are present.
func ValidateRedfishQuery(q url.Values) (target, hostname, serialnumber string, err error) {
	target = strings.TrimSpace(q.Get("target"))
	if target == "" {
		return "", "", "", fmt.Errorf("target parameter is missing; %s", redfishQueryHelp)
	}
	hostname = strings.TrimSpace(q.Get("hostname"))
	if hostname == "" {
		return "", "", "", fmt.Errorf("hostname parameter is missing; %s", redfishQueryHelp)
	}
	serialnumber = strings.TrimSpace(q.Get("serialnumber"))
	if serialnumber == "" {
		return "", "", "", fmt.Errorf("serialnumber parameter is missing; %s", redfishQueryHelp)
	}
	return target, hostname, serialnumber, nil
}

// redfishQueryErrorMessage returns a short message for HTTP 400 responses.
func redfishQueryErrorMessage(err error) string {
	if err == nil {
		return ""
	}
	return fmt.Sprintf("%v", err)
}
