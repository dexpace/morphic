// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package bind_test

import (
	"encoding/json/jsontext"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/bind"
	"github.com/dexpace/morphic/compilers/openapi/internal/budget"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/internal/ctxtest"
)

// reachDraft is the dialect that has `$recursiveAnchor`.
const reachDraft = "https://json-schema.org/draft/2019-09/schema"

// reachConfig reads reachDraft as the draft with a recursive anchor.
func reachConfig() bind.Config {
	cfg := self
	cfg.Dialect = func(_, dialect string) bind.DialectRule {
		return bind.DialectRule{Recursive: dialect == reachDraft}
	}
	return cfg
}

// anchoredChain is n anchored resources, each `$ref`ing the next and each
// writing a `$recursiveRef`.
func anchoredChain(n int) string {
	var b strings.Builder
	b.WriteString(head + "components:\n  schemas:\n")
	for i := range n {
		fmt.Fprintf(&b, "    R%d: {$schema: '%s', $id: 'https://x.test/r%d', $recursiveAnchor: true, properties: {r: {$recursiveRef: '#'}}", i, reachDraft, i)
		if i+1 < n {
			fmt.Fprintf(&b, ", $ref: 'https://x.test/r%d'", i+1)
		}
		b.WriteString("}\n")
	}
	return b.String()
}

// TestRecursiveAnchor_AResourceAnotherAnchoredOneReachesIsReached pins what the
// graph answers: of a chain of anchored resources each `$ref`ing the next,
// every one but the first is reached by one other than itself, and the first
// by none.
func TestRecursiveAnchor_AResourceAnotherAnchoredOneReachesIsReached(t *testing.T) {
	t.Parallel()
	table := bound(t, anchoredChain(4), reachConfig())
	require.False(t, table.Exhausted())
	for i, wantReached := range []bool{false, true, true, true} {
		declared, reached, known := table.RecursiveAnchor(at(jsontext.Pointer(fmt.Sprintf("/components/schemas/R%d", i))))
		assert.True(t, declared, "R%d", i)
		assert.Equal(t, wantReached, reached, "R%d", i)
		assert.True(t, known, "R%d", i)
	}

	// A discriminator's mapping value is a reference too.
	mapped := bound(t, head+"components:\n  schemas:\n"+
		"    M: {$schema: '"+reachDraft+"', $id: 'https://x.test/m', $recursiveAnchor: true, "+
		"discriminator: {propertyName: k, mapping: {x: 'https://x.test/t'}}}\n"+
		"    T: {$schema: '"+reachDraft+"', $id: 'https://x.test/t', $recursiveAnchor: true}\n", reachConfig())
	for name, want := range map[string]bool{"M": false, "T": true} {
		_, reached, known := mapped.RecursiveAnchor(at(jsontext.Pointer("/components/schemas/" + name)))
		assert.Equal(t, want, reached, name)
		assert.True(t, known, name)
	}

	// Two anchored resources and nothing joining them: neither is reached.
	alone := bound(t, head+"components:\n  schemas:\n"+
		"    A: {$schema: '"+reachDraft+"', $id: 'https://x.test/a', $recursiveAnchor: true}\n"+
		"    B: {$schema: '"+reachDraft+"', $id: 'https://x.test/b', $recursiveAnchor: true}\n", reachConfig())
	for _, name := range []string{"A", "B"} {
		_, reached, known := alone.RecursiveAnchor(at(jsontext.Pointer("/components/schemas/" + name)))
		assert.False(t, reached, name)
		assert.True(t, known, name)
	}
}

// TestBind_TheRecursiveReachGrowsLinearly pins the cost of the walk: n anchored
// resources each reaching the next cost steps growing by about a doubling per
// doubling, not four times.
func TestBind_TheRecursiveReachGrowsLinearly(t *testing.T) {
	t.Parallel()
	const n = 250
	steps := func(size int) int { return bound(t, anchoredChain(size), reachConfig()).Steps() }
	s1, s2, s4 := steps(n), steps(2*n), steps(4*n)
	require.Less(t, s1, s2)
	assert.LessOrEqual(t, float64(s4-s2), 2.2*float64(s2-s1), "steps at n, 2n, 4n: %d, %d, %d", s1, s2, s4)
}

// TestRecursiveAnchor_AWalkTheBudgetCutsKnowsNothing pins every cut of the
// reading of a chain of anchored resources: a table the budget ran out in,
// the walk over its resources included, knows of no verdict, so every
// reference resting on one is kept.
func TestRecursiveAnchor_AWalkTheBudgetCutsKnowsNothing(t *testing.T) {
	t.Parallel()
	spec := anchoredChain(6)
	full := bound(t, spec, reachConfig())
	require.False(t, full.Exhausted())
	for limit := 1; limit < full.Steps(); limit++ {
		cfg := reachConfig()
		cfg.Limit = budget.AtMost(limit)
		cut := bound(t, spec, cfg)
		require.True(t, cut.Exhausted(), "limit %d", limit)
		for i := range 6 {
			_, _, known := cut.RecursiveAnchor(at(jsontext.Pointer(fmt.Sprintf("/components/schemas/R%d", i))))
			assert.False(t, known, "limit %d: R%d", limit, i)
		}
	}
}

