// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package load

import (
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
)

// TestRunRecovered_QuotesAPanicOnOneBoundedLine pins that every walk and build
// load runs behind runRecovered reports a panic as diag.Catch quotes it,
// whatever it panicked with, after the name of what was running.
func TestRunRecovered_QuotesAPanicOnOneBoundedLine(t *testing.T) {
	t.Parallel()
	huge := strings.Repeat("the walk faulted here\n", 1<<10)

	err := runRecovered("validation", func() error { panic(huge) })

	require.Error(t, err)
	assert.Equal(t, "validation panicked: "+diag.OneLine(errors.New(huge)), err.Error())
	assert.NotContains(t, err.Error(), "\n")
	assert.Less(t, len(err.Error()), len(huge)/4, "the bound cut the value")
}

// TestRunRecovered_ReturnsWhatTheWorkReturned pins the barrier as a pass-through for
// work that returns.
func TestRunRecovered_ReturnsWhatTheWorkReturned(t *testing.T) {
	t.Parallel()
	refused := errors.New("refused")

	require.NoError(t, runRecovered("walk", func() error { return nil }))
	assert.Same(t, refused, runRecovered("walk", func() error { return refused }))
}
