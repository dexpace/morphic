package ctxtest_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/internal/ctxtest"
)

// TestCountdown_RefusesFromItsLeftOn pins the schedule: Err answers nil for the
// first Left calls, the context's error from then on, and counts every call.
func TestCountdown_RefusesFromItsLeftOn(t *testing.T) {
	t.Parallel()
	c := &ctxtest.Countdown{Context: t.Context(), Left: 2}

	got := []error{c.Err(), c.Err(), c.Err(), c.Err()}

	assert.Equal(t, []error{nil, nil, context.Canceled, context.Canceled}, got)
	assert.Equal(t, 4, c.Calls)
	assert.Zero(t, c.Left)
}
