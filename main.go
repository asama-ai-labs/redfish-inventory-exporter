// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Asama Inc.

// File: main.go
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/VictoriaMetrics/metrics"
	alog "github.com/apex/log"

	"github.com/asama-ai-labs/redfish-inventory-exporter/collectors"
	"github.com/asama-ai-labs/redfish-inventory-exporter/internal/credentials"
	_ "github.com/asama-ai-labs/redfish-inventory-exporter/internal/credentials/config"
	"github.com/asama-ai-labs/redfish-inventory-exporter/internal/credentials/hashicorp"
	"github.com/asama-ai-labs/redfish-inventory-exporter/internal/credentials/onepassword"
	"github.com/asama-ai-labs/redfish-inventory-exporter/otlplogexport"
	"github.com/asama-ai-labs/redfish-inventory-exporter/redfishlogs"
	"github.com/asama-ai-labs/redfish-inventory-exporter/redfishsession"
	"github.com/asama-ai/redfish_exporter/vault"

	"github.com/stmcginnis/gofish"
	kingpin "gopkg.in/alecthomas/kingpin.v2"
)

// proxyForRequest returns a Proxy function that uses HTTP_PROXY/HTTPS_PROXY for all
// requests when set, including localhost/loopback. Go's ProxyFromEnvironment skips
// the proxy for loopback, so with target 127.0.0.1 no collector traffic would go
// through redpool; this allows the golden script (and any proxy-in-the-middle case)
// to send all traffic through the proxy.
func proxyForRequest() func(*http.Request) (*url.URL, error) {
	httpProxy := strings.TrimSpace(os.Getenv("HTTP_PROXY"))
	if httpProxy == "" {
		httpProxy = strings.TrimSpace(os.Getenv("http_proxy"))
	}
	httpsProxy := strings.TrimSpace(os.Getenv("HTTPS_PROXY"))
	if httpsProxy == "" {
		httpsProxy = strings.TrimSpace(os.Getenv("https_proxy"))
	}
	if httpProxy == "" && httpsProxy == "" {
		return http.ProxyFromEnvironment
	}
	var httpURL, httpsURL *url.URL
	if httpProxy != "" {
		httpURL, _ = url.Parse(httpProxy)
	}
	if httpsProxy != "" {
		httpsURL, _ = url.Parse(httpsProxy)
	}
	if httpsURL == nil {
		httpsURL = httpURL
	}
	if httpURL == nil {
		httpURL = httpsURL
	}
	return func(req *http.Request) (*url.URL, error) {
		if req == nil || req.URL == nil {
			return nil, nil
		}
		if req.URL.Scheme == "https" && httpsURL != nil {
			return httpsURL, nil
		}
		if req.URL.Scheme == "http" && httpURL != nil {
			return httpURL, nil
		}
		return nil, nil
	}
}

// parseDisabledCollectors returns a set of collector names from a comma-separated string.
// Valid names: bios, bmc, chassis, firmware_inventory, memory, network, pcie, power, processor, sensors, storage, thermal.
func parseDisabledCollectors(csv string) map[string]bool {
	set := make(map[string]bool)
	if csv == "" {
		return set
	}
	for _, s := range strings.Split(csv, ",") {
		name := strings.TrimSpace(strings.ToLower(s))
		if name != "" {
			set[name] = true
		}
	}
	return set
}

func flushResponse(w http.ResponseWriter) {
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
}

type redfishLogLogger struct{}

func (redfishLogLogger) Warn(msg string, fields map[string]any) {
	f := alog.Fields{}
	for k, v := range fields {
		f[k] = v
	}
	alog.WithFields(f).Warn(msg)
}

func (redfishLogLogger) Info(msg string, fields map[string]any) {
	f := alog.Fields{}
	for k, v := range fields {
		f[k] = v
	}
	alog.WithFields(f).Info(msg)
}

