// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package reftable_test

import (
	"encoding/json/jsontext"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/budget"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/compilers/openapi/internal/reftable"
	"github.com/dexpace/morphic/internal/ctxtest"
)

// dynamicSpec writes each way a $dynamicRef reads: as the $ref it spells, where
// what it lands on declares no $dynamicAnchor of its fragment's name, and as
// dynamic where it does, or where the resource it names declares the name more
// than once, one of them a $dynamicAnchor, which another resource's
// declaration of the name does not make it.
const dynamicSpec = head + `components:
  schemas:
    P: {$anchor: p, type: string}
    D: {$dynamicAnchor: d, type: string}
    Dup1: {$dynamicAnchor: dup}
    Dup2: {$dynamicAnchor: dup}
    Twin1: {$anchor: twin}
    Twin2: {$anchor: twin}
    Elsewhere: {$id: 'https://example.com/elsewhere', $defs: {t: {$dynamicAnchor: twin}}}
    Mix1: {$anchor: mix}
    Mix2: {$dynamicAnchor: mix}
    Mixed: {$dynamicRef: '#mix'}
    ByAnchor: {$dynamicRef: '#p'}
    ByPointer: {$dynamicRef: '#/components/schemas/P'}
    Dynamic: {$dynamicRef: '#d'}
    Duplicated: {$dynamicRef: '#dup'}
    Twinned: {$dynamicRef: '#twin'}
    Nowhere: {$dynamicRef: '#nope'}
    Null: {$dynamicRef: null}
    Listed: {$dynamicRef: [d]}
    Beside: {$ref: '#/components/schemas/P', $dynamicRef: '#d'}
    BesidePointer: {$ref: '#/components/schemas/P', $dynamicRef: '#/components/schemas/D'}
    ToDynamic: {$ref: '#/components/schemas/Dynamic'}
    ToStatic: {$ref: '#/components/schemas/ByAnchor'}
`

// TestBind_ReadsADynamicRefAsTheRefItSpellsUnlessItNamesADynamicAnchor pins
// which $dynamicRefs the table holds an entry for, under which keyword, and
// how a $ref's chain passes each: through one that is the $ref it spells, and
// ending on a dynamic one.
func TestBind_ReadsADynamicRefAsTheRefItSpellsUnlessItNamesADynamicAnchor(t *testing.T) {
	t.Parallel()
	table := bound(t, dynamicSpec, self)
	documentPointer := documentPointerAt("/components/schemas/P")
	for name, want := range map[string]reftable.Status{
		"ByAnchor": reftable.Resolved, "ByPointer": reftable.Resolved, "Twinned": reftable.Unresolved, "Nowhere": reftable.Unresolved,
	} {
		referenceSite := entryAt(t, table, jsontext.Pointer("/components/schemas/"+name))
		assert.Equal(t, "$dynamicRef", referenceSite.Keyword, name)
		assert.Equal(t, want, referenceSite.Hop.Status, name)
	}
	assert.Equal(t, documentPointer, entryAt(t, table, "/components/schemas/ByAnchor").Hop.Target)
	assert.Equal(t, reftable.DuplicateAnchor, entryAt(t, table, "/components/schemas/Twinned").Hop.Reason)
	for _, name := range []string{"Dynamic", "Duplicated", "Mixed", "Null", "Listed"} {
		_, ok := table.Entry(documentPointerAt(jsontext.Pointer("/components/schemas/" + name)))
		assert.False(t, ok, "%s is no $ref site", name)
	}
	for _, name := range []string{"Beside", "BesidePointer"} {
		referenceSite := entryAt(t, table, jsontext.Pointer("/components/schemas/"+name))
		assert.Equal(t, "$ref", referenceSite.Keyword, "%s: the $ref beside it is the entry", name)
		assert.Equal(t, documentPointer, referenceSite.Hop.Target, name)
	}
	_, ok := (*reftable.ReferenceTargetTable)(nil).Entry(documentPointer)
	assert.False(t, ok, "a nil table read none")

	toDynamic := entryAt(t, table, "/components/schemas/ToDynamic")
	assert.Equal(t, reftable.Chain{End: documentPointerAt("/components/schemas/Dynamic"), Hops: 1, Status: reftable.Resolved}, toDynamic.Chain)
	toStatic := entryAt(t, table, "/components/schemas/ToStatic")
	assert.Equal(t, reftable.Chain{End: documentPointer, Hops: 2, Status: reftable.Resolved}, toStatic.Chain)
}

