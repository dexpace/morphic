// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package ir

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestIDCounts_ScanReadsEachTypeOnce pins that a scan reads what a type can
// declare the first time it meets the type and allocates nothing after, so the
// ID check costs an update its values alone. It is not run in parallel:
// AllocsPerRun measures the process.
func TestIDCounts_ScanReadsEachTypeOnce(t *testing.T) {
	td := &Model{ID: "t/m", Properties: []Property{{ID: "p/a", Args: []Parameter{{ID: "q/a"}}}, {ID: "p/b"}}}
	var c idCounts
	list := c.scan(nil, td)
	require.Len(t, list, 4)

	assert.Zero(t, testing.AllocsPerRun(10, func() { list = c.scan(list[:0], td) }))
}
