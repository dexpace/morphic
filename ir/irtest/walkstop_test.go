// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package irtest_test

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/ir/irtest"
)

// cutDoc returns a document whose one model holds an example nested to the
// walk's depth cap, so a walk of it stops.
func cutDoc() *ir.Document {
	deep := irtest.NestedListValue(ir.MaxWalkDepth)
	return irtest.DocOf(irtest.ModelOf("t/x/M", func(m *ir.Model) {
		m.Examples = []ir.Example{{Value: &deep}}
	}))
}

// TestSpacesUsed_SaysWhenTheWalkWasCut holds the result to telling a caller the
// spaces are those of part of the document: the map is what the walk found,
// and the second result says it stopped.
func TestSpacesUsed_SaysWhenTheWalkWasCut(t *testing.T) {
	t.Parallel()
	got, cut := irtest.SpacesUsed(cutDoc())

	assert.True(t, cut)
	assert.Equal(t, map[string][]string{ir.IDKindType: {"x"}}, got, "what the walk found before it stopped")
}

// TestAllocatingVisits_ListsACutWalkAsAPath holds the measurement to failing a
// walk it could not finish: a document read in part is no document measured
// clean, so the cut is one path in the list, and assert.Empty fails on it. It is
// not run in parallel: AllocsPerRun measures the process.
func TestAllocatingVisits_ListsACutWalkAsAPath(t *testing.T) {
	nothing := ir.VisitFunc(func(reflect.Value, ir.WalkPath) bool { return true })

	assert.Empty(t, irtest.AllocatingVisits(irtest.DocOf(irtest.ModelOf("t/x/M", nil)), nothing),
		"the control: a whole walk of a visitor doing nothing lists nothing")
	paths := irtest.AllocatingVisits(cutDoc(), nothing)

	require.Len(t, paths, 1, "one path for the cut")
	assert.NotEmpty(t, paths[0], "and it names the cut")
}
