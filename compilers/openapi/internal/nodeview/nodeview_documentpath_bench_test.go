// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package nodeview_test

import (
	"encoding/json/jsontext"
	"fmt"
	"testing"

	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
)

// BenchmarkDocumentPath_IntoAWideMapping resolves one pointer per component of
// a components mapping, as the schema bindings do when every schema is
// referenced once.
//
// It guards a shape rather than a number. Each width does n times one
// resolution's work, so read the per-component cost: it should stay flat across
// widths, and growth with n means the index is no longer reached
// (TestDocumentPath_ReachesTheIndexOnAWideMapping asserts that without a
// stopwatch). Without keyIndex the cost is quadratic.
//
// The narrow widths show where an index loses: below minIndexedPairs the walk
// scans, and a regression there means the gate has stopped paying for itself.
func BenchmarkDocumentPath_IntoAWideMapping(b *testing.B) {
	for _, n := range []int{2, 8, 64, 256, 1024} {
		root := nodeview.ComponentsDoc(n)
		pointers := make([]jsontext.Pointer, n)
		for i := range pointers {
			pointers[i] = jsontext.Pointer(fmt.Sprintf("/components/schemas/S%d", i))
		}

		b.Run(fmt.Sprintf("components%d", n), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				v := nodeview.New() // one view per pass: a view never outlives its compile
				for _, p := range pointers {
					if _, complete := v.DocumentPath(root, p); !complete {
						b.Fatalf("pointer %s must resolve", p)
					}
				}
			}
		})
	}
}
