// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Asama Inc.

// Package redfishsession coordinates one gofish session per Redfish target with refcounted leases.
package redfishsession

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/stmcginnis/gofish"
)

// dialConnect creates gofish sessions; tests may override.
var dialConnect = gofish.ConnectContext

// logout ends a client's session; tests may override it to count calls. gofish's Logout deletes
// the session by the id it got at login. With basic auth there is no session and no id, so it
// would send DELETE "" to the BMC on every scrape. Closing the idle connections is all that is left.
var logout = func(c *gofish.APIClient) {
	if c != nil && c.HTTPClient != nil {
		c.HTTPClient.CloseIdleConnections()
	}
}

// ConnectParams holds everything needed to create a session the first time a target is used.
type ConnectParams struct {
	Insecure              bool   // skip TLS verification (gofish's own setting)
	Scheme                string // "https" or "http"
	Target                string // host:port (no scheme)
	Username              string
	Password              string
	HTTPClient            *http.Client
	MaxConcurrentRequests int64
}

// Manager owns per-target sessions and lease counts.
type Manager struct {
	mu      sync.Mutex
	targets map[string]*targetSession
}

// NewManager returns an empty session manager.
func NewManager() *Manager {
	return &Manager{targets: make(map[string]*targetSession)}
}

// NormalizeTarget trims whitespace for map keys.
func NormalizeTarget(target string) string {
	return strings.TrimSpace(target)
}

func (m *Manager) getOrCreateTS(key string) *targetSession {
	m.mu.Lock()
	defer m.mu.Unlock()
	if ts, ok := m.targets[key]; ok {
		return ts
	}
	ts := &targetSession{mgr: m, key: key}
	m.targets[key] = ts
	return ts
}

// PrioritizeMetrics cancels any in-flight log export for the target so a metrics scrape
// can use the shared session without waiting on log collection.
func (m *Manager) PrioritizeMetrics(targetKey string) {
	m.mu.Lock()
	ts := m.targets[targetKey]
	m.mu.Unlock()
	if ts != nil {
		ts.cancelLogExport()
	}
}

// Acquire connects if needed and increments the lease count for this target.
func (m *Manager) Acquire(key string, p ConnectParams) (*Lease, error) {
	for {
		ts := m.getOrCreateTS(key)
		lease, err := ts.connectAndAcquire(p)
		if err == errTargetRemoved {
			// The last lease dropped and the idle target was forgotten; use a fresh one.
			continue
		}
		return lease, err
	}
}

// errTargetRemoved means the targetSession was dropped from the manager between lookup and use.
var errTargetRemoved = fmt.Errorf("redfishsession: target removed")

// forget drops an idle target so the map does not grow with every distinct target.
func (m *Manager) forget(key string, ts *targetSession) {
	m.mu.Lock()
	if m.targets[key] == ts {
		delete(m.targets, key)
	}
	m.mu.Unlock()
}

// StartLogExport runs log export in the background. Metrics scrapes cancel it via PrioritizeMetrics.
// The session lease is bumped so the metrics lease can be released without logging out.
func (m *Manager) StartLogExport(
	targetKey string,
	timeout time.Duration,
	job func(ctx context.Context, client *gofish.APIClient),
) {
	m.mu.Lock()
	ts := m.targets[targetKey]
	m.mu.Unlock()
	if ts == nil || job == nil {
		return
	}

	if timeout <= 0 {
		timeout = 30 * time.Second
	}

	parentCtx, cancel := context.WithCancel(context.Background())

	ts.mu.Lock()
	if ts.logJobRunning || ts.client == nil {
		ts.mu.Unlock()
		cancel()
		return
	}
	ts.logJobRunning = true
	ts.logCancel = cancel
	// A full lease, counted per client like any other, so the metrics lease can be released
	// without logging out while this job still uses the session.
	lease := ts.grantLease(ts.client)
	client := ts.client
	ts.mu.Unlock()

	go func() {
		defer func() {
			ts.mu.Lock()
			ts.logJobRunning = false
			ts.logCancel = nil
			ts.mu.Unlock()
		}()
		defer lease.Release()

		ctx, timeoutCancel := context.WithTimeout(parentCtx, timeout)
		defer timeoutCancel()

		if client != nil {
			job(ctx, client)
		}
	}()
}

