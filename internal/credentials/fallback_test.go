// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Asama Inc.

package credentials

import (
	"context"
	"errors"
	"testing"
)

type detailedMiss struct{}

func (detailedMiss) Error() string { return "item missing" }

func (detailedMiss) Unwrap() error { return ErrNotFound }

func TestPickLoginKeepsFirstDetailedMiss(t *testing.T) {
	want := &detailedMiss{}
	_, err := PickLogin(context.Background(),
		func(context.Context) (Login, error) { return Login{}, want },
		func(context.Context) (Login, error) { return Login{}, ErrNotFound },
		func(context.Context) (Login, error) { return Login{}, nil },
	)
	var got *detailedMiss
	if !errors.As(err, &got) || got != want {
		t.Fatalf("err %v", err)
	}
}