func main() {
	configFile := kingpin.Flag("config", "Path to credentials YAML (vmagent-style: hosts.<target>.username/password, hosts.default fallback, optional top-level default).").Default("redfish.yml").String()
	listenPort := kingpin.Flag("port", "HTTP listen port for the /redfish metrics endpoint on 127.0.0.1. Ignored when --web.listen-address is set.").Default("9611").Short('p').Int()
	listenAddress := kingpin.Flag("web.listen-address", "Address to listen on (host:port). Default is 127.0.0.1 and --port. Set 0.0.0.0:9611 to listen on every interface.").String()
	scrapeTimeout := kingpin.Flag("scrape-timeout", "Server-side budget for one /redfish scrape: the credential lookup is cancelled after it and the HTTP write timeout is derived from it. Set it just below the scraper's own timeout.").Default("60s").Duration()
	bmcTimeout := kingpin.Flag("bmc-request-timeout", "Timeout for each HTTP request to a BMC, including the login.").Default("30s").Duration()
	maxConcurrentScrapes := kingpin.Flag("max-concurrent-scrapes", "Max /redfish scrapes at once across all targets; more get HTTP 503. 0 means no limit.").Default("32").Int()
	targetAllowlist := kingpin.Flag("target-allowlist", "Comma-separated BMC hosts a scrape may target: IPs, CIDR ranges or exact host names. Required when credentials are configured.").String()
	tlsVerify := kingpin.Flag("tls.verify", "Verify BMC TLS certificates against the system roots. BMCs with self-signed certificates need --tls.ca-file instead.").Bool()
	tlsCAFile := kingpin.Flag("tls.ca-file", "PEM file of the CA that signed the BMC certificates. Implies certificate verification against this CA only.").String()
	disableCollectors := kingpin.Flag("disable-collectors", "Comma-separated list of collectors to disable (bios, bmc, chassis, firmware_inventory, memory, network, pcie, power, processor, sensors, storage, thermal)").String()
	exposePrometheusMetadata := kingpin.Flag("expose-prometheus-metadata", "Emit # HELP and # TYPE lines on /redfish; health/state-style metrics include documented Redfish enum values in HELP.").Bool()

	otelLogProtocol := kingpin.Flag("otel-log-protocol", "OTLP logs transport: http or grpc.").Default("http").Enum("http", "grpc")
	otelLogEndpoint := kingpin.Flag("otel-log-endpoint", "OTLP logs endpoint (HTTP URL e.g. http://host:4318/v1/logs, or gRPC host:port). Empty uses defaults for the protocol.").String()
	otelLogTimeout := kingpin.Flag("otel-log-timeout", "Timeout for OTLP export requests.").Default("30s").Duration()
	otelLogInsecure := kingpin.Flag("otel-log-insecure", "Use gRPC plaintext (insecure credentials). Has no effect on HTTP OTLP.").Bool()
	otelLogInsecureSkipVerify := kingpin.Flag("otel-log-insecure-skip-verify", "Skip TLS certificate verification for HTTPS OTLP or TLS gRPC.").Bool()
	otelLogCheckpointFile := kingpin.Flag("otel-log-checkpoint-file", "JSON file for Redfish log checkpoints.").Default("redfish-log-checkpoints.json").String()
	otelLogInitialLastN := kingpin.Flag("otel-log-initial-last-n", "When no checkpoint, ingest only the newest N log entries per log service.").Default("500").Int()
	otelLogResetClockSkew := kingpin.Flag("otel-log-reset-clock-skew", "Clock skew window when detecting log clears for checkpoint reset.").Default("5m").Duration()
	redfishMaxConcurrent := kingpin.Flag("redfish-max-concurrent-requests", "Max concurrent HTTP requests per gofish client (session).").Default("2").Int64()
	enableOtelLogs := kingpin.Flag("enable-otel-logs", "Collect Redfish log entries after each scrape and send them to an OTLP endpoint. Off by default; this also writes the checkpoint file.").Bool()

	credentialFlags := credentials.AddFlags(kingpin.CommandLine)
	vault.AddFlags(kingpin.CommandLine)
	hashicorp.AddFlags(kingpin.CommandLine)
	if err := hashicorp.RejectTokenFlag(os.Args[1:]); err != nil {
		log.Fatal(err)
	}
	kingpin.HelpFlag.Short('h')
	kingpin.Parse()
	if *exposePrometheusMetadata {
		metrics.ExposeMetadata(true)
	}
	if *scrapeTimeout <= 0 || *bmcTimeout <= 0 {
		log.Fatalf("--scrape-timeout and --bmc-request-timeout must be positive")
	}
	policy, err := parseTargetPolicy(*targetAllowlist)
	if err != nil {
		log.Fatalf("--target-allowlist: %v", err)
	}
	bmcTLS, err := bmcTLSConfig(*tlsVerify, *tlsCAFile)
	if err != nil {
		log.Fatalf("%v", err)
	}
	if bmcTLS.InsecureSkipVerify {
		alog.Warn("BMC TLS certificates are not verified; set --tls.verify or --tls.ca-file")
	}
	limiter := newScrapeLimiter(*maxConcurrentScrapes)
	cfg, err := LoadConfig(*configFile)
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}
	selection, err := credentials.Resolve(&cfg.Credentials, credentialFlags)
	if err != nil {
		log.Fatalf("Failed to load credentials: %v", err)
	}
	providerName, err := selection.Active(credentials.External{Modes: map[string]bool{
		hashicorp.Name: vault.IsVaultEnabled(),
	}})
	if err != nil {
		log.Fatalf("Failed to load credentials: %v", err)
	}
	alog.WithField("provider", providerName).Info("credential provider selected")
	if policy.empty() && credentialsConfigured(providerName, cfg) {
		log.Fatal("credentials are configured but --target-allowlist is empty; set an allowlist or the exporter can be pointed at any host")
	}
	if policy.empty() {
		alog.Warn("no --target-allowlist: any caller of /redfish can point this exporter at any host")
	}

	credProvider, err := credentials.Open(context.Background(), providerName, selection, credentials.Deps{
		Lookup: func(target string) credentials.Login {
			host := cfg.HostCredentials(target)
			return credentials.Login{Username: host.Username, Password: host.Password}
		},
	})
	if err != nil {
		log.Fatalf("Failed to load credentials: %v", err)
	}
	if closer, ok := credProvider.(interface{ Close() error }); ok {
		defer func() {
			if err := closer.Close(); err != nil {
				alog.Fatal(err.Error())
			}
		}()
	}

	var logStore *redfishlogs.FileStore
	if *enableOtelLogs {
		var err error
		logStore, err = redfishlogs.NewFileStore(*otelLogCheckpointFile)
		if err != nil {
			log.Fatalf("otel log checkpoints: %v", err)
		}
	}

	sessionMgr := redfishsession.NewManager()

	ep := strings.TrimSpace(*otelLogEndpoint)
	if ep == "" {
		ep = otlplogexport.DefaultEndpoint(strings.ToLower(*otelLogProtocol))
	}
	if *otelLogProtocol == "grpc" {
		ep = otlplogexport.NormalizeGRPCEndpoint(ep)
	}

	otelExportCfg := otlplogexport.Config{
		Protocol:           *otelLogProtocol,
		Endpoint:           ep,
		Timeout:            *otelLogTimeout,
		Insecure:           *otelLogInsecure,
		InsecureSkipVerify: *otelLogInsecureSkipVerify,
	}

	var otelExporter *otlplogexport.Exporter
	if *enableOtelLogs {
		var err error
		otelExporter, err = otlplogexport.NewExporter(otelExportCfg)
		if err != nil {
			log.Fatalf("otel log exporter: %v", err)
		}
		defer func() {
			if err := otelExporter.Close(); err != nil {
				alog.WithError(err).Warn("otel log exporter close")
			}
		}()
	}

	logCollectCfg := redfishlogs.CollectConfig{
		InitialLastN:      *otelLogInitialLastN,
		LogResetClockSkew: *otelLogResetClockSkew,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/redfish", func(w http.ResponseWriter, r *http.Request) {
		target, hostName, serialNumber, vErr := ValidateRedfishQuery(r.URL.Query())
		if vErr != nil {
			http.Error(w, redfishQueryErrorMessage(vErr), http.StatusBadRequest)
			return
		}
		host, tErr := splitTarget(target)
		if tErr != nil {
			http.Error(w, tErr.Error(), http.StatusBadRequest)
			return
		}
		if !policy.allows(host) {
			alog.WithField("target", host).Warn("refused target outside --target-allowlist")
			http.Error(w, "target is not on the allowlist", http.StatusForbidden)
			return
		}
		if !limiter.tryAcquire() {
			w.Header().Set("Retry-After", "5")
			http.Error(w, "too many concurrent scrapes", http.StatusServiceUnavailable)
			return
		}
		defer limiter.release()

		ctx, cancel := context.WithTimeout(r.Context(), *scrapeTimeout)
		defer cancel()

		login, err := credProvider.Credentials(ctx, credentials.Subject{
			Target:       target,
			Hostname:     hostName,
			SerialNumber: serialNumber,
		})
		if err != nil {
			writeCredentialError(w, target, err)
			return
		}
		hostConfig := HostConfig{Username: login.Username, Password: login.Password}

		httpClient := &http.Client{
			Transport: &http.Transport{
				TLSClientConfig:     bmcTLS.Clone(),
				TLSHandshakeTimeout: 10 * time.Second,
				Proxy:               proxyForRequest(),
			},
			Timeout: *bmcTimeout,
		}

		scheme := "https"
		if s := os.Getenv("REDFISH_SCHEME"); s == "http" {
			scheme = "http"
		}

		key := redfishsession.NormalizeTarget(target)
		sessionMgr.PrioritizeMetrics(key)

		lease, err := sessionMgr.Acquire(key, redfishsession.ConnectParams{
			Insecure:              bmcTLS.InsecureSkipVerify,
			Scheme:                scheme,
			Target:                target,
			Username:              hostConfig.Username,
			Password:              hostConfig.Password,
			HTTPClient:            httpClient,
			MaxConcurrentRequests: *redfishMaxConcurrent,
		})
		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to connect to %s: %v", target, err), http.StatusInternalServerError)
			return
		}
		defer lease.Release()

		disabledSet := parseDisabledCollectors(*disableCollectors)
		collector := collectors.NewCollector(lease.Client(), disabledSet, *exposePrometheusMetadata)
		handlers := NewHandlers(collector)
		handlers.MetricsHandler(w, r, target)
		flushResponse(w)

		if *enableOtelLogs && logStore != nil && otelExporter != nil {
			redfishEndpoint := fmt.Sprintf("%s://%s", scheme, target)
			logLogger := redfishLogLogger{}
			sessionMgr.StartLogExport(key, *otelLogTimeout, func(ctx context.Context, c *gofish.APIClient) {
				resources, updates, err := redfishlogs.CollectFromClient(ctx, c, redfishEndpoint, key, hostName, serialNumber, logCollectCfg, logStore, logLogger)
				if err != nil {
					if errors.Is(err, context.Canceled) {
						return
					}
					alog.WithError(err).Error("redfish log collect")
					return
				}
				if len(resources) == 0 {
					return
				}
				recordCount := redfishlogs.CountLogRecords(resources)
				if err := otelExporter.Export(ctx, resources); err != nil {
					if errors.Is(err, context.Canceled) {
						return
					}
					alog.WithError(err).Error("otlp log export")
					return
				}
				if err := logStore.SetMany(updates); err != nil {
					alog.WithError(err).Error("redfish log checkpoint write")
					return
				}
				alog.WithFields(alog.Fields{
					"target":        key,
					"hostname":      hostName,
					"serial_number": serialNumber,
					"log_records":   recordCount,
					"log_services":  len(resources),
					"checkpoints":   len(updates),
				}).Info("redfish log export complete")
			})
		}
	})

	addr := *listenAddress
	if addr == "" {
		addr = fmt.Sprintf("127.0.0.1:%d", *listenPort)
	}
	alog.Info(fmt.Sprintf("Starting server on %s", addr))
	if err := newServer(addr, mux, *scrapeTimeout).ListenAndServe(); err != nil {
		alog.Fatal(err.Error())
	}
}

func credentialsConfigured(providerName string, cfg *Config) bool {
	if providerName != credentials.NameConfig {
		return true
	}
	return cfg.HasPassword()
}

func writeCredentialError(w http.ResponseWriter, target string, err error) {
	var missing *onepassword.MissingFieldError
	if errors.As(err, &missing) {
		alog.WithFields(alog.Fields{
			"vault": missing.Vault,
			"item":  missing.Item,
			"field": missing.Field,
		}).Error("1Password item or field not found in vault")
	} else if ip, normErr := credentials.NormalizeTargetIP(target); normErr == nil {
		alog.WithField("target", ip).Error("credential lookup failed")
	} else {
		alog.Error("credential lookup failed")
	}
	status, msg := credentials.PublicFailure(err)
	http.Error(w, msg, status)
}
