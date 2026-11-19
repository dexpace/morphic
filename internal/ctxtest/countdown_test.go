// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package ctxtest_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/internal/ctxtest"
)

// TestCountdown_RefusesFromItsLeftOn pins the schedule: Err answers nil for the
// first Left calls, the context's error from then on, and counts every call.
// Refused turns as the first refusal is answered, and OnRefuse is called then,
// once.
func TestCountdown_RefusesFromItsLeftOn(t *testing.T) {
	t.Parallel()
	var told []int
	countdown := &ctxtest.Countdown{Context: t.Context(), Left: 2}
	countdown.OnRefuse = func() { told = append(told, countdown.Calls) }

	const asks = 4
	got, refused := make([]error, 0, asks), make([]bool, 0, asks)
	for range asks {
		got = append(got, countdown.Err())
		refused = append(refused, countdown.Refused())
	}

	assert.Equal(t, []error{nil, nil, context.Canceled, context.Canceled}, got)
	assert.Equal(t, []bool{false, false, true, true}, refused)
	assert.Equal(t, []int{3}, told, "told once, at the call that first refused")
	assert.Equal(t, asks, countdown.Calls)
	assert.Zero(t, countdown.Left)
}

// TestCountdown_RefusesWithNoOneToTell pins that OnRefuse is optional.
func TestCountdown_RefusesWithNoOneToTell(t *testing.T) {
	t.Parallel()
	countdown := &ctxtest.Countdown{Context: t.Context()}

	assert.ErrorIs(t, countdown.Err(), context.Canceled)
	assert.True(t, countdown.Refused())
}
