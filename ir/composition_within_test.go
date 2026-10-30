// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package ir_test

import (
	"fmt"
	"math"
	"math/rand/v2"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/ir/irtest"
)

// withinRandom builds a registry of up to twenty types: models declaring up to
// two properties from a shared pool and naming any type, an undeclared ID or
// an alias as Base, Implements and Mixins; alias scalars in chains and cycles;
// scalars with no Base; and a typed nil. It returns the document, the type IDs
// and the property IDs worth asking about.
func withinRandom(rng *rand.Rand) (*ir.Document, []ir.TypeID, []ir.PropID) {
	n := 1 + rng.IntN(20)
	types := make([]ir.TypeID, 0, n+3)
	for i := range n {
		types = append(types, ir.TypeID(fmt.Sprintf("t%02d", i)))
	}
	declared := types[:n:n]
	types = append(types, "ghost", "nilm", "")
	pick := func() ir.TypeID { return types[rng.IntN(len(types))] }
	props := []ir.PropID{"p/ghost"}
	doc := irtest.DocOf()
	for _, id := range declared {
		if rng.IntN(4) == 0 {
			doc.Types[id] = irtest.ScalarOf(id, pick())
			continue
		}
		doc.Types[id] = irtest.ModelOf(id, func(m *ir.Model) {
			for range rng.IntN(3) {
				p := ir.PropID(fmt.Sprintf("p%d", rng.IntN(8)))
				m.Properties = append(m.Properties, prop(p))
				props = append(props, p)
			}
			if rng.IntN(2) == 0 {
				m.Base = &ir.TypeRef{Target: pick()}
			}
			m.Implements = irtest.RefsTo(pickSome(rng, pick)...)
			m.Mixins = irtest.RefsTo(pickSome(rng, pick)...)
		})
	}
	doc.Types["nilm"] = (*ir.Model)(nil)
	return doc, types, props
}

// pickSome returns up to two IDs from pick.
func pickSome(rng *rand.Rand, pick func() ir.TypeID) []ir.TypeID {
	out := make([]ir.TypeID, 0, 2)
	for range rng.IntN(3) {
		out = append(out, pick())
	}
	return out
}

// within is one step-limited walk, as IsSubtypeWithin and ExposesWithin run
// one.
type within func(limit int) (answer bool, spent int, ok bool)

// requireWithinAgrees holds a step-limited walk to the unlimited answer want:
// given all the steps it needs it answers want and spends them, given exactly
// those it still does, and given one fewer it stops without an answer, having
// spent every step it had.
func requireWithinAgrees(t *testing.T, walk within, want bool, what string) {
	t.Helper()
	got, need, ok := walk(math.MaxInt)
	require.True(t, ok, "%s: an unlimited walk always finishes", what)
	require.Equal(t, want, got, "%s", what)
	got, spent, ok := walk(need)
	require.True(t, ok && got == want && spent == need, "%s: the steps it needs are enough", what)
	if need == 0 {
		return
	}
	_, spent, ok = walk(need - 1)
	require.False(t, ok, "%s: one step fewer is not enough", what)
	require.Equal(t, need-1, spent, "%s: a walk cut short spent every step it had", what)
}

func TestWithin_AnswersAsTheUnlimitedWalk(t *testing.T) {
	t.Parallel()
	for seed := range uint64(300) {
		doc, types, props := withinRandom(rand.New(rand.NewPCG(seed, seed+11)))
		for _, a := range types {
			for _, b := range types {
				requireWithinAgrees(t, func(limit int) (bool, int, bool) { return ir.IsSubtypeWithin(doc, a, b, limit) },
					ir.IsSubtype(doc, a, b), fmt.Sprintf("seed %d: IsSubtypeWithin(%q, %q)", seed, a, b))
			}
			exposed := ir.ExposedProps(doc, a)
			for _, p := range props {
				requireWithinAgrees(t, func(limit int) (bool, int, bool) { return ir.ExposesWithin(doc, a, p, limit) },
					exposed[p], fmt.Sprintf("seed %d: ExposesWithin(%q, %q)", seed, a, p))
			}
		}
	}
}

func TestIsSubtypeWithin_CountsEachSupertypeAndAliasHop(t *testing.T) {
	t.Parallel()
	doc := irtest.DocOf(
		irtest.ModelOf("a", func(m *ir.Model) { m.Base = &ir.TypeRef{Target: "alias1"}; m.Mixins = irtest.RefsTo("mixin") }),
		irtest.ScalarOf("alias1", "alias2"),
		irtest.ScalarOf("alias2", "b"),
		irtest.ModelOf("b", func(m *ir.Model) { m.Base = &ir.TypeRef{Target: "c"} }),
		irtest.ModelOf("c", nil),
		irtest.ModelOf("mixin", nil),
	)
	for _, tc := range []struct {
		base  ir.TypeID
		sub   bool
		steps int
	}{
		{"b", true, 3},      // a, then its Base read through two aliases, which names b
		{"c", true, 4},      // as above, then b, whose Base names c
		{"mixin", false, 5}, // a mixin is no supertype: a, two alias hops, b, c
	} {
		sub, spent, ok := ir.IsSubtypeWithin(doc, "a", tc.base, math.MaxInt)
		assert.Equal(t, []any{tc.sub, tc.steps, true}, []any{sub, spent, ok}, "a below %s", tc.base)
	}
	_, spent, ok := ir.IsSubtypeWithin(doc, "a", "b", -1)
	assert.Equal(t, []any{0, false}, []any{spent, ok}, "a limit below zero allows no step")
}

func TestExposesWithin_CountsEachSourceAndProperty(t *testing.T) {
	t.Parallel()
	doc := irtest.DocOf(
		irtest.ModelOf("m", func(m *ir.Model) {
			m.Properties = []ir.Property{prop("m.a"), prop("m.b"), prop("m.c")}
			m.Mixins = irtest.RefsTo("viaAlias", "ghost")
		}),
		irtest.ScalarOf("viaAlias", "n"),
		irtest.ModelOf("n", func(m *ir.Model) { m.Properties = []ir.Property{prop("n.p"), prop("n.q")} }),
	)
	for _, tc := range []struct {
		prop    ir.PropID
		exposed bool
		steps   int
	}{
		{"m.a", true, 2},     // m, its first property
		{"n.p", true, 8},     // m and its three properties, ghost, viaAlias, n, n.p
		{"p/none", false, 9}, // every type and property: m, three, ghost, viaAlias, n, two
		{"n.q", true, 9},     // the last property read
	} {
		exposed, spent, ok := ir.ExposesWithin(doc, "m", tc.prop, math.MaxInt)
		assert.Equal(t, []any{tc.exposed, tc.steps, true}, []any{exposed, spent, ok}, "m exposes %s", tc.prop)
	}
	exposed, spent, ok := ir.ExposesWithin(nil, "m", "m.a", 5)
	assert.Equal(t, []any{false, 0, true}, []any{exposed, spent, ok}, "no document exposes nothing, in no steps")
}
