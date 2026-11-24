// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Asama Inc.

package redfishsession

import (
	"context"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stmcginnis/gofish"
)

func TestParamsEqual(t *testing.T) {
	a := &ConnectParams{Scheme: "https", Target: "10.0.0.1", Username: "u1", Password: "p1"}
	b := &ConnectParams{Scheme: "https", Target: "10.0.0.1", Username: "u1", Password: "p1"}
	c := &ConnectParams{Scheme: "https", Target: "10.0.0.1", Username: "u2", Password: "p1"}
	if !paramsEqual(a, b) {
		t.Fatal("expected equal params")
	}
	if paramsEqual(a, c) {
		t.Fatal("expected different username to differ")
	}
}

func TestConnectAndAcquire_reconnectsOnCredentialChange(t *testing.T) {
	var dials atomic.Int32
	orig := dialConnect
	t.Cleanup(func() { dialConnect = orig })
	dialConnect = func(_ context.Context, _ gofish.ClientConfig) (*gofish.APIClient, error) {
		dials.Add(1)
		return &gofish.APIClient{}, nil
	}

	ts := &targetSession{}
	httpClient := &http.Client{}
	base := ConnectParams{
		Scheme:     "https",
		Target:     "10.0.0.1",
		Username:   "user-a",
		Password:   "pass-a",
		HTTPClient: httpClient,
	}

	lease1, err := ts.connectAndAcquire(base)
	if err != nil {
		t.Fatal(err)
	}
	if dials.Load() != 1 {
		t.Fatalf("dials: %d", dials.Load())
	}

	changed := base
	changed.Password = "pass-b"
	lease2, err := ts.connectAndAcquire(changed)
	if err != nil {
		t.Fatal(err)
	}
	if dials.Load() != 2 {
		t.Fatalf("expected reconnect on credential change, dials: %d", dials.Load())
	}

	lease2.Release()
	lease1.Release()
}

func TestConnectAndAcquire_keepsOldClientForExistingLeaseOnCredentialChange(t *testing.T) {
	var dials atomic.Int32
	orig := dialConnect
	t.Cleanup(func() { dialConnect = orig })
	dialConnect = func(_ context.Context, _ gofish.ClientConfig) (*gofish.APIClient, error) {
		dials.Add(1)
		return &gofish.APIClient{}, nil
	}

	ts := &targetSession{}
	httpClient := &http.Client{}
	base := ConnectParams{
		Scheme:     "https",
		Target:     "10.0.0.1",
		Username:   "user-a",
		Password:   "pass-a",
		HTTPClient: httpClient,
	}

	lease1, err := ts.connectAndAcquire(base)
	if err != nil {
		t.Fatal(err)
	}
	oldClient := lease1.Client()

	changed := base
	changed.Password = "pass-b"
	lease2, err := ts.connectAndAcquire(changed)
	if err != nil {
		t.Fatal(err)
	}
	if lease1.Client() != oldClient {
		t.Fatal("existing lease should keep its original client after credential change")
	}
	if lease2.Client() == oldClient {
		t.Fatal("new lease should use a new client after credential change")
	}

	lease2.Release()
	lease1.Release()
}

func TestStartLogExport_keepsSessionAlive(t *testing.T) {
	orig := dialConnect
	t.Cleanup(func() { dialConnect = orig })
	dialConnect = func(_ context.Context, _ gofish.ClientConfig) (*gofish.APIClient, error) {
		return &gofish.APIClient{}, nil
	}

	m := NewManager()
	key := "10.0.0.1"
	metricsLease, err := m.Acquire(key, ConnectParams{
		Scheme: "https", Target: key, Username: "u", Password: "p",
		HTTPClient: &http.Client{},
	})
	if err != nil {
		t.Fatal(err)
	}

	done := make(chan struct{})
	m.StartLogExport(key, time.Second, func(_ context.Context, _ *gofish.APIClient) {
		close(done)
	})

	metricsLease.Release()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("log export did not run")
	}

	metricsLease2, err := m.Acquire(key, ConnectParams{
		Scheme: "https", Target: key, Username: "u", Password: "p",
		HTTPClient: &http.Client{},
	})
	if err != nil {
		t.Fatal(err)
	}
	metricsLease2.Release()
}

