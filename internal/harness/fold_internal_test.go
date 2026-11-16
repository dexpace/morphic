// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package harness

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"reflect"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
)

// The IDs the fold's tests name: a model, a decorator of it, a second
// decorator and the type holding the use.
const (
	objID    ir.TypeID = "t/x/Obj"
	decID    ir.TypeID = "t/x/Use/dec"
	outerID  ir.TypeID = "t/x/Use/outer"
	holderID ir.TypeID = "t/x/Use"
)

// annotated is a TypeCommon of id setting every annotation a decorator may
// set, described as said.
func annotated(id ir.TypeID, said string) ir.TypeCommon {
	return ir.TypeCommon{ID: id, Anonymous: true, Docs: ir.Docs{Description: said},
		Deprecation: &ir.Deprecation{Message: said}, Examples: []ir.Example{{Name: said}},
		Availability: &ir.Availability{Added: []string{said}},
		Unmodeled:    ir.Unmodeled{"openapi:x": {Reason: ir.ReasonNoIRHome, Value: ir.RawValue(`"` + said + `"`)}}}
}

// listOf is a document whose list holds a use of Obj through decorator, or of
// Obj itself when decorator is nil.
func listOf(decorator ir.TypeDef) *ir.Document {
	types := ir.TypeRegistry{
		objID:    &ir.Model{ID: objID},
		holderID: &ir.List{ID: holderID, Elem: ir.TypeRef{Target: objID}},
	}
	if decorator != nil {
		types[decID] = decorator
		types[holderID].(*ir.List).Elem.Target = decID
	}
	return &ir.Document{Types: types}
}

// foldedText is doc encoded as the spelling oracle encodes it.
func foldedText(t *testing.T, doc *ir.Document) string {
	t.Helper()
	own, more := folded(doc)
	b, err := encodeComparable(own, sortedArray, more, jsontext.WithIndent("  "))
	require.NoError(t, err)
	return string(b)
}

func TestFolded_AScalarAndAModelDecoratorAreOneUse(t *testing.T) {
	base := &ir.TypeRef{Target: objID}
	scalar := listOf(&ir.Scalar{TypeCommon: annotated(decID, "d"), Base: base})
	model := listOf(&ir.Model{TypeCommon: annotated(decID, "d"), Base: base})

	assert.Empty(t, cmp.Diff(foldedText(t, scalar), foldedText(t, model)))
	assert.NotContains(t, foldedText(t, model), `"kind": "model",`+"\n      \"id\": \"t/x/Use/dec\"",
		"the decorator is left out of Types")
	assert.Contains(t, foldedText(t, model), `"use": {`, "the use writes what the decorator held")

	other := listOf(&ir.Model{TypeCommon: annotated(decID, "e"), Base: base})
	assert.NotEqual(t, foldedText(t, model), foldedText(t, other), "a use holding another description differs")
	assert.NotEqual(t, foldedText(t, model), foldedText(t, listOf(nil)), "a use holds its annotations")
}

// carrying returns a document whose carrier of kind names Obj and holds what
// annotated(_, "d") holds, through a decorator when decorated.
func carrying(kind string, decorated bool) *ir.Document {
	notes := annotated(decID, "d")
	ref := ir.TypeRef{Target: objID}
	var p ir.Property
	if decorated {
		ref.Target = decID
	} else {
		p = ir.Property{Docs: notes.Docs, Deprecation: notes.Deprecation, Examples: notes.Examples,
			Availability: notes.Availability, Unmodeled: notes.Unmodeled}
	}
	p.ID, p.Type = "p/x/Use/p", ref
	doc := listOf(nil)
	if decorated {
		doc.Types[decID] = &ir.Scalar{TypeCommon: notes, Base: &ir.TypeRef{Target: objID}}
	}
	switch kind {
	case "property":
		doc.Types[holderID] = &ir.Model{ID: holderID, Properties: []ir.Property{p}}
	case "header":
		doc.Services = []ir.Service{{ID: "s/x", Groups: []ir.OperationGroup{{ID: "g/x", Operations: []ir.Operation{
			{ID: "op/x", Responses: []ir.Response{{Headers: []ir.Header{{Property: p}}}}},
		}}}}}
	case "parameter":
		doc.Services = []ir.Service{{ID: "s/x", Groups: []ir.OperationGroup{{ID: "g/x", Operations: []ir.Operation{
			{ID: "op/x", Params: []ir.Parameter{{ID: "param/x", Type: p.Type, Docs: p.Docs, Deprecation: p.Deprecation,
				Examples: p.Examples, Availability: p.Availability, Unmodeled: p.Unmodeled}}},
		}}}}}
	default:
		doc.Types[holderID] = &ir.Union{ID: holderID, Variants: []ir.Variant{{
			Type: p.Type, Docs: p.Docs, Deprecation: p.Deprecation, Examples: p.Examples, Availability: p.Availability,
			Unmodeled: p.Unmodeled,
		}}}
	}
	return doc
}

