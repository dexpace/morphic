// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package plan

import (
	"encoding/json/v2"
	"fmt"
	"maps"
	"math/rand/v2"
	"slices"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
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
		model := &ir.Model{ID: chainModelID(i), DiscriminatorValue: fmt.Sprint(i)}
		if i > 0 {
			model.Base = &ir.TypeRef{Target: chainModelID(i - 1)}
		}
		if i == 0 || every {
			model.Discriminator = &ir.Discriminator{PropertyName: "kind"}
		}
		doc.Types[model.ID] = model
	}
	return doc
}

// chainModelID returns the ID of the i-th model of a chain.
func chainModelID(i int) ir.TypeID { return ir.TypeID(fmt.Sprintf("type/chain-%d", i)) }

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
				want := chainModelID(0)
				if every {
					want = chainModelID(size - 2)
				}
				require.Equal(t, want, bases.nearest[chainModelID(size-1)])
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

// nearestByWalk returns the nearest base of id read as
// docs/emitters/plan/types.md#a-discriminators-routes states it, one model at
// a time: level by level up ir.Supertypes, the first model holding a
// discriminator in the earliest level holding one, each level in the order of
// the supertypes reaching it, and never id itself.
func nearestByWalk(doc *ir.Document, id ir.TypeID) (ir.TypeID, bool) {
	visited := map[ir.TypeID]bool{id: true}
	level := []ir.TypeID{id}
	for hops := 0; len(level) > 0 && hops < len(doc.Types); hops++ {
		var next []ir.TypeID
		for _, model := range level {
			for _, super := range ir.Supertypes(doc, model) {
				if !visited[super] {
					visited[super] = true
					next = append(next, super)
				}
			}
		}
		for _, model := range next {
			if holder, isModel := doc.Types[model].(*ir.Model); isModel && holder.Discriminator != nil {
				return model, true
			}
		}
		level = next
	}
	return "", false
}

// routesByWalk returns the routes valueRoutes gives doc, read off
// nearestByWalk: a value a Mapping keys to its model routes from no nearest.
func routesByWalk(doc *ir.Document, ids []ir.TypeID) map[ir.TypeID][]valueRoute {
	keyed := map[valueRoute]bool{}
	for _, id := range ids {
		if holder, isModel := doc.Types[id].(*ir.Model); isModel && holder.Discriminator != nil {
			for tag, target := range holder.Discriminator.Mapping {
				keyed[valueRoute{tag: tag, subtype: target}] = true
			}
		}
	}
	routes := map[ir.TypeID][]valueRoute{}
	for _, id := range ids {
		model, isModel := doc.Types[id].(*ir.Model)
		if !isModel || model.DiscriminatorValue == "" {
			continue
		}
		route := valueRoute{tag: model.DiscriminatorValue, subtype: id}
		if holder, found := nearestByWalk(doc, id); found && !keyed[route] {
			routes[holder] = append(routes[holder], route)
		}
	}
	return routes
}

// generatedHierarchy builds a registry of up to eight models from random:
// each may name any of them, itself included, as its Base and up to two as
// Implements, some through an alias scalar; some hold a discriminator whose
// Mapping keys a tag to any model, and most carry a DiscriminatorValue.
func generatedHierarchy(random *rand.Rand) *ir.Document {
	size := 1 + random.IntN(8)
	doc := &ir.Document{Types: ir.TypeRegistry{}}
	modelID := func(i int) ir.TypeID { return ir.TypeID(fmt.Sprintf("type/m%d", i)) }
	tags := []string{"", "a", "b", "c"}
	supertype := func() ir.TypeRef {
		target := modelID(random.IntN(size))
		if random.IntN(6) == 0 {
			alias := &ir.Scalar{ID: target + "-alias", Base: &ir.TypeRef{Target: target}}
			doc.Types[alias.ID] = alias
			target = alias.ID
		}
		return ir.TypeRef{Target: target}
	}
	for i := range size {
		model := &ir.Model{ID: modelID(i), DiscriminatorValue: tags[random.IntN(len(tags))]}
		if random.IntN(4) > 0 {
			base := supertype()
			model.Base = &base
		}
		for range random.IntN(3) {
			model.Implements = append(model.Implements, supertype())
		}
		if random.IntN(5) < 2 {
			model.Discriminator = &ir.Discriminator{PropertyName: "kind"}
			if random.IntN(2) == 0 {
				tag := tags[1+random.IntN(len(tags)-1)]
				model.Discriminator.Mapping = map[string]ir.TypeID{tag: modelID(random.IntN(size))}
			}
		}
		doc.Types[model.ID] = model
	}
	return doc
}

