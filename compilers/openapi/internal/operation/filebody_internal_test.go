// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package operation

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering/loweringtest"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
)

// TestLeastOf_StopsAtOther holds leastOf to the least of its items and to the
// items it reads: none past the first reading as other, which no later one can
// lower, so a wide union one branch decides is read no further than that
// branch. No items read as the top.
func TestLeastOf_StopsAtOther(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		items []fileReading
		want  fileReading
		reads int
	}{
		{"none", nil, readsBinary, 0},
		{"raw bytes alone", []fileReading{readsBinary, readsBinary}, readsBinary, 2},
		{"the least of open readings", []fileReading{readsUntyped, readsBinary, readsString, readsUntyped}, readsString, 4},
		{"other first", []fileReading{readsOther, readsBinary, readsUntyped}, readsOther, 1},
		{"other among them", []fileReading{readsUntyped, readsOther, readsString, readsBinary}, readsOther, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			reads := 0
			c := loweringtest.Context(t, openapitest.EmptySpec, lowering.Policy{})
			got := leastOf(c, tc.items, func(_ lowering.Ctx, r fileReading) fileReading {
				reads++
				return r
			})
			assert.Equal(t, tc.want, got, "least")
			assert.Equal(t, tc.reads, reads, "items read")
		})
	}
}