// TestFolded_ACarrierHoldsWhatItsDecoratorHeld pins each carrier: one naming
// a decorator encodes as one naming its target and holding its annotations.
func TestFolded_ACarrierHoldsWhatItsDecoratorHeld(t *testing.T) {
	for _, kind := range []string{"property", "header", "parameter", "variant"} {
		t.Run(kind, func(t *testing.T) {
			got := foldedText(t, carrying(kind, true))
			assert.Empty(t, cmp.Diff(foldedText(t, carrying(kind, false)), got))
			assert.NotContains(t, got, string(decID))
		})
	}
}

// TestFolded_TheCarriersOwnComeFirst pins the merge onto a carrier: a field it
// sets is its own, one it leaves empty the decorator's, a description and its
// references as one, and an unmodeled name both hold kept as a list.
func TestFolded_TheCarriersOwnComeFirst(t *testing.T) {
	doc := carrying("property", true)
	p := &doc.Types[holderID].(*ir.Model).Properties[0]
	p.Docs = ir.Docs{Summary: "own", Refs: []ir.DocRef{{Type: objID}}}
	p.Examples = []ir.Example{{Name: "own"}}
	p.Unmodeled = ir.Unmodeled{"openapi:x": {Reason: ir.ReasonOutOfScope, Value: ir.RawValue(`1`)}}
	dec := doc.Types[decID].Common()
	dec.Docs.Summary, dec.Docs.ExternalDocs = "theirs", []ir.Link{{URL: "https://example.com"}}

	var got struct {
		Properties []struct {
			Docs      ir.Docs                        `json:"docs"`
			Examples  []ir.Example                   `json:"examples"`
			Unmodeled map[string][]ir.UnmodeledEntry `json:"unmodeled"`
		} `json:"properties"`
	}
	own, more := folded(doc)
	b, err := json.Marshal(own.Types[holderID], json.WithMarshalers(more))
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(b, &got))

	require.Len(t, got.Properties, 1)
	assert.Equal(t, ir.Docs{Summary: "own", Refs: []ir.DocRef{{Type: objID}},
		ExternalDocs: []ir.Link{{URL: "https://example.com"}}}, got.Properties[0].Docs)
	assert.Equal(t, []ir.Example{{Name: "own"}}, got.Properties[0].Examples)
	assert.Len(t, got.Properties[0].Unmodeled["openapi:x"], 2, "a repeated name keeps both entries")
}

// TestMergeDocs_TakesEveryField holds mergeDocs to ir.Docs' fields: each is
// taken from the second where the first leaves it empty.
func TestMergeDocs_TakesEveryField(t *testing.T) {
	fields := reflect.VisibleFields(reflect.TypeFor[ir.Docs]())
	names := make([]string, 0, len(fields))
	for _, f := range fields {
		names = append(names, f.Name)
	}
	assert.Equal(t, []string{"Summary", "Description", "Refs", "ExternalDocs"}, names, "mergeDocs merges these")

	full := ir.Docs{Summary: "s", Description: "d", Refs: []ir.DocRef{{Type: objID}},
		ExternalDocs: []ir.Link{{URL: "u"}}}
	assert.Equal(t, full, mergeDocs(ir.Docs{}, full))
	assert.Equal(t, ir.Docs{Summary: "s", Refs: []ir.DocRef{{Type: objID}}, ExternalDocs: []ir.Link{{URL: "u"}}},
		mergeDocs(ir.Docs{Refs: []ir.DocRef{{Type: objID}}}, full), "references hold their description's place")
}

