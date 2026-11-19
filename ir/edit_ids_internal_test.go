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
	td := &Model{ID: "type/m", Properties: []Property{{ID: "property/a", Args: []Parameter{{ID: "q/a"}}}, {ID: "property/b"}}}
	var idCountsValue idCounts
	list := idCountsValue.scan(nil, td)
	require.Len(t, list, 4)

	assert.Zero(t, testing.AllocsPerRun(10, func() { list = idCountsValue.scan(list[:0], td) }))
}