// TestRecursiveAnchor_AReferenceTheMergeBoundHidesTheResourceOfKnowsNothing
// pins that a reference site whose resource a merge past the bound may hide,
// in another document, leaves the walk short of an edge, so no verdict it
// gives is known, though neither the source nor the budget is cut.
func TestRecursiveAnchor_AReferenceTheMergeBoundHidesTheResourceOfKnowsNothing(t *testing.T) {
	t.Parallel()
	var other strings.Builder
	other.WriteString("x-c0: &c0 {description: d}\n")
	for i := 1; i <= 70; i++ {
		fmt.Fprintf(&other, "x-c%d: &c%d {<<: *c%d}\n", i, i, i-1)
	}
	other.WriteString("Outer: {<<: *c70, properties: {t: {$ref: 'https://x.test/r0'}}}\n")
	cfg := withDocuments(map[string]string{"other.yaml": other.String()})
	cfg.Dialect = reachConfig().Dialect
	table := bound(t, anchoredChain(1)+"    Use: {$ref: 'other.yaml#/Outer'}\n", cfg)
	require.False(t, table.Exhausted())
	declared, _, known := table.RecursiveAnchor(at("/components/schemas/R0"))
	assert.True(t, declared)
	assert.False(t, known, "Outer's resource may be an anchored one the bound hides")
}

// TestRecursiveAnchor_TheWalkAsksNoMoreOnceRefused cancels the reading of a
// chain of anchored resources at every ask of its context, and holds what it
// asks after the refusal to asksAfterRefusal and the rest of the one walk to
// a position it may be making, a step of each of its pointer's tokens and its
// root's: no loop over the resources or the sites goes on.
func TestRecursiveAnchor_TheWalkAsksNoMoreOnceRefused(t *testing.T) {
	t.Parallel()
	names, other := dynamicNames(4)
	// The walk alone: the names are declared and no $dynamicRef names one.
	low, _, _ := strings.Cut(other, "properties:")
	_, defs, _ := strings.Cut(other, "$defs:")
	other = low + "$defs:" + defs
	for name, c := range map[string]struct {
		spec string
		cfg  bind.Config
	}{
		"anchored resources":          {spec: anchoredChain(6), cfg: reachConfig()},
		"names two documents declare": {spec: names, cfg: withDocuments(map[string]string{"other.yaml": other})},
	} {
		whole := &ctxtest.Countdown{Context: t.Context(), Left: 1 << 30}
		bind.Bind(whole, openapitest.YAMLDocument(t, c.spec), c.cfg)
		for left := range whole.Calls {
			ctx := &ctxtest.Countdown{Context: t.Context(), Left: left}
			bind.Bind(ctx, openapitest.YAMLDocument(t, c.spec), c.cfg)
			require.LessOrEqual(t, ctx.Calls, left+1+asksAfterRefusal+onePlacement,
				"%s, ask %d: asked on after the refusal", name, left+1)
		}
	}
}

// onePlacement is the most steps one walk to a position of anchoredChain or
// dynamicNames takes: its document's root and each token of its pointer, as
// /components/schemas/Rn/properties/r.
const onePlacement = 6

// dynamicNames is two documents declaring n names each once, the first document's resource `$ref`ing the second's, and the
// second writing a `$dynamicRef` to each name.
func dynamicNames(n int) (spec string, other string) {
	var top, low, refs strings.Builder
	for i := range n {
		fmt.Fprintf(&top, "n%d: {$dynamicAnchor: n%d}, ", i, i)
		fmt.Fprintf(&low, "n%d: {$dynamicAnchor: n%d}, ", i, i)
		fmt.Fprintf(&refs, "p%d: {$dynamicRef: '#n%d'}, ", i, i)
	}
	spec = head + "components:\n  schemas:\n    Top: {$ref: 'other.yaml#/Low', $defs: {" +
		strings.TrimSuffix(top.String(), ", ") + "}}\n"
	other = "Low: {$id: 'https://x.test/low', properties: {" + strings.TrimSuffix(refs.String(), ", ") +
		"}, $defs: {" + strings.TrimSuffix(low.String(), ", ") + "}}\n"
	return spec, other
}

