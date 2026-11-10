// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package operation

import (
	"maps"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/ir"
)

// TestKeptOnce_ReadsADeclarationOnce holds what makes a kept entry cost what the
// source wrote: the first reach reads and reports, a later one through any copy of
// the walk does neither, and another declaration is a read of its own.
func TestKeptOnce_ReadsADeclarationOnce(t *testing.T) {
	t.Parallel()
	w := walk{kept: newKeptOnce()}
	reads := 0
	keep := func(key string) func() (ir.Unmodeled, []ir.Diagnostic) {
		return func() (ir.Unmodeled, []ir.Diagnostic) {
			reads++
			return ir.Unmodeled{key: {Reason: ir.ReasonDegradedLowering}}, []ir.Diagnostic{{Code: key}}
		}
	}
	reach := func(copied walk, coordinate string) []ir.Diagnostic {
		return copied.kept.once(coordinate, keep(coordinate))
	}

	assert.Len(t, reach(w, "a"), 1, "the first reach reports")
	assert.Empty(t, reach(w, "a"), "a copy shares what was read")
	assert.Len(t, reach(w, "b"), 1)

	assert.Equal(t, 2, reads)
	assert.Equal(t, []string{"a", "b"}, slices.Sorted(maps.Keys(w.kept.entries)))
}
