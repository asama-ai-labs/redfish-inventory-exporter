// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Asama Inc.

package otlplogexport

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	collogspb "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	logspb "go.opentelemetry.io/proto/otlp/logs/v1"
	resourcepb "go.opentelemetry.io/proto/otlp/resource/v1"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"
)

func TestExportHTTP_roundTripProto(t *testing.T) {
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if ct := r.Header.Get("Content-Type"); ct != "application/x-protobuf" {
			t.Errorf("content-type: %s", ct)
		}
		var err error
		gotBody, err = io.ReadAll(r.Body)
		if err != nil {
			t.Fatal(err)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	rl := &logspb.ResourceLogs{
		Resource: &resourcepb.Resource{
			Attributes: []*commonpb.KeyValue{
				{Key: "host.name", Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: "node-1"}}},
				{Key: "host.serial_number", Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: "SN99"}}},
			},
		},
	}

	exp, err := NewExporter(Config{Protocol: "http", Endpoint: srv.URL, Timeout: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := exp.Close(); err != nil {
			t.Fatal(err)
		}
	}()
	if err := exp.Export(context.Background(), []*logspb.ResourceLogs{rl}); err != nil {
		t.Fatal(err)
	}
	var dec collogspb.ExportLogsServiceRequest
	if err := proto.Unmarshal(gotBody, &dec); err != nil {
		t.Fatal(err)
	}
	if len(dec.ResourceLogs) != 1 {
		t.Fatalf("resource logs: %d", len(dec.ResourceLogs))
	}
	attrs := dec.ResourceLogs[0].Resource.GetAttributes()
	var host, serial string
	for _, a := range attrs {
		switch a.Key {
		case "host.name":
			host = a.GetValue().GetStringValue()
		case "host.serial_number":
			serial = a.GetValue().GetStringValue()
		}
	}
	if host != "node-1" || serial != "SN99" {
		t.Fatalf("attrs host=%q serial=%q", host, serial)
	}
}

func TestExporterHTTP_reusesClient(t *testing.T) {
	exp := &Exporter{cfg: Config{Protocol: "http", Endpoint: "http://127.0.0.1", Timeout: time.Second}}
	c1 := exp.httpClientInstance()
	c2 := exp.httpClientInstance()
	if c1 != c2 {
		t.Fatal("expected reused http client")
	}
}

func TestExporterGRPC_dialsOnce(t *testing.T) {
	var dials atomic.Int32
	orig := grpcDialContext
	t.Cleanup(func() { grpcDialContext = orig })
	grpcDialContext = func(ctx context.Context, target string, opts ...grpc.DialOption) (*grpc.ClientConn, error) {
		dials.Add(1)
		return grpc.NewClient(target, opts...)
	}

	exp, err := NewExporter(Config{Protocol: "grpc", Endpoint: "passthrough:///unused", Timeout: time.Second, Insecure: true})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := exp.Close(); err != nil {
			t.Fatal(err)
		}
	}()
	if dials.Load() != 1 {
		t.Fatalf("initial dials: %d", dials.Load())
	}
	if err := exp.ensureGRPCConn(context.Background()); err != nil {
		t.Fatal(err)
	}
	if dials.Load() != 1 {
		t.Fatalf("expected no second dial, got: %d", dials.Load())
	}
}
