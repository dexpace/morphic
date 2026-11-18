// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package harness

import (
	"context"
	"encoding/json/jsontext"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
)

// TestSequenceOrderInvariant_ATieIsHeldToTheNodeItsReportNames plants a
// report that moves with the order and names, in its node, the property it
// decided: the oracle forgives any difference in that property and finds one
// in another member of the model.
func TestSequenceOrderInvariant_ATieIsHeldToTheNodeItsReportNames(t *testing.T) {
	const tie = "/components/schemas/AB/allOf/1/properties/p"
	type change func(doc *ir.Document)
	retype := func(doc *ir.Document) { property(t, doc, "p").Type.Target = "t/prim/integer" }
	exemplify := func(doc *ir.Document) {
		property(t, doc, "p").Examples = []ir.Example{{Value: &ir.Value{Kind: ir.ValueString, Str: "x"}}}
	}
	other := func(doc *ir.Document) { property(t, doc, "alpha").Docs.Description = "reversed" }
	tests := []struct {
		name   string
		change change
		found  bool
	}{
		{name: "a difference in the type of the property it names is forgiven", change: retype},
		{name: "a difference in any other fact of that property is forgiven", change: exemplify},
		{name: "a difference in another member is found", change: other, found: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			plant(t, func(t *testing.T, reversed bool, doc *ir.Document, diags []ir.Diagnostic) (*ir.Document, []ir.Diagnostic, error) {
				p := property(t, doc, "p")
				diags = append(slices.Clip(diags), diagnostic(ir.SeverityWarning, "x/tie", tie, string(p.ID)))
				if reversed {
					tc.change(doc)
				}
				return doc, diags, nil
			})

			got := sequenceOrderInvariant(context.Background(), "spec.yaml", []byte(fixtureY), true)

			if !tc.found {
				assert.Equal(t, passed(OracleSequenceOrder), got)
				return
			}
			require.True(t, got.Found(), "the oracle must find a difference the report does not name: %+v", got)
			assert.Contains(t, got.Detail, "document depends on sequence order")
		})
	}
}

// TestSequenceOrderInvariant_AReportNamingItsNodePairsAtAnyDepth plants a
// report that moves from an entry to the model holding it, as one moves
// between an entry of a nested allOf and an outer one: naming the node it
// decided, it is a tie whatever its depth, and naming none, a difference.
func TestSequenceOrderInvariant_AReportNamingItsNodePairsAtAnyDepth(t *testing.T) {
	tests := []struct {
		name  string
		named bool
		found bool
	}{
		{name: "a report naming its node pairs", named: true},
		{name: "a report naming no node does not", found: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			plant(t, func(t *testing.T, reversed bool, doc *ir.Document, diags []ir.Diagnostic) (*ir.Document, []ir.Diagnostic, error) {
				at, node := "/components/schemas/AB/allOf/1/properties/p", ""
				if reversed {
					at = "/components/schemas/AB"
				}
				if tc.named {
					node = string(property(t, doc, "p").ID)
				}
				return doc, append(slices.Clip(diags), diagnostic(ir.SeverityWarning, "x/tie", at, node)), nil
			})

			got := sequenceOrderInvariant(context.Background(), "spec.yaml", []byte(fixtureY), true)

			if !tc.found {
				assert.Equal(t, passed(OracleSequenceOrder), got)
				return
			}
			require.True(t, got.Found(), "a report naming no node must pair by its pointer: %+v", got)
			assert.Contains(t, got.Detail, "diagnostics depend on sequence order")
		})
	}
}

// settledDoc is a model composing AB and declaring p over AB's, a model
// declaring q apart, and a union whose discriminator over p was inferred,
// and another's declared.
func settledDoc() *ir.Document {
	member := func(id ir.PropID, wire, at string) ir.Property {
		return ir.Property{ID: id, WireName: wire, Type: ir.TypeRef{Target: "t/prim/string"},
			Provenance: ir.Provenance{Pointer: jsontext.Pointer(at)}}
	}
	discriminated := func(id ir.TypeID, inferred bool) *ir.Union {
		return &ir.Union{ID: id, Variants: []ir.Variant{{Type: ir.TypeRef{Target: "t/M"}}},
			Discriminator: &ir.Discriminator{PropertyName: "p", Inferred: inferred}}
	}
	return &ir.Document{Types: ir.TypeRegistry{
		"t/AB":       &ir.Model{ID: "t/AB", Properties: []ir.Property{member("p/AB/p", "p", "/AB/p")}},
		"t/M":        &ir.Model{ID: "t/M", Base: &ir.TypeRef{Target: "t/AB"}, Properties: []ir.Property{member("p/M/p", "p", "/M/allOf/properties/p")}},
		"t/Q":        &ir.Model{ID: "t/Q", Properties: []ir.Property{member("p/Q/q", "q", "/Q/q")}},
		"t/inferred": discriminated("t/inferred", true),
		"t/declared": discriminated("t/declared", false),
		"t/apart": &ir.Union{ID: "t/apart", Variants: []ir.Variant{{Type: ir.TypeRef{Target: "t/Q"}}},
			Discriminator: &ir.Discriminator{PropertyName: "q", Inferred: true}},
	}}
}