// TestDecoratorUses_FollowsAChainAndLeavesACycle pins the walk: a chain of two
// collapses to its end, its annotations nearest first; a chain returning to a
// decorator collapses nowhere; a dangling base ends a chain.
func TestDecoratorUses_FollowsAChainAndLeavesACycle(t *testing.T) {
	outer := annotated(outerID, "outer")
	outer.Docs.Summary = ""
	inner := annotated(decID, "inner")
	inner.Docs.Summary = "inner summary"
	types := ir.TypeRegistry{
		objID:   &ir.Model{ID: objID},
		outerID: &ir.Scalar{TypeCommon: outer, Base: &ir.TypeRef{Target: decID}},
		decID:   &ir.Model{TypeCommon: inner, Base: &ir.TypeRef{Target: objID}},
		"t/x/a": &ir.Scalar{ID: "t/x/a", Anonymous: true, Base: &ir.TypeRef{Target: "t/x/b"}},
		"t/x/b": &ir.Scalar{ID: "t/x/b", Anonymous: true, Base: &ir.TypeRef{Target: "t/x/a"}},
		"t/x/d": &ir.Scalar{ID: "t/x/d", Anonymous: true, Base: &ir.TypeRef{Target: "t/x/gone"}},
	}

	uses := decoratorUses(types)

	assert.Len(t, uses, 3, "the cycle's decorators are left out")
	assert.Equal(t, objID, uses[outerID].target)
	assert.Equal(t, ir.Docs{Summary: "inner summary", Description: "outer"}, uses[outerID].notes.docs)
	assert.Equal(t, "outer", uses[outerID].notes.deprecation.Message)
	assert.Equal(t, []ir.Example{{Name: "outer"}}, uses[outerID].notes.examples)
	assert.Len(t, uses[outerID].notes.unmodeled["openapi:x"], 2)
	assert.Equal(t, ir.TypeID("t/x/gone"), uses["t/x/d"].target)

	_, more := folded(&ir.Document{Types: types})
	b, err := json.Marshal(types["t/x/a"], json.WithMarshalers(more))
	require.NoError(t, err)
	assert.Contains(t, string(b), `"target":"t/x/b"`, "a cycle's reference is written as it stands")
}

// TestFolded_AnIDNamingADecoratorNamesWhatItStandsFor pins the TypeID
// marshaler: a discriminator's mapping naming a decorator names its target.
func TestFolded_AnIDNamingADecoratorNamesWhatItStandsFor(t *testing.T) {
	doc := listOf(&ir.Scalar{TypeCommon: annotated(decID, "d"), Base: &ir.TypeRef{Target: objID}})
	doc.Types["t/x/U"] = &ir.Union{ID: "t/x/U",
		Discriminator: &ir.Discriminator{Mapping: map[string]ir.TypeID{"a": decID}}}

	assert.Contains(t, foldedText(t, doc), `"a": "t/x/Obj"`)
	assert.NotContains(t, foldedText(t, doc), string(decID))
}

func TestFolded_ADocumentOfNoDecoratorIsItself(t *testing.T) {
	doc := listOf(nil)
	own, more := folded(doc)
	assert.Same(t, doc, own)
	assert.Nil(t, more)
}

// TestFolded_WhatNamesNoDecoratorIsWrittenAsItStands pins the fold's reach: a
// carrier naming no decorator is written as it stands beside one that does,
// and a use of a decorator holding nothing is a bare reference.
func TestFolded_WhatNamesNoDecoratorIsWrittenAsItStands(t *testing.T) {
	plain := carrying("property", false)
	holder := func(text string) any {
		var doc struct {
			Types map[string]any `json:"types"`
		}
		require.NoError(t, json.Unmarshal([]byte(text), &doc))
		return doc.Types[string(holderID)]
	}
	want := holder(foldedText(t, plain))
	plain.Types["t/x/L"] = &ir.List{ID: "t/x/L", Elem: ir.TypeRef{Target: decID}}
	plain.Types[decID] = &ir.Scalar{ID: decID, Anonymous: true, Base: &ir.TypeRef{Target: objID}}

	got := foldedText(t, plain)

	assert.Empty(t, cmp.Diff(want, holder(got)), "the carrier is written as it stands")
	assert.NotContains(t, got, `"use"`, "a use of a decorator holding nothing is a bare reference")
	assert.Contains(t, got, `"elem": {`+"\n"+`        "target": "t/x/Obj",`+"\n"+`        "nullable": false`+"\n"+`      }`)
}
