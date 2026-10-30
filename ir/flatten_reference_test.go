// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package ir_test

import (
	"fmt"
	"math/rand/v2"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/ir/irtest"
)

// TestFlattenedProperties_MatchesTheRulesOnAcyclicCompositions holds every
// flattening of a composition with no cycle to reference, which states the
// rules of ir-design §4.3 as directly as they read: order included, which the
// order-free reading cannot check. Its roots are those flatRandom gives no
// cycle and every root of dagRandom, which never builds one.
func TestFlattenedProperties_MatchesTheRulesOnAcyclicCompositions(t *testing.T) {
	t.Parallel()
	compared := 0
	for seed := range uint64(300) {
		for name, gen := range map[string]func(uint64) (*ir.Document, []ir.TypeID){"flatRandom": flatRandom, "dagRandom": dagRandom} {
			doc, types := gen(seed)
			for _, root := range types {
				models, _ := composition(doc.Types, root)
				if cyclic(doc.Types, models) {
					continue
				}
				compared++
				what := fmt.Sprintf("%s seed %d, root %q", name, seed, root)
				f := ir.FlattenedProperties(doc.Types, root)
				lines, undeclared, unflattened := reference(t, doc.Types, root)
				assert.Equal(t, lines, flatLines(f), "%s: members", what)
				assert.Equal(t, undeclared, f.Undeclared, "%s: undeclared", what)
				assert.Equal(t, unflattened, f.Unflattened, "%s: unflattened", what)
			}
		}
	}
	require.Greater(t, compared, 3000, "the comparison must reach many acyclic compositions to mean anything")
}

// cyclic reports whether any of models composes itself.
func cyclic(types ir.TypeRegistry, models map[ir.TypeID]*ir.Model) bool {
	for id := range models {
		if composedBelow(types, id)[id] {
			return true
		}
	}
	return false
}

// reference flattens the acyclic composition of the model root names, read
// through alias scalars, as flatLines renders a flattening: each model after
// every model it composes, its entries followed in order, Base, Implements,
// Mixins, depth first and each model once; one member per wire name at its
// first reading, taken by its last, overriding each other declaration once at
// its first reading; required where a declaration or a Requires says so.
func reference(t *testing.T, types ir.TypeRegistry, root ir.TypeID) (lines, undeclared []string, unflattened []ir.TypeID) {
	t.Helper()
	r := referenceWalk{t: t, types: types, seen: map[ir.TypeID]bool{}, marked: map[ir.TypeID]bool{}}
	if m, ok := types[standsFor(types, root)].(*ir.Model); !ok || m == nil {
		return []string{}, nil, nil
	}
	r.visit(standsFor(types, root), 0)
	return r.result()
}

// referenceWalk is one reference walk: the models in the order it read them,
// and the entry targets naming no model in the order it met them.
type referenceWalk struct {
	t           *testing.T
	types       ir.TypeRegistry
	seen        map[ir.TypeID]bool
	order       []ir.TypeID
	marked      map[ir.TypeID]bool
	unflattened []ir.TypeID
}

// visit reads the model id and, first, every model it composes. Depth is
// bounded by the registry, which has no cycle here.
func (r *referenceWalk) visit(id ir.TypeID, depth int) {
	r.t.Helper()
	require.Less(r.t, depth, 64, "an acyclic registry of at most twenty types nests no deeper")
	r.seen[id] = true
	for _, target := range entries(r.types[id].(*ir.Model)) {
		end := standsFor(r.types, target)
		if m, ok := r.types[end].(*ir.Model); !ok || m == nil {
			if !r.marked[target] {
				r.marked[target] = true
				r.unflattened = append(r.unflattened, target)
			}
			continue
		}
		if !r.seen[end] {
			r.visit(end, depth+1)
		}
	}
	r.order = append(r.order, id)
}

// result reads the models in order into members, and renders them.
func (r *referenceWalk) result() (lines, undeclared []string, unflattened []ir.TypeID) {
	type reading struct {
		owner ir.TypeID
		p     *ir.Property
	}
	var names []string
	reads := map[string][]reading{}
	var requires []string
	listed := map[string]bool{}
	for _, id := range r.order {
		m := r.types[id].(*ir.Model)
		for i := range m.Properties {
			wire := m.Properties[i].EffectiveWireName()
			if _, known := reads[wire]; !known {
				names = append(names, wire)
			}
			reads[wire] = append(reads[wire], reading{id, &m.Properties[i]})
		}
		for _, name := range m.Requires {
			if !listed[name] {
				listed[name] = true
				requires = append(requires, name)
			}
		}
	}
	lines = make([]string, 0, len(names))
	for _, wire := range names {
		rs := reads[wire]
		last := rs[len(rs)-1]
		line := fmt.Sprintf("%s@%s", last.p.ID, last.owner)
		required := listed[wire]
		shown := map[ir.PropID]bool{last.p.ID: true}
		for _, rd := range rs {
			required = required || rd.p.Required
			if !shown[rd.p.ID] {
				shown[rd.p.ID] = true
				line += fmt.Sprintf(" < %s@%s", rd.p.ID, rd.owner)
			}
		}
		if required {
			line += " required"
		}
		lines = append(lines, line)
	}
	for _, name := range requires {
		if _, declared := reads[name]; !declared {
			undeclared = append(undeclared, name)
		}
	}
	return lines, undeclared, r.unflattened
}

// dagRandom builds a registry like flatRandom's whose composition has no
// cycle: each model and alias scalar names only types after it, or one that
// is no model (an enum, an undeclared ID, a typed nil, an unset reference).
func dagRandom(seed uint64) (*ir.Document, []ir.TypeID) {
	rng := rand.New(rand.NewPCG(seed, seed+23))
	n := 2 + rng.IntN(16)
	leaves := []ir.TypeID{"enum", "ghost", "nilm", ""}
	ids := make([]ir.TypeID, 0, n+len(leaves))
	for i := range n {
		ids = append(ids, ir.TypeID(fmt.Sprintf("d%02d", i)))
	}
	pick := func(after int) ir.TypeID {
		if after+1 >= n || rng.IntN(5) == 0 {
			return leaves[rng.IntN(len(leaves))]
		}
		return ids[after+1+rng.IntN(n-after-1)]
	}
	doc := irtest.DocOf(&ir.Enum{ID: "enum"})
	doc.Types["nilm"] = (*ir.Model)(nil)
	for i, id := range ids {
		if rng.IntN(5) == 0 {
			doc.Types[id] = irtest.ScalarOf(id, pick(i))
			continue
		}
		doc.Types[id] = irtest.ModelOf(id, func(m *ir.Model) {
			for range rng.IntN(4) {
				digit := rng.IntN(8)
				m.Properties = append(m.Properties, ir.Property{
					ID: ir.PropID(fmt.Sprintf("p%d", digit)), WireName: fmt.Sprintf("w%d", digit%3),
					Required: digit == 1 || digit == 4,
				})
			}
			if rng.IntN(2) == 0 {
				m.Base = &ir.TypeRef{Target: pick(i)}
			}
			for range rng.IntN(3) {
				m.Implements = append(m.Implements, ir.TypeRef{Target: pick(i)})
			}
			for range rng.IntN(3) {
				m.Mixins = append(m.Mixins, ir.TypeRef{Target: pick(i)})
			}
			if rng.IntN(3) == 0 {
				m.Requires = []string{fmt.Sprintf("w%d", rng.IntN(4))}
			}
		})
	}
	return doc, append(ids, leaves...)
}
