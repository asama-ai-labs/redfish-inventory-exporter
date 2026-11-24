// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Asama Inc.

package onepassword

import (
	"context"
	"time"
)

// A successful lookup is reused for secretCacheTTL. Concurrent scrapes for the
// same item share one lookup. A missing field is not cached.

func (p *secretProvider) singleflight(ctx context.Context, key string, fn func() (login, error)) (login, error) {
	p.mu.Lock()
	if call := p.inflight[key]; call != nil {
		p.mu.Unlock()
		select {
		case <-ctx.Done():
			return login{}, ErrLookupFailed
		case <-call.done:
			return call.val, call.err
		}
	}
	call := &loginCall{done: make(chan struct{})}
	p.inflight[key] = call
	p.mu.Unlock()

	val, err := fn()
	call.val, call.err = val, err
	close(call.done)

	p.mu.Lock()
	delete(p.inflight, key)
	p.mu.Unlock()
	return val, err
}

func (p *secretProvider) cached(key string, fresh bool) (login, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	ent, ok := p.cache[key]
	if !ok || (fresh && time.Now().After(ent.expires)) {
		return login{}, false
	}
	return ent.value, true
}

func (p *secretProvider) remember(key string, value login) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.cache[key] = cachedLogin{value: value, expires: time.Now().Add(secretCacheTTL)}
}