// TestNearestBases_AgreesWithAWalkUpEachModel holds the descent, over
// registries generated from a fixed seed, to nearestByWalk for every model
// and valueRoutes to the routes read off it: a model its own nearest, a
// farther holder taken or a tie decided by another order than the
// supertypes' differs from the walk.
func TestNearestBases_AgreesWithAWalkUpEachModel(t *testing.T) {
	t.Parallel()
	random := rand.New(rand.NewPCG(2136, 2313))
	for run := range 20000 {
		doc := generatedHierarchy(random)
		ids := slices.Sorted(maps.Keys(doc.Types))
		want := map[ir.TypeID]ir.TypeID{}
		for _, id := range ids {
			if holder, found := nearestByWalk(doc, id); found {
				want[id] = holder
			}
		}
		got := newNearestBases(doc, ids).nearest
		if diff := cmp.Diff(want, got); diff != "" {
			encoded, err := json.Marshal(doc.Types, json.Deterministic(true))
			require.NoError(t, err)
			t.Fatalf("run %d: %s\nnearest (-walk +descent):\n%s", run, encoded, diff)
		}
		wantRoutes := routesByWalk(doc, ids)
		gotRoutes := valueRoutes(doc, ids)
		require.Empty(t, cmp.Diff(wantRoutes, gotRoutes, cmp.AllowUnexported(valueRoute{}), cmpopts.EquateEmpty()),
			"run %d (-walk +descent)", run)
	}
}

// TestNearestBases_ReachesAHolderPastACycleThroughItself holds a model holding
// a discriminator whose supertype takes it as that supertype's own nearest:
// it takes the supertype's next nearest, at the fewest hops and, of those as
// near, through its earlier supertype, rather than a farther holder or none.
func TestNearestBases_ReachesAHolderPastACycleThroughItself(t *testing.T) {
	t.Parallel()
	model := func(id ir.TypeID, holds bool, base ir.TypeID, implements ...ir.TypeID) *ir.Model {
		built := &ir.Model{ID: id}
		if base != "" {
			built.Base = &ir.TypeRef{Target: base}
		}
		for _, super := range implements {
			built.Implements = append(built.Implements, ir.TypeRef{Target: super})
		}
		if holds {
			built.Discriminator = &ir.Discriminator{PropertyName: "kind"}
		}
		return built
	}
	tests := []struct {
		name  string
		types []*ir.Model
		want  map[ir.TypeID]ir.TypeID
	}{
		{"a holder above the cycle, through Implements", []*ir.Model{
			model("type/a", true, "type/b"), model("type/b", false, "type/a", "type/f"), model("type/f", true, ""),
		}, map[ir.TypeID]ir.TypeID{"type/a": "type/f", "type/b": "type/a"}},
		{"a nearer holder through the cycle than through the other supertype", []*ir.Model{
			model("type/s", true, "type/x", "type/y"), model("type/x", false, "type/s", "type/h"),
			model("type/h", true, ""), model("type/y", false, "type/z"), model("type/z", false, "type/g"),
			model("type/g", true, ""),
		}, map[ir.TypeID]ir.TypeID{
			"type/s": "type/h", "type/x": "type/s", "type/y": "type/g", "type/z": "type/g",
		}},
		{"two holders as near, the one through the cycle reached through the Base", []*ir.Model{
			model("type/s", true, "type/x", "type/y"), model("type/x", false, "type/s", "type/h"),
			model("type/h", true, ""), model("type/y", false, "type/g"), model("type/g", true, ""),
		}, map[ir.TypeID]ir.TypeID{"type/s": "type/h", "type/x": "type/s", "type/y": "type/g"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			doc := &ir.Document{Types: ir.TypeRegistry{}}
			for _, built := range tt.types {
				doc.Types[built.ID] = built
			}

			got := newNearestBases(doc, slices.Sorted(maps.Keys(doc.Types))).nearest

			assert.Empty(t, cmp.Diff(tt.want, got), "(-want +got)")
		})
	}
}