type targetSession struct {
	mu sync.Mutex

	// mgr and key let an idle session remove itself; mgr is nil in tests.
	mgr     *Manager
	key     string
	removed bool

	client *gofish.APIClient
	leases int

	// refcounts per client so credential swaps can retire old sessions safely
	clientRefs map[*gofish.APIClient]int

	// set on first successful connect
	params *ConnectParams

	logJobRunning bool
	logCancel     context.CancelFunc
}

func (ts *targetSession) cancelLogExport() {
	ts.mu.Lock()
	cancel := ts.logCancel
	ts.logCancel = nil
	ts.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func paramsEqual(a, b *ConnectParams) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.Scheme == b.Scheme &&
		a.Target == b.Target &&
		a.Username == b.Username &&
		a.Password == b.Password
}

func (ts *targetSession) connectAndAcquire(p ConnectParams) (*Lease, error) {
	ts.mu.Lock()
	if ts.removed {
		ts.mu.Unlock()
		return nil, errTargetRemoved
	}
	if ts.client != nil && ts.params != nil && !paramsEqual(ts.params, &p) {
		ts.mu.Unlock()
		newClient, err := ts.dial(p)
		if err != nil {
			return nil, err
		}
		ts.mu.Lock()
		if ts.removed {
			ts.mu.Unlock()
			logout(newClient)
			return nil, errTargetRemoved
		}
		ts.client = newClient
		pc := p
		ts.params = &pc
		lease := ts.grantLease(newClient)
		ts.mu.Unlock()
		return lease, nil
	}
	if ts.client != nil {
		client := ts.client
		lease := ts.grantLease(client)
		ts.mu.Unlock()
		return lease, nil
	}
	ts.mu.Unlock()

	client, err := ts.dial(p)
	if err != nil {
		return nil, err
	}

	ts.mu.Lock()
	defer ts.mu.Unlock()
	if ts.removed {
		logout(client)
		return nil, errTargetRemoved
	}
	if ts.client == nil {
		ts.client = client
		pc := p
		ts.params = &pc
	} else {
		logout(client)
		client = ts.client
	}
	return ts.grantLease(client), nil
}

func (ts *targetSession) grantLease(client *gofish.APIClient) *Lease {
	ts.leases++
	if ts.clientRefs == nil {
		ts.clientRefs = make(map[*gofish.APIClient]int)
	}
	ts.clientRefs[client]++
	return &Lease{ts: ts, client: client}
}

func (ts *targetSession) dial(p ConnectParams) (*gofish.APIClient, error) {
	if p.HTTPClient == nil {
		return nil, fmt.Errorf("redfishsession: HTTPClient is nil")
	}
	if p.Scheme != "http" && p.Scheme != "https" {
		return nil, fmt.Errorf("redfishsession: invalid scheme %q", p.Scheme)
	}
	endpoint := fmt.Sprintf("%s://%s", p.Scheme, p.Target)
	max := p.MaxConcurrentRequests
	if max <= 0 {
		max = 2
	}
	// gofish keeps this context for every later request on the session, so it must
	// outlive one scrape. HTTPClient.Timeout bounds each request instead.
	return dialConnect(context.Background(), gofish.ClientConfig{
		Endpoint:              endpoint,
		Username:              p.Username,
		Password:              p.Password,
		Insecure:              p.Insecure,
		HTTPClient:            p.HTTPClient,
		BasicAuth:             true,
		MaxConcurrentRequests: max,
		ReuseConnections:      false,
	})
}

// Lease is a refcount holder; Release decrements and may Logout when count reaches zero.
type Lease struct {
	ts     *targetSession
	client *gofish.APIClient
}

// Client returns the gofish client pinned for this lease.
func (l *Lease) Client() *gofish.APIClient {
	if l == nil {
		return nil
	}
	return l.client
}

// Release decrements active leases and logs out clients when the last lease drops.
func (l *Lease) Release() {
	if l == nil || l.ts == nil || l.client == nil {
		return
	}
	ts := l.ts
	client := l.client
	ts.mu.Lock()
	defer ts.mu.Unlock()
	if ts.leases <= 0 {
		return
	}
	ts.leases--
	ts.clientRefs[client]--
	if ts.clientRefs[client] <= 0 {
		logout(client)
		delete(ts.clientRefs, client)
	}
	if ts.leases == 0 {
		ts.client = nil
		ts.params = nil
		if ts.mgr != nil {
			ts.removed = true
			ts.mgr.forget(ts.key, ts)
		}
	}
}
