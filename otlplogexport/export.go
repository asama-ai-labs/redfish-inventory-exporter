// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Asama Inc.

package otlplogexport

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	collogspb "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	logspb "go.opentelemetry.io/proto/otlp/logs/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/proto"
)

// grpcDialContext dials gRPC endpoints; tests may override to count reuse.
var grpcDialContext = grpc.DialContext

// Config selects OTLP transport and endpoint.
type Config struct {
	Protocol string // "http" or "grpc"
	Endpoint string
	Timeout  time.Duration

	Insecure           bool
	InsecureSkipVerify bool
}

// Exporter sends OTLP logs over a reused HTTP client or gRPC connection.
type Exporter struct {
	cfg Config
	mu  sync.Mutex

	httpClient *http.Client
	grpcConn   *grpc.ClientConn
}

// NewExporter returns an exporter configured for the selected protocol.
func NewExporter(cfg Config) (*Exporter, error) {
	switch cfg.Protocol {
	case "http", "grpc":
	default:
		return nil, fmt.Errorf("otlplogexport: unsupported protocol %q", cfg.Protocol)
	}
	e := &Exporter{cfg: cfg}
	if cfg.Protocol == "grpc" {
		if err := e.ensureGRPCConn(context.Background()); err != nil {
			return nil, err
		}
	}
	return e, nil
}

// Export sends resource logs using OTLP HTTP or gRPC.
func (e *Exporter) Export(ctx context.Context, resourceLogs []*logspb.ResourceLogs) error {
	if len(resourceLogs) == 0 {
		return nil
	}
	req := &collogspb.ExportLogsServiceRequest{ResourceLogs: resourceLogs}
	switch e.cfg.Protocol {
	case "grpc":
		return e.exportGRPC(ctx, req)
	default:
		return e.exportHTTP(ctx, req)
	}
}

// Close releases pooled transport resources.
func (e *Exporter) Close() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.grpcConn != nil {
		err := e.grpcConn.Close()
		e.grpcConn = nil
		return err
	}
	return nil
}

// Export sends resource logs using a one-shot exporter (for tests and simple callers).
func Export(ctx context.Context, cfg Config, resourceLogs []*logspb.ResourceLogs) (err error) {
	exp, err := NewExporter(cfg)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := exp.Close(); err == nil && cerr != nil {
			err = cerr
		}
	}()
	return exp.Export(ctx, resourceLogs)
}

func (e *Exporter) exportHTTP(ctx context.Context, req *collogspb.ExportLogsServiceRequest) error {
	body, err := proto.Marshal(req)
	if err != nil {
		return err
	}

	client := e.httpClientInstance()
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, e.cfg.Endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	httpReq.Header.Set("Content-Type", "application/x-protobuf")

	resp, err := client.Do(httpReq)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("otlplogexport: HTTP %s", resp.Status)
	}
	return nil
}

func (e *Exporter) httpClientInstance() *http.Client {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.httpClient != nil {
		return e.httpClient
	}
	to := e.cfg.Timeout
	if to <= 0 {
		to = 30 * time.Second
	}
	client := &http.Client{Timeout: to}
	if strings.HasPrefix(strings.ToLower(e.cfg.Endpoint), "https://") && e.cfg.InsecureSkipVerify {
		client.Transport = &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec
		}
	}
	e.httpClient = client
	return e.httpClient
}

func (e *Exporter) ensureGRPCConn(ctx context.Context) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.grpcConn != nil {
		return nil
	}
	to := e.cfg.Timeout
	if to <= 0 {
		to = 30 * time.Second
	}
	dialCtx, cancel := context.WithTimeout(ctx, to)
	defer cancel()

	var opts []grpc.DialOption
	if e.cfg.Insecure {
		opts = append(opts, grpc.WithTransportCredentials(insecure.NewCredentials()))
	} else {
		opts = append(opts, grpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{
			InsecureSkipVerify: e.cfg.InsecureSkipVerify, //nolint:gosec
		})))
	}

	conn, err := grpcDialContext(dialCtx, e.cfg.Endpoint, opts...)
	if err != nil {
		return fmt.Errorf("otlplogexport: grpc dial: %w", err)
	}
	e.grpcConn = conn
	return nil
}

func (e *Exporter) exportGRPC(ctx context.Context, req *collogspb.ExportLogsServiceRequest) error {
	if err := e.ensureGRPCConn(ctx); err != nil {
		return err
	}

	to := e.cfg.Timeout
	if to <= 0 {
		to = 30 * time.Second
	}
	callCtx, callCancel := context.WithTimeout(ctx, to)
	defer callCancel()

	e.mu.Lock()
	conn := e.grpcConn
	e.mu.Unlock()
	if conn == nil {
		return fmt.Errorf("otlplogexport: grpc connection unavailable")
	}

	cli := collogspb.NewLogsServiceClient(conn)
	_, err := cli.Export(callCtx, req)
	if err != nil {
		return fmt.Errorf("otlplogexport: grpc export: %w", err)
	}
	return nil
}

// DefaultEndpoint returns the conventional OTLP logs URL/host for the protocol.
func DefaultEndpoint(protocol string) string {
	switch protocol {
	case "grpc":
		return "localhost:4317"
	default:
		return "http://localhost:4318/v1/logs"
	}
}

// NormalizeGRPCEndpoint returns host:port for grpc.Dial.
func NormalizeGRPCEndpoint(endpoint string) string {
	e := strings.TrimSpace(endpoint)
	if !strings.Contains(e, "://") {
		return e
	}
	u, err := url.Parse(e)
	if err != nil || u.Host == "" {
		return e
	}
	return u.Host
}