// TestSettledFrom_TiesWhatATiedMemberDecides pins the closure of a tie: a
// member whose flattening overrides a tied one is tied, at its pointer too,
// and so is a union whose discriminator was inferred from it, and the model
// holding it holds a tied member; a declared
// discriminator, one inferred from a member apart, a member apart, and a
// document with no tie add nothing.
func TestSettledFrom_TiesWhatATiedMemberDecides(t *testing.T) {
	t.Parallel()
	doc := settledDoc()

	got := settledFrom(tieSites{nodes: []string{"p/AB/p"}}, doc, nil)

	assert.Equal(t, []string{"p/AB/p", "p/M/p", "t/inferred"}, got.nodes)
	assert.Contains(t, got.pointers, "/M/allOf/properties/p", "where the tied member's fold mints")
	assert.Equal(t, []string{"t/AB", "t/M"}, got.holders, "the models holding a tied member")
	bySite := settledFrom(tieSites{pointers: []string{"/AB"}}, doc, nil)
	assert.Equal(t, []string{"p/AB/p", "p/M/p", "t/inferred"}, bySite.nodes,
		"a member declared beneath a tie site is tied, as a fold of inline declarations reports where they are")
	assert.Equal(t, tieSites{pointers: []string{"/x"}}, settledFrom(tieSites{pointers: []string{"/x"}}, doc, nil),
		"a site no member is declared at ties none")
	assert.Equal(t, tieSites{nodes: []string{"p/AB/p"}}, settledFrom(tieSites{nodes: []string{"p/AB/p"}}, nil, nil),
		"a refused compile adds none")
}

// TestCanonicalForm_LeavesOutANodeATieMints pins a node a registry keys by its
// ID beneath a tie site: whether a fold mints it follows the tie, so a document
// holding it reads as one that does not, and without the tie it differs.
func TestCanonicalForm_LeavesOutANodeATieMints(t *testing.T) {
	t.Parallel()
	minted := &ir.Model{ID: "t/composed/M/allOf/properties/p", Provenance: ir.Provenance{Pointer: "/M/allOf/properties/p"}}
	with := settledDoc()
	with.Types[minted.ID] = minted
	ties := tieSites{pointers: []string{"/M/allOf/properties/p"}}

	a, err := (&formReader{doc: with}).canonicalForm(ties)
	require.NoError(t, err)
	b, err := (&formReader{doc: settledDoc()}).canonicalForm(ties)
	require.NoError(t, err)
	assert.Equal(t, b.document, a.document)
	assert.Equal(t, b.shape, a.shape)

	untied, err := (&formReader{doc: with}).canonicalForm(tieSites{})
	require.NoError(t, err)
	assert.NotEqual(t, b.document, untied.document, "the node is compared where no tie decides it")
}

// TestCanonicalForm_ForgivesANodeATieNamesWhereverItSits pins a node a moved
// report names, away from any tie site: a document whose member differs there
// reads as one that does not, in its own form and in its flattened shape, and
// one differing in a member no tie names does not.
func TestCanonicalForm_ForgivesANodeATieNamesWhereverItSits(t *testing.T) {
	t.Parallel()
	retyped := settledDoc()
	retyped.Types["t/M"].(*ir.Model).Properties[0].Type.Target = "t/prim/integer"
	ties := tieSites{nodes: []string{"p/M/p"}}

	a, err := (&formReader{doc: settledDoc()}).canonicalForm(ties)
	require.NoError(t, err)
	b, err := (&formReader{doc: retyped}).canonicalForm(ties)
	require.NoError(t, err)
	assert.Equal(t, a.document, b.document)
	assert.Equal(t, a.shape, b.shape)

	c, err := (&formReader{doc: retyped}).canonicalForm(tieSites{nodes: []string{"p/Q/q"}})
	require.NoError(t, err)
	assert.NotEqual(t, a.shape, c.shape, "a member no tie names is compared")
}

// TestUntied_LeavesOutWhatATieDecided pins which reports one order alone makes
// are no difference: one naming a tied node or a model holding one, or one at
// or beneath a tie site, says what a tie decided; any other is a difference.
func TestUntied_LeavesOutWhatATieDecided(t *testing.T) {
	t.Parallel()
	ties := tieSites{pointers: []string{"/M/allOf/properties/p"}, nodes: []string{"p/M/p"}, holders: []string{"t/M"}}
	rs := []report{
		{key: "tied node", node: "p/M/p", pointer: "/elsewhere"},
		{key: "holder", node: "t/M", pointer: "/elsewhere"},
		{key: "at a site", pointer: "/M/allOf/properties/p/items"},
		{key: "another node", node: "t/Q", pointer: "/Q"},
		{key: "beside a site", pointer: "/M/allOf/properties/pq"},
	}

	got := untied(rs, ties, nil)

	keys := make([]string, 0, len(got))
	for _, r := range got {
		keys = append(keys, r.key)
	}
	assert.Equal(t, []string{"another node", "beside a site"}, keys)
}

