// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Asama Inc.

package credentials

import (
	"context"
	"errors"
	"strings"
)

// DefaultName is the integration item, and the YAML host key, used when a
// target has no password of its own.
const DefaultName = "default"

// ErrNotFound means this step has no password. The lookup chain tries the next step.
var ErrNotFound = errors.New("credentials not found")

// PickLogin returns the first login that has a password.
// A step returns ErrNotFound to continue. Any other error stops the chain.
func PickLogin(ctx context.Context, steps ...func(context.Context) (Login, error)) (Login, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	var missed error
	for _, step := range steps {
		if step == nil {
			continue
		}
		login, err := step(ctx)
		if err == nil && strings.TrimSpace(login.Password) != "" {
			return login, nil
		}
		if err != nil && !errors.Is(err, ErrNotFound) {
			return Login{}, err
		}
		if err != nil {
			// Keep the first miss that names the item. A later plain ErrNotFound,
			// such as the YAML step, must not replace it.
			if missed == nil || missed == ErrNotFound {
				missed = err
			}
			continue
		}
		if missed == nil {
			missed = ErrNotFound
		}
	}
	if missed == nil {
		missed = ErrNotFound
	}
	return Login{}, missed
}
