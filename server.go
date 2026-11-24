// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Asama Inc.

package main

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net/http"
	"os"
	"time"
)

// bmcTLSConfig builds the TLS settings for BMC connections. Without verification
// the connection can be read or altered by anyone on the path; a CA file implies
// verification against that CA only.
func bmcTLSConfig(verify bool, caFile string) (*tls.Config, error) {
	cfg := &tls.Config{MinVersion: tls.VersionTLS12}
	if caFile != "" {
		pem, err := os.ReadFile(caFile)
		if err != nil {
			return nil, fmt.Errorf("read --tls.ca-file: %w", err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(pem) {
			return nil, fmt.Errorf("--tls.ca-file %s holds no PEM certificates", caFile)
		}
		cfg.RootCAs = pool
		return cfg, nil
	}
	cfg.InsecureSkipVerify = !verify
	return cfg, nil
}

// scrapeLimiter caps concurrent scrapes. A full limiter refuses at once instead
// of queueing, so a burst cannot pile up goroutines behind slow BMCs.
type scrapeLimiter chan struct{}

func newScrapeLimiter(n int) scrapeLimiter {
	if n <= 0 {
		return nil
	}
	return make(scrapeLimiter, n)
}

func (l scrapeLimiter) tryAcquire() bool {
	if l == nil {
		return true
	}
	select {
	case l <- struct{}{}:
		return true
	default:
		return false
	}
}

func (l scrapeLimiter) release() {
	if l != nil {
		<-l
	}
}

// newServer returns an http.Server whose timeouts outlast one scrape.
func newServer(addr string, handler http.Handler, scrapeTimeout time.Duration) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      scrapeTimeout + 15*time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    1 << 16,
	}
}
