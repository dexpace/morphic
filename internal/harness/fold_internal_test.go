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
	objID    ir.TypeID = "type/x/Obj"
	decID    ir.TypeID = "type/x/Use/dec"
	outerID  ir.TypeID = "type/x/Use/outer"
	holderID ir.TypeID = "type/x/Use"
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
		holderID: &ir.List{ID: holderID, ElementType: ir.TypeRef{Target: objID}},
	}
	if decorator != nil {
		types[decID] = decorator
		types[holderID].(*ir.List).ElementType.Target = decID
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
	assert.NotContains(t, foldedText(t, model), `"kind": "model",`+"\n      \"id\": \""+string(decID)+"\"",
		"the decorator is left out of Types")
	assert.Contains(t, foldedText(t, model), `"use": {`, "the use writes what the decorator held")

	other := listOf(&ir.Model{TypeCommon: annotated(decID, "e"), Base: base})
	assert.NotEqual(t, foldedText(t, model), foldedText(t, other), "a use holding another description differs")
	assert.NotEqual(t, foldedText(t, model), foldedText(t, listOf(nil)), "a use holds its annotations")
}

// carrying returns a document whose carrier of kind names Obj and holds the
// Docs, Deprecation and Examples annotated(_, "d") holds, the annotations a
// carrier takes from its decorator, through a decorator when decorated.
func carrying(kind string, decorated bool) *ir.Document {
	notes := annotated(decID, "d")
	notes.Availability, notes.Unmodeled = nil, nil
	ref := ir.TypeRef{Target: objID}
	var property ir.Property
	if decorated {
		ref.Target = decID
	} else {
		property = ir.Property{Docs: notes.Docs, Deprecation: notes.Deprecation, Examples: notes.Examples}
	}
	property.ID, property.Type = "property/x/Use/p", ref
	doc := listOf(nil)
	if decorated {
		doc.Types[decID] = &ir.Scalar{TypeCommon: notes, Base: &ir.TypeRef{Target: objID}}
	}
	switch kind {
	case "property":
		doc.Types[holderID] = &ir.Model{ID: holderID, Properties: []ir.Property{property}}
	case "header":
		doc.Services = []ir.Service{{ID: "service/x", Groups: []ir.OperationGroup{{ID: "operation-group/x", Operations: []ir.Operation{
			{ID: "operation/x", Responses: []ir.Response{{Headers: []ir.Header{{Property: property}}}}},
		}}}}}
	case "parameter":
		doc.Services = []ir.Service{{ID: "service/x", Groups: []ir.OperationGroup{{ID: "operation-group/x", Operations: []ir.Operation{
			{ID: "operation/x", Params: []ir.Parameter{{ID: "parameter/x", Type: property.Type, Docs: property.Docs, Deprecation: property.Deprecation,
				Examples: property.Examples, Availability: property.Availability, Unmodeled: property.Unmodeled}}},
		}}}}}
	default:
		doc.Types[holderID] = &ir.Union{ID: holderID, Variants: []ir.Variant{{
			Type: property.Type, Docs: property.Docs, Deprecation: property.Deprecation, Examples: property.Examples, Availability: property.Availability,
			Unmodeled: property.Unmodeled,
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

// keepOn sets the Availability and Unmodeled of the carrier of kind in a
// document carrying builds.
func keepOn(doc *ir.Document, kind string, a *ir.Availability, unmodeled ir.Unmodeled) {
	switch kind {
	case "property":
		property := &doc.Types[holderID].(*ir.Model).Properties[0]
		property.Availability, property.Unmodeled = a, unmodeled
	case "header":
		header := &doc.Services[0].Groups[0].Operations[0].Responses[0].Headers[0]
		header.Availability, header.Unmodeled = a, unmodeled
	case "parameter":
		parameter := &doc.Services[0].Groups[0].Operations[0].Params[0]
		parameter.Availability, parameter.Unmodeled = a, unmodeled
	default:
		variant := &doc.Types[holderID].(*ir.Union).Variants[0]
		variant.Availability, variant.Unmodeled = a, unmodeled
	}
}

// TestFolded_ACarrierLeavesWhatTheCollapseLeaves pins what no carrier takes:
// a decorator's Availability and Unmodeled stay beside the carrier naming it,
// so one holding them itself differs.
func TestFolded_ACarrierLeavesWhatTheCollapseLeaves(t *testing.T) {
	notes := annotated(decID, "d")
	for _, kind := range []string{"property", "header", "parameter", "variant"} {
		t.Run(kind, func(t *testing.T) {
			decorated, plain := carrying(kind, true), carrying(kind, false)
			dec := decorated.Types[decID].Common()
			dec.Availability, dec.Unmodeled = notes.Availability, notes.Unmodeled
			keepOn(plain, kind, notes.Availability, notes.Unmodeled)

			got := foldedText(t, decorated)

			assert.NotEqual(t, foldedText(t, plain), got)
			assert.Contains(t, got, `"decorator": {`+"\n")
			assert.NotContains(t, got, string(decID))
			keepOn(decorated, kind, notes.Availability, notes.Unmodeled)
			assert.NotEqual(t, foldedText(t, plain), foldedText(t, decorated), "the carrier's own stay its own")
		})
	}
}

// TestFolded_TheCarriersOwnComeFirst pins the merge onto a carrier: a field it
// sets is its own, one it leaves empty the decorator's, of its deprecation
// too, a description and its references as one, and an unmodeled name both
// hold kept on each.
func TestFolded_TheCarriersOwnComeFirst(t *testing.T) {
	doc := carrying("property", true)
	property := &doc.Types[holderID].(*ir.Model).Properties[0]
	property.Docs = ir.Docs{Summary: "own", Refs: []ir.DocRef{{Type: objID}}}
	property.Examples = []ir.Example{{Name: "own"}}
	property.Deprecation = &ir.Deprecation{Since: "own"}
	property.Unmodeled = ir.Unmodeled{"openapi:x": {Reason: ir.ReasonOutOfScope, Value: ir.RawValue(`1`)}}
	dec := doc.Types[decID].Common()
	dec.Docs.Summary, dec.Docs.ExternalDocs = "theirs", []ir.Link{{URL: "https://example.com"}}
	dec.Unmodeled = annotated(decID, "d").Unmodeled

	var got struct {
		Properties []struct {
			Docs        ir.Docs                      `json:"docs"`
			Deprecation *ir.Deprecation              `json:"deprecation"`
			Examples    []ir.Example                 `json:"examples"`
			Unmodeled   map[string]ir.UnmodeledEntry `json:"unmodeled"`
			Decorator   struct {
				Unmodeled map[string]ir.UnmodeledEntry `json:"unmodeled"`
			} `json:"decorator"`
		} `json:"properties"`
	}
	own, more := folded(doc)
	b, err := json.Marshal(own.Types[holderID], json.WithMarshalers(more))
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(b, &got))

	require.Len(t, got.Properties, 1)
	assert.Equal(t, ir.Docs{Summary: "own", Refs: []ir.DocRef{{Type: objID}},
		ExternalDocs: []ir.Link{{URL: "https://example.com"}}}, got.Properties[0].Docs)
	assert.Equal(t, &ir.Deprecation{Message: "d", Since: "own"}, got.Properties[0].Deprecation)
	assert.Equal(t, []ir.Example{{Name: "own"}}, got.Properties[0].Examples)
	assert.Equal(t, property.Unmodeled, ir.Unmodeled(got.Properties[0].Unmodeled), "the carrier's own")
	assert.Equal(t, dec.Unmodeled, ir.Unmodeled(got.Properties[0].Decorator.Unmodeled), "the decorator's, beside it")
}

// carrierSlots points at the carrier of kind in a document carrying builds.
func carrierSlots(doc *ir.Document, kind string) slots {
	switch kind {
	case "property":
		property := &doc.Types[holderID].(*ir.Model).Properties[0]
		return slots{ref: &property.Type, docs: &property.Docs, deprecation: &property.Deprecation, examples: &property.Examples}
	case "header":
		header := &doc.Services[0].Groups[0].Operations[0].Responses[0].Headers[0]
		return slots{ref: &header.Type, docs: &header.Docs, deprecation: &header.Deprecation, examples: &header.Examples}
	case "parameter":
		parameter := &doc.Services[0].Groups[0].Operations[0].Params[0]
		return slots{ref: &parameter.Type, docs: &parameter.Docs, deprecation: &parameter.Deprecation, examples: &parameter.Examples}
	default:
		variant := &doc.Types[holderID].(*ir.Union).Variants[0]
		return slots{ref: &variant.Type, docs: &variant.Docs, deprecation: &variant.Deprecation, examples: &variant.Examples}
	}
}

// TestFolded_ACarrierTakesTheDocsOfWhatItsDecoratorStandsFor pins the last
// step of the merge: a property, parameter or header takes the Docs and
// Deprecation of the type its decorator stands for where the carrier and the
// decorator set none, as a reference naming that type is lent them, and never
// its Examples. A union variant takes none of them.
func TestFolded_ACarrierTakesTheDocsOfWhatItsDecoratorStandsFor(t *testing.T) {
	for _, c := range []struct {
		kind          string
		takesFallback bool
	}{{"property", true}, {"header", true}, {"parameter", true}, {"variant", false}} {
		t.Run(c.kind, func(t *testing.T) {
			decorated, plain := carrying(c.kind, true), carrying(c.kind, false)
			for _, doc := range []*ir.Document{decorated, plain} {
				obj := doc.Types[objID].Common()
				obj.Docs = ir.Docs{Summary: "obj", Description: "obj"}
				obj.Deprecation, obj.Examples = &ir.Deprecation{Message: "obj"}, []ir.Example{{Name: "obj"}}
			}
			dec := decorated.Types[decID].Common()
			dec.Deprecation, dec.Examples = nil, nil
			slots := carrierSlots(plain, c.kind)
			*slots.deprecation, *slots.examples = nil, nil
			if c.takesFallback {
				slots.docs.Summary, *slots.deprecation = "obj", &ir.Deprecation{Message: "obj"}
			}

			assert.Empty(t, cmp.Diff(foldedText(t, plain), foldedText(t, decorated)))
		})
	}
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

// TestMergeDeprecation_TakesEveryField holds mergeDeprecation to
// ir.Deprecation's fields: each is taken from the second where the first
// leaves it empty, and either stands where the other is nil.
func TestMergeDeprecation_TakesEveryField(t *testing.T) {
	fields := reflect.VisibleFields(reflect.TypeFor[ir.Deprecation]())
	names := make([]string, 0, len(fields))
	for _, f := range fields {
		names = append(names, f.Name)
	}
	assert.Equal(t, []string{"Message", "Since", "RemovalVersion", "RemovalDate", "ReplacedBy"}, names,
		"mergeDeprecation merges these")

	full := &ir.Deprecation{Message: "m", Since: "s", RemovalVersion: "v", RemovalDate: "d",
		ReplacedBy: []ir.EntityRef{{}}}
	own := &ir.Deprecation{Since: "own"}
	assert.Equal(t, full, mergeDeprecation(&ir.Deprecation{}, full))
	assert.Equal(t, &ir.Deprecation{Message: "m", Since: "own", RemovalVersion: "v", RemovalDate: "d",
		ReplacedBy: []ir.EntityRef{{}}}, mergeDeprecation(own, full), "a field the first sets is its own")
	assert.Equal(t, &ir.Deprecation{Since: "own"}, own, "the first is left as it was")
	assert.Same(t, full, mergeDeprecation(nil, full))
	assert.Same(t, own, mergeDeprecation(own, nil))
	assert.Nil(t, mergeDeprecation(nil, nil))
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
		objID:      &ir.Model{ID: objID},
		outerID:    &ir.Scalar{TypeCommon: outer, Base: &ir.TypeRef{Target: decID}},
		decID:      &ir.Model{TypeCommon: inner, Base: &ir.TypeRef{Target: objID}},
		"type/x/a": &ir.Scalar{ID: "type/x/a", Anonymous: true, Base: &ir.TypeRef{Target: "type/x/b"}},
		"type/x/b": &ir.Scalar{ID: "type/x/b", Anonymous: true, Base: &ir.TypeRef{Target: "type/x/a"}},
		"type/x/d": &ir.Scalar{ID: "type/x/d", Anonymous: true, Base: &ir.TypeRef{Target: "type/x/gone"}},
	}

	uses := decoratorUses(types)

	assert.Len(t, uses, 3, "the cycle's decorators are left out")
	assert.Equal(t, objID, uses[outerID].target)
	assert.Equal(t, ir.Docs{Summary: "inner summary", Description: "outer"}, uses[outerID].notes.docs)
	assert.Equal(t, "outer", uses[outerID].notes.deprecation.Message)
	assert.Equal(t, []ir.Example{{Name: "outer"}}, uses[outerID].notes.examples)
	assert.Len(t, uses[outerID].notes.unmodeled["openapi:x"], 2)
	repeated, ok := uses[outerID].notes.unmodeledMember()["openapi:x"].([]ir.UnmodeledEntry)
	require.True(t, ok, "a name the chain repeats is written as a list")
	assert.Len(t, repeated, 2)
	assert.Equal(t, ir.TypeID("type/x/gone"), uses["type/x/d"].target)
	assert.Zero(t, uses["type/x/d"].fallbackAnnotations, "a type the registry does not hold lends nothing")

	_, more := folded(&ir.Document{Types: types})
	b, err := json.Marshal(types["type/x/a"], json.WithMarshalers(more))
	require.NoError(t, err)
	assert.Contains(t, string(b), `"target":"type/x/b"`, "a cycle's reference is written as it stands")
}

// TestFolded_AnIDNamingADecoratorNamesWhatItStandsFor pins the TypeID
// marshaler: a discriminator's mapping naming a decorator names its target.
func TestFolded_AnIDNamingADecoratorNamesWhatItStandsFor(t *testing.T) {
	doc := listOf(&ir.Scalar{TypeCommon: annotated(decID, "d"), Base: &ir.TypeRef{Target: objID}})
	doc.Types["type/x/U"] = &ir.Union{ID: "type/x/U",
		Discriminator: &ir.Discriminator{Mapping: map[string]ir.TypeID{"a": decID}}}

	assert.Contains(t, foldedText(t, doc), `"a": "type/x/Obj"`)
	assert.NotContains(t, foldedText(t, doc), string(decID))
}

// TestFolded_AUseKeepsItsNullable pins the referring reference's Nullable,
// which answers for the whole decorator: a nullable use of one, at a list and
// at a carrier, differs from a use that is not.
func TestFolded_AUseKeepsItsNullable(t *testing.T) {
	decorator := func() ir.TypeDef {
		return &ir.Scalar{TypeCommon: annotated(decID, "d"), Base: &ir.TypeRef{Target: objID}}
	}
	nullable := listOf(decorator())
	nullable.Types[holderID].(*ir.List).ElementType.Nullable = true
	assert.Contains(t, foldedText(t, nullable), `"nullable": true`)
	assert.NotEqual(t, foldedText(t, listOf(decorator())), foldedText(t, nullable))

	carrier := carrying("property", true)
	carrier.Types[holderID].(*ir.Model).Properties[0].Type.Nullable = true
	assert.Contains(t, foldedText(t, carrier), `"nullable": true`)
	assert.NotEqual(t, foldedText(t, carrying("property", true)), foldedText(t, carrier))
}

// TestWithoutTypes_LeavesTheDocumentAsItWas pins the removal: the decorators
// leave the copy alone, and an ID the document holds no type at is a panic.
func TestWithoutTypes_LeavesTheDocumentAsItWas(t *testing.T) {
	doc := listOf(&ir.Scalar{TypeCommon: annotated(decID, "d"), Base: &ir.TypeRef{Target: objID}})

	own, _ := folded(doc)

	assert.NotContains(t, own.Types, decID)
	assert.Contains(t, own.Types, holderID)
	assert.Contains(t, doc.Types, decID, "the document folded keeps its decorator")
	assert.PanicsWithValue(t,
		`harness: leave the decorators out: internal: edit: remove rejected: no type "type/x/none" to remove; an edit adds no type`,
		func() { withoutTypes(doc, []ir.TypeID{"type/x/none"}) })
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
	plain.Types["type/x/L"] = &ir.List{ID: "type/x/L", ElementType: ir.TypeRef{Target: decID}}
	plain.Types[decID] = &ir.Scalar{ID: decID, Anonymous: true, Base: &ir.TypeRef{Target: objID}}

	got := foldedText(t, plain)

	assert.Empty(t, cmp.Diff(want, holder(got)), "the carrier is written as it stands")
	assert.NotContains(t, got, `"use"`, "a use of a decorator holding nothing is a bare reference")
	assert.Contains(t, got, `"elementType": {`+"\n"+`        "target": "type/x/Obj",`+"\n"+`        "nullable": false`+"\n"+`      }`)
}
