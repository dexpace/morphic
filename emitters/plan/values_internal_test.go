// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package plan

import (
	"fmt"
	"maps"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
)

// hierarchyOf builds a chain of n models, each the Base of the next, the
// first holding a discriminator and, where every is set, each of the others
// too.
func hierarchyOf(n int, every bool) *ir.Document {
	doc := &ir.Document{Types: ir.TypeRegistry{}}
	for i := range n {
		model := &ir.Model{ID: rung(i), DiscriminatorValue: fmt.Sprint(i)}
		if i > 0 {
			model.Base = &ir.TypeRef{Target: rung(i - 1)}
		}
		if i == 0 || every {
			model.Discriminator = &ir.Discriminator{PropertyName: "kind"}
		}
		doc.Types[model.ID] = model
	}
	return doc
}

// rung names the i-th model of a chain.
func rung(i int) ir.TypeID { return ir.TypeID(fmt.Sprintf("type/rung-%d", i)) }

// TestNearestBases_CostGrowsWithTheRegistry counts the models, edges and
// subtype reads the descent makes over a chain of n models, at n, 2n and 4n,
// and holds them to one of each and their growth to the registry's, with a
// discriminator on every model and on the first alone. Reading each base's
// subtypes would take the square of n on the first, and walking up from each
// model to its nearest the square on the second.
func TestNearestBases_CostGrowsWithTheRegistry(t *testing.T) {
	t.Parallel()
	const n = 400
	for _, every := range []bool{true, false} {
		t.Run(fmt.Sprintf("every model discriminated: %v", every), func(t *testing.T) {
			t.Parallel()
			steps := func(size int) int {
				doc := hierarchyOf(size, every)
				bases := newNearestBases(doc, slices.Sorted(maps.Keys(doc.Types)))
				require.Len(t, bases.nearest, size-1, "every model but the first has a nearest base")
				want := rung(0)
				if every {
					want = rung(size - 2)
				}
				require.Equal(t, want, bases.nearest[rung(size-1)])
				require.Equal(t, 3*size-2, bases.steps,
					"each model, each supertype edge and each subtype read once")
				return bases.steps
			}
			small, double, large := steps(n), steps(2*n), steps(4*n)
			t.Logf("%d, %d and %d steps at %d, %d and %d models", small, double, large, n, 2*n, 4*n)

			require.Positive(t, double-small, "the descent reads more over a longer chain")
			assert.LessOrEqual(t, float64(large-double), 2.5*float64(double-small))
		})
	}
}
