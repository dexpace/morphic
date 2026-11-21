// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package reftable_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/budget"
	"github.com/dexpace/morphic/compilers/openapi/internal/reftable"
)

// TestBind_AStaticDynamicRefAChainPassedInAnotherDocumentIsWordedAsOneAtEveryCut
// pins that an unresolved $dynamicRef that is the $ref it spells, in a document
// a $ref's chain passes into, keeps its keyword wherever the budget cuts the
// reading: the entry the table finishes for a position the chain passed and no
// walk answered is worded $dynamicRef, as the one a walk answers is.
func TestBind_AStaticDynamicRefAChainPassedInAnotherDocumentIsWordedAsOneAtEveryCut(t *testing.T) {
	t.Parallel()
	const src = head + "components:\n  schemas:\n    A: {$ref: 'o.yaml#/S'}\n"
	docs := map[string]string{"o.yaml": "S: {$dynamicRef: '#nope'}\n"}
	full := bound(t, src, withDocuments(docs))
	require.False(t, full.Exhausted())
	seen := 0
	for limit := 1; limit <= full.Steps(); limit++ {
		cfg := withDocuments(docs)
		cfg.Limit = budget.AtMost(limit)
		cut := bound(t, src, cfg)
		docID, ok := cut.Find("o.yaml")
		if !ok {
			continue
		}
		referenceSite, ok := cut.Entry(reftable.DocumentPointer{Doc: docID, Pointer: "/S"})
		if !ok {
			continue
		}
		seen++
		assert.Equal(t, "$dynamicRef", referenceSite.Keyword, "limit %d", limit)
	}
	assert.Positive(t, seen, "some cut holds an entry for it")
	docIDValue, ok := full.Find("o.yaml")
	require.True(t, ok)
	referenceSiteValue, ok := full.Entry(reftable.DocumentPointer{Doc: docIDValue, Pointer: "/S"})
	require.True(t, ok)
	assert.Equal(t, reftable.Unresolved, referenceSiteValue.Hop.Status)
}

// TestRecursiveAnchor_AWalkCutMidwayStopsAtTheBudget pins that the walk over
// the graph of anchored resources charges each edge it follows and stops at
// the budget: a table cut inside the walk has spent one step past its limit,
// never more, however much of the graph is left.
func TestRecursiveAnchor_AWalkCutMidwayStopsAtTheBudget(t *testing.T) {
	t.Parallel()
	spec := anchoredChain(50)
	full := bound(t, spec, reachConfig())
	require.False(t, full.Exhausted())
	for back := 1; back <= 12; back++ {
		cfg := reachConfig()
		cfg.Limit = budget.AtMost(full.Steps() - back)
		cut := bound(t, spec, cfg)
		require.True(t, cut.Exhausted(), "%d steps short", back)
		assert.Equal(t, cfg.Limit.Cap()+1, cut.Steps(), "%d steps short: the walk went on past the budget", back)
	}
}

// TestTable_ANameDeclaredWhereAMergeMayHideItsResourceIsNotKnown pins that a
// $dynamicAnchor name two documents declare, one declaration lying where a
// merge past the bound may hide the schema resource it is in, is not known
// whole in the other document, though no budget is cut, where the same
// declarations placed plainly are.
func TestTable_ANameDeclaredWhereAMergeMayHideItsResourceIsNotKnown(t *testing.T) {
	t.Parallel()
	const src = head + "components:\n  schemas:\n    A: {$dynamicAnchor: n}\n    Use: {$ref: 'other.yaml#/Outer'}\n"
	var hidden strings.Builder
	hidden.WriteString("x-c0: &c0 {description: d}\n")
	for i := 1; i <= 70; i++ {
		fmt.Fprintf(&hidden, "x-c%d: &c%d {<<: *c%d}\n", i, i, i-1)
	}
	hidden.WriteString("Outer: {<<: *c70, properties: {t: {$dynamicAnchor: n}}}\n")
	plain := "Outer: {properties: {t: {$dynamicAnchor: n}}}\n"
	read := func(other string) *reftable.ReferenceTargetTable {
		return bound(t, src, withDocuments(map[string]string{"other.yaml": other}))
	}

	control := read(plain)
	require.False(t, control.Exhausted())
	require.Equal(t, 1, control.DynamicAnchor(reftable.Source, "n").Count)
	assert.True(t, control.DynamicAnchor(reftable.Source, "n").Complete, "declared plainly, the name is known whole")

	table := read(hidden.String())
	require.False(t, table.Exhausted(), "no budget is cut")
	assert.Equal(t, 1, table.DynamicAnchor(reftable.Source, "n").Count)
	assert.False(t, table.DynamicAnchor(reftable.Source, "n").Complete, "the other declaration's resource may be hidden")
}