// fixtureMinted: both branches declare r as an object of their own, each with a
// member of its own beside two whose names share a neutral form, so the fold
// mints a model composing both at the branch written later, and pass.Validate
// reports the collision on each.
var fixtureMinted = schemas("AB: {allOf: [{type: object, properties: {r: {type: object, " +
	"properties: {'+1': {type: integer}, '-1': {type: integer}, alpha: {type: string}}}}}, " +
	"{type: object, properties: {r: {type: object, properties: {'+1': {type: integer}, '-1': {type: integer}, " +
	"beta: {type: integer}}}}}]}")

// TestSequenceOrderInvariant_AReportOnANodeAReversalMovesPairsByWhatItHolds
// pins how a report naming a node is compared: a node a fold mints at the
// branch written later carries that branch's index in either order, so a
// report on it pairs with its counterpart by what each node holds, as the
// document's forms tell such IDs apart, and one moved to a node holding
// something else does not.
func TestSequenceOrderInvariant_AReportOnANodeAReversalMovesPairsByWhatItHolds(t *testing.T) {
	t.Run("the real compiler's reports on the minted node pair", func(t *testing.T) {
		got := sequenceOrderInvariant(context.Background(), "spec.yaml", []byte(fixtureMinted), true)

		assert.Equal(t, passed(OracleSequenceOrder), got)
	})
	t.Run("a report moved to another node is found", func(t *testing.T) {
		plant(t, func(t *testing.T, reversed bool, doc *ir.Document, diags []ir.Diagnostic) (*ir.Document, []ir.Diagnostic, error) {
			// Each compile names the node in its own coordinates: alpha's branch
			// is first as written, beta's first reversed.
			const node = "t/anon/components/schemas/AB/allOf/0/properties/r"
			return doc, append(slices.Clip(diags), diagnostic(ir.SeverityWarning, "x/node", "", node)), nil
		})

		got := sequenceOrderInvariant(context.Background(), "spec.yaml", []byte(fixtureMinted), true)

		require.True(t, got.Found(), "the report names alpha's model as written and beta's reversed: %+v", got)
		assert.Contains(t, got.Detail, "x/node")
	})
}

// TestNodeKey_PairsOnlyANodeItsFormTellsByWhatItHolds pins what a report's
// node key is: none for a report naming no node, none where the form cannot
// tell the node by what it holds, so the report pairs with nothing and stays
// a difference, and otherwise the node as the form writes it, with no pointer.
func TestNodeKey_PairsOnlyANodeItsFormTellsByWhatItHolds(t *testing.T) {
	t.Parallel()
	named := report{raw: diagnostic(ir.SeverityWarning, "x/node", "/a/allOf/0", "t/anon/a/allOf/0")}
	tells := func(id string) (string, bool) { return id + "#held", true }
	fails := func(id string) (string, bool) { return id, false }

	assert.Empty(t, nodeKey(report{raw: diagnostic(ir.SeverityWarning, "x/node", "/a", "")}, tells), "no node")
	assert.Empty(t, nodeKey(named, fails), "a node no form tells")
	want := named.raw
	want.Provenance = ir.Provenance{Node: "t/anon/a/allOf/0#held"}
	assert.Equal(t, diagnosticKey(want), nodeKey(named, tells))
}

// TestSequenceOrderInvariant_ReadsTieClassesOneWayOnEveryRun plants reports
// at alpha's and beta's p, one class of the tie fixtureSwap's two models
// compose, and one at AB, of no class: the oracle finds the last alone, on
// every run.
func TestSequenceOrderInvariant_ReadsTieClassesOneWayOnEveryRun(t *testing.T) {
	plant(t, func(_ *testing.T, reversed bool, doc *ir.Document, diags []ir.Diagnostic) (*ir.Document, []ir.Diagnostic, error) {
		planted := []ir.Diagnostic{diagnostic(ir.SeverityWarning, "x/swap", "/components/schemas/alpha/properties/p", "")}
		if reversed {
			planted = []ir.Diagnostic{diagnostic(ir.SeverityWarning, "x/swap", "/components/schemas/beta/properties/p", ""),
				diagnostic(ir.SeverityWarning, "x/swap", "/components/schemas/AB", "")}
		}
		return doc, append(slices.Clip(diags), planted...), nil
	})

	first := sequenceOrderInvariant(context.Background(), "spec.yaml", []byte(fixtureSwap), true)
	require.True(t, first.Found(), "the report at AB must stay unpaired: %+v", first)
	assert.Contains(t, first.Detail, `x/swap\x00/components/schemas/AB\x00`)
	assert.NotContains(t, first.Detail, "properties/p", "the reports at p's declarations are one class's")
	for range 19 {
		assert.Equal(t, first, sequenceOrderInvariant(context.Background(), "spec.yaml", []byte(fixtureSwap), true))
	}
}