// TestTable_ANameSeveralDocumentsDeclareIsWalkedOncePerName pins the stated
// shape: each name declared in more than one document costs one walk over
// the graph, so steps grow no more than four times a doubling of the names,
// and each name's anchor in the second document is reached by a resource of
// the first.
func TestTable_ANameSeveralDocumentsDeclareIsWalkedOncePerName(t *testing.T) {
	t.Parallel()
	steps := func(n int) (*bind.Table, int) {
		spec, other := dynamicNames(n)
		table := bound(t, spec, withDocuments(map[string]string{"other.yaml": other}))
		require.False(t, table.Exhausted(), "n=%d", n)
		return table, table.Steps()
	}
	_, s100 := steps(100)
	_, s200 := steps(200)
	table, s400 := steps(400)
	require.Less(t, s100, s200)
	assert.LessOrEqual(t, float64(s400-s200), 4.4*float64(s200-s100), "steps at 100, 200, 400: %d, %d, %d", s100, s200, s400)

	other := docNamed(t, table, "other.yaml")
	for _, name := range []string{"n0", "n399"} {
		anchor := table.DynamicAnchor(other, name)
		assert.Equal(t, 1, anchor.Count, name)
		assert.True(t, anchor.Reached, "a resource of the first document declares %s and reaches this one", name)
		assert.True(t, anchor.Complete, name)
	}
}

// TestTable_ANameTheBudgetCutsTheWalkOfIsNotKnown pins every cut of the
// reading of names two documents declare: a table the budget ran out in, the
// walks over its resources included, knows no name's declarations whole.
func TestTable_ANameTheBudgetCutsTheWalkOfIsNotKnown(t *testing.T) {
	t.Parallel()
	spec, other := dynamicNames(3)
	cfg := withDocuments(map[string]string{"other.yaml": other})
	full := bound(t, spec, cfg)
	require.False(t, full.Exhausted())
	for limit := 1; limit < full.Steps(); limit++ {
		cut := cfg
		cut.Limit = budget.AtMost(limit)
		table := bound(t, spec, cut)
		require.True(t, table.Exhausted(), "limit %d", limit)
		for id := range bind.DocID(table.Documents()) {
			for _, name := range []string{"n0", "n1", "n2"} {
				assert.False(t, table.DynamicAnchor(id, name).Complete, "limit %d: %d %s", limit, id, name)
			}
		}
	}
}

// TestTable_AccessingADynamicAnchorAllocatesNothing pins that a name's
// declarations are sorted once when the table is built, not on each call
// (the declarations are the same name written 1,000 times, as a document
// reading each reference asks for it 1,000 times).
func TestTable_AccessingADynamicAnchorAllocatesNothing(t *testing.T) {
	for _, n := range []int{500, 1000, 2000} {
		var b strings.Builder
		b.WriteString(head + "components:\n  schemas:\n")
		for i := range n {
			fmt.Fprintf(&b, "    C%d: {$dynamicAnchor: n, type: object, properties: {k: {$dynamicRef: '#n'}}}\n", i)
		}
		table := bound(t, b.String(), self)
		require.False(t, table.Exhausted(), "n=%d", n)

		anchor := table.DynamicAnchor(bind.Source, "n")
		assert.Equal(t, n, anchor.Count, "every declaration is counted")
		assert.Equal(t, bind.Position{Pointer: "/components/schemas/C0"}, anchor.Site, "the first, in pointer order, is the site")
		assert.Zero(t, testing.AllocsPerRun(20, func() { _ = table.DynamicAnchor(bind.Source, "n") }), "n=%d", n)
		assert.Equal(t, 0, table.DynamicAnchor(bind.Source, "absent").Count)
	}
}

// TestBind_AReferenceToADynamicRefThatReadsAsARefEndsWhereItEnds pins what a
// $dynamicRef naming no dynamic anchor is in the table: an entry as a $ref
// has, tagged with the keyword it was written with, whose chain a $ref onto
// it passes through.
func TestBind_AReferenceToADynamicRefThatReadsAsARefEndsWhereItEnds(t *testing.T) {
	t.Parallel()
	table := bound(t, head+"components:\n  schemas:\n"+
		"    Pet: {$anchor: pet, type: object}\n"+
		"    ByPointer: {$dynamicRef: '#/components/schemas/Pet'}\n"+
		"    ByAnchor: {$dynamicRef: '#pet'}\n"+
		"    ByRef: {$ref: '#pet'}\n"+
		"    S: {$ref: '#/components/schemas/ByPointer'}\n", self)
	pet := at("/components/schemas/Pet")

	for site, keyword := range map[string]string{"ByPointer": "$dynamicRef", "ByAnchor": "$dynamicRef", "ByRef": "$ref"} {
		e := entryAt(t, table, jsontext.Pointer("/components/schemas/"+site))
		assert.Equal(t, keyword, e.Keyword, site)
		assert.Equal(t, bind.Resolved, e.Chain.Status, site)
		assert.Equal(t, pet, e.Chain.End, site)
	}
	s := entryAt(t, table, "/components/schemas/S")
	assert.Equal(t, "$ref", s.Keyword)
	assert.Equal(t, pet, s.Chain.End, "the chain of a $ref onto the static $dynamicRef runs through it")
	assert.Equal(t, 2, s.Chain.Hops)

	// A truly dynamic reference is no entry.
	dyn := bound(t, head+"components:\n  schemas:\n    Pet: {$dynamicAnchor: pet}\n    D: {$dynamicRef: '#pet'}\n", self)
	_, ok := dyn.Entry(at("/components/schemas/D"))
	assert.False(t, ok, "a reference naming a dynamic anchor is dynamic")
}
