// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package testspec_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/internal/testspec"
)

// TestRefShapedData_IsFreshOnEachCall pins that a caller's edit to its rows
// never reaches the next caller, which a shared slice would let through.
func TestRefShapedData_IsFreshOnEachCall(t *testing.T) {
	t.Parallel()
	rows := testspec.RefShapedData()
	rows[0].Data = "edited"
	assert.NotEqual(t, "edited", testspec.RefShapedData()[0].Data)
}