func TestPrioritizeMetrics_cancelsLogExport(t *testing.T) {
	orig := dialConnect
	t.Cleanup(func() { dialConnect = orig })
	dialConnect = func(_ context.Context, _ gofish.ClientConfig) (*gofish.APIClient, error) {
		return &gofish.APIClient{}, nil
	}

	m := NewManager()
	key := "10.0.0.1"
	lease, err := m.Acquire(key, ConnectParams{
		Scheme: "https", Target: key, Username: "u", Password: "p",
		HTTPClient: &http.Client{},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Release()

	started := make(chan struct{})
	unblocked := make(chan struct{})
	m.StartLogExport(key, time.Minute, func(ctx context.Context, _ *gofish.APIClient) {
		close(started)
		<-ctx.Done()
		close(unblocked)
	})

	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("log export did not start")
	}

	m.PrioritizeMetrics(key)

	select {
	case <-unblocked:
	case <-time.After(time.Second):
		t.Fatal("log export was not cancelled")
	}
}

func TestNormalizeTarget(t *testing.T) {
	if got := NormalizeTarget("  10.0.0.1:443  "); got != "10.0.0.1:443" {
		t.Fatalf("got %q", got)
	}
}

func TestIdleTargetIsForgotten(t *testing.T) {
	orig := dialConnect
	t.Cleanup(func() { dialConnect = orig })
	dialConnect = func(_ context.Context, _ gofish.ClientConfig) (*gofish.APIClient, error) {
		return &gofish.APIClient{}, nil
	}
	m := NewManager()
	p := ConnectParams{Scheme: "https", Target: "10.0.0.9", Username: "u", Password: "p", HTTPClient: &http.Client{}}
	lease, err := m.Acquire("10.0.0.9", p)
	if err != nil {
		t.Fatal(err)
	}
	if len(m.targets) != 1 {
		t.Fatalf("targets while leased: %d", len(m.targets))
	}
	lease.Release()
	if len(m.targets) != 0 {
		t.Fatalf("targets after the last lease: %d", len(m.targets))
	}
	lease, err = m.Acquire("10.0.0.9", p)
	if err != nil {
		t.Fatal(err)
	}
	lease.Release()
}

func TestStartLogExport_logsOutOnceAfterTheLastLease(t *testing.T) {
	origDial, origLogout := dialConnect, logout
	t.Cleanup(func() { dialConnect, logout = origDial, origLogout })
	dialConnect = func(_ context.Context, _ gofish.ClientConfig) (*gofish.APIClient, error) {
		return &gofish.APIClient{}, nil
	}
	var logouts atomic.Int32
	logout = func(*gofish.APIClient) { logouts.Add(1) }

	m := NewManager()
	key := "10.0.0.1"
	metricsLease, err := m.Acquire(key, ConnectParams{
		Scheme: "https", Target: key, Username: "u", Password: "p", HTTPClient: &http.Client{},
	})
	if err != nil {
		t.Fatal(err)
	}

	started, finish, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
	m.StartLogExport(key, time.Minute, func(_ context.Context, _ *gofish.APIClient) {
		close(started)
		<-finish
		close(done)
	})
	<-started

	metricsLease.Release()
	if n := logouts.Load(); n != 0 {
		t.Fatalf("logged out %d time(s) while the log job still used the session", n)
	}

	close(finish)
	<-done
	deadline := time.Now().Add(2 * time.Second)
	for logouts.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	time.Sleep(50 * time.Millisecond) // a second, wrong logout would land here
	if n := logouts.Load(); n != 1 {
		t.Fatalf("logouts after the last lease = %d, want 1", n)
	}
}