// TestBind_ReadsADynamicRefIntoAnotherDocumentOnceItIsRead pins a $dynamicRef
// whose hop waits for the document it names: it is read as dynamic or as the
// $ref it spells once that document is, and a $ref onto it ends there or
// passes through it accordingly.
func TestBind_ReadsADynamicRefIntoAnotherDocumentOnceItIsRead(t *testing.T) {
	t.Parallel()
	const src = head + "components:\n  schemas:\n    S: {$ref: '#/components/schemas/A'}\n" +
		"    A: {$dynamicRef: 'o.yaml#n'}\n"
	a := documentPointerAt("/components/schemas/A")

	dynamic := bound(t, src, withDocuments(map[string]string{"o.yaml": "T: {$dynamicAnchor: n}\n"}))
	_, ok := dynamic.Entry(a)
	assert.False(t, ok, "it lands on a $dynamicAnchor of its name")
	assert.Equal(t, reftable.Chain{End: a, Hops: 1, Status: reftable.Resolved}, entryAt(t, dynamic, "/components/schemas/S").Chain)

	static := bound(t, src, withDocuments(map[string]string{"o.yaml": "T: {$anchor: n}\n"}))
	referenceSite := entryAt(t, static, "/components/schemas/A")
	assert.Equal(t, "$dynamicRef", referenceSite.Keyword)
	docID := docNamed(t, static, "o.yaml")
	assert.Equal(t, reftable.DocumentPointer{Doc: docID, Pointer: "/T"}, referenceSite.Hop.Target)
	assert.Equal(t, reftable.Chain{End: reftable.DocumentPointer{Doc: docID, Pointer: "/T"}, Hops: 2, Status: reftable.Resolved},
		entryAt(t, static, "/components/schemas/S").Chain)
}

// TestBind_ReadsADynamicRefAMergeMayHideTheAnchorOfAsDynamic pins that a name
// a merge past the bound may hide the declaration of is not shown to be the
// $ref it spells, so the table holds no entry for it, where a document the
// bound cuts nothing of reads it as one.
func TestBind_ReadsADynamicRefAMergeMayHideTheAnchorOfAsDynamic(t *testing.T) {
	t.Parallel()
	const ref = "    D: {$dynamicRef: '#n'}\n"
	_, ok := bound(t, deepMerge()+ref, self).Entry(documentPointerAt("/components/schemas/D"))
	assert.False(t, ok)
	assert.Equal(t, "$dynamicRef", entryAt(t, bound(t, head+"components:\n  schemas:\n"+ref, self),
		"/components/schemas/D").Keyword)
}

// TestBind_ADynamicRefTheBudgetCutsIsNoOtherEntry pins every cut of
// dynamicSpec's reading: an entry read within the budget is the full one, or
// one the budget stopped, and a $dynamicRef the full reading holds no entry
// for gains none.
func TestBind_ADynamicRefTheBudgetCutsIsNoOtherEntry(t *testing.T) {
	t.Parallel()
	full := bound(t, dynamicSpec, self)
	require.False(t, full.Exhausted())
	want := map[reftable.DocumentPointer]reftable.ReferenceSite{}
	for _, referenceSite := range full.Entries() {
		want[referenceSite.Site] = referenceSite
	}
	for limit := 1; limit < full.Steps(); limit++ {
		cfg := self
		cfg.Limit = budget.AtMost(limit)
		for _, referenceSite := range bound(t, dynamicSpec, cfg).Entries() {
			whole, ok := want[referenceSite.Site]
			require.True(t, ok, "limit %d: %s holds no entry read whole", limit, referenceSite.Site.Pointer)
			if referenceSite.Hop.Status != reftable.OverBudget && referenceSite.Chain.Status != reftable.OverBudget {
				assert.Equal(t, whole, referenceSite, "limit %d", limit)
			}
			assert.True(t, strings.HasPrefix(referenceSite.Keyword, "$"), "limit %d", limit)
		}
	}
}

// TestBind_ADynamicRefIsReadNoFurtherOnceRefused cancels the reading of
// dynamicSpec at every ask of its context, a $dynamicRef among those it reads
// at each: the binding takes no step past what the whole reading spends, and
// reports no budget exhausted, as no budget stopped it.
func TestBind_ADynamicRefIsReadNoFurtherOnceRefused(t *testing.T) {
	t.Parallel()
	full := bound(t, dynamicSpec, self)
	whole := &ctxtest.Countdown{Context: t.Context(), Left: 1 << 30}
	reftable.Bind(whole, openapitest.YAMLDocument(t, dynamicSpec), self)
	for left := range whole.Calls {
		ctx := &ctxtest.Countdown{Context: t.Context(), Left: left}
		table := reftable.Bind(ctx, openapitest.YAMLDocument(t, dynamicSpec), self)
		assert.LessOrEqual(t, table.Steps(), full.Steps(), "ask %d", left+1)
		assert.False(t, table.Exhausted(), "ask %d", left+1)
	}
}
