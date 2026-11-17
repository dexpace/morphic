// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test

import (
	"fmt"
	"maps"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
)

// TestRefConjunction_ABareRefHoistsNoNode holds that a $ref to a string or an
// object writing nothing beside it is a use of the target at every position:
// no anonymous node stands there or names the target as its base, and a
// component over a string is the alias that owns it.
func TestRefConjunction_ABareRefHoistsNoNode(t *testing.T) {
	t.Parallel()
	for _, version := range []string{"3.0.3", "3.1.0"} {
		for _, target := range []string{"Obj", "Str"} {
			for _, site := range conjSites {
				if site.name == "component" && target == "Obj" {
					continue // a component over an object is a model, which a bare $ref makes only from now on
				}
				t.Run(fmt.Sprintf("%s/%s/%s", version, target, site.name), func(t *testing.T) {
					t.Parallel()
					requireNothingHoisted(t, version, target, site, "{"+refTo(target)+"}")
				})
			}
		}
	}
}

// TestRefConjunction_TwoUnionsOnEntriesLowerTheOneOfWhicheverEntryWritesIt holds
// that a oneOf and an anyOf on two entries beside a body, in either order of the
// entries, distribute the oneOf and keep the anyOf whole on each variant.
func TestRefConjunction_TwoUnionsOnEntriesLowerTheOneOfWhicheverEntryWritesIt(t *testing.T) {
	t.Parallel()
	for _, version := range []string{"3.0.3", "3.1.0"} {
		for _, name := range []string{"Entries", "EntriesRev"} {
			t.Run(version+"/"+name, func(t *testing.T) {
				t.Parallel()
				doc, _ := compileUnrefused(t, admissionSpec(version, "", "Use: "+twoUnionShapes[name]))
				requireOneOfDistributed(t, doc)
			})
		}
	}
}

// TestRefConjunction_KeywordsAnOverlayWritesBesideARefConjoinAsWritten holds
// that an overlay's update writing keywords beside a $ref makes the same
// conjunction the source would: a bound and a description beside a $ref to a
// string are the alias's own, and a oneOf of bounds beside one at a property
// is validation logic, kept once on the property.
func TestRefConjunction_KeywordsAnOverlayWritesBesideARefConjoinAsWritten(t *testing.T) {
	t.Parallel()
	doc, diags := compileWithPatch(t, `openapi: 3.1.0
info: {title: t, version: '1'}
paths: {}
components:
  schemas:
    T: {type: string}
    U: {$ref: '#/components/schemas/T'}
    H: {type: object, properties: {p: {$ref: '#/components/schemas/T'}}}
`, `overlay: 1.0.0
info: {title: p, version: '1'}
actions:
  - target: $.components.schemas.U
    update: {description: d, minLength: 2}
  - target: $.components.schemas.H.properties.p
    update: {description: e, oneOf: [{minLength: 1}, {maxLength: 5}]}
`)
	requireErrorsAndWarnings(t, diags, nil)
	u, ok := kindNode(t, doc, componentID("U")).(*ir.Scalar)
	require.True(t, ok)
	require.NotNil(t, u.Base)
	assert.Equal(t, componentID("T"), u.Base.Target)
	assert.Equal(t, "d", u.Docs.Description)
	require.NotNil(t, u.Constraints)
	require.NotNil(t, u.Constraints.MinLength)
	assert.Equal(t, int64(2), *u.Constraints.MinLength)
	assert.Empty(t, u.Unmodeled)

	h := requireModelNode(t, doc, componentID("H"))
	p, ok := propByWire(h, "p")
	require.True(t, ok)
	assert.Equal(t, componentID("T"), p.Type.Target, "a carrier hoists no node")
	assert.Equal(t, "e", p.Docs.Description)
	assert.Equal(t, ir.ReasonValidationOnly, unmodeledEntry(t, p.Unmodeled, "openapi:oneOf").Reason)
	assert.Len(t, p.Unmodeled, 1)
}

// TestRefConjunction_ACarrierOfAnAllOfOfOneRefReadsItsTargetsDocs holds that a
// property or a parameter whose schema is an allOf of one $ref saying nothing
// is the use of the target a bare $ref is: no node stands between them, and the
// carrier reads the target's description as it does through the bare $ref.
func TestRefConjunction_ACarrierOfAnAllOfOfOneRefReadsItsTargetsDocs(t *testing.T) {
	t.Parallel()
	params := make([]ir.Parameter, 0, 2)
	for _, schema := range []string{"{" + refTo("Doc") + "}", "{allOf: [{" + refTo("Doc") + "}]}"} {
		paths := "paths:\n  /a:\n    get:\n      parameters:\n        - {name: q, in: query, schema: " + schema +
			"}\n      responses: {'200': {description: ok}}\n"
		doc, diags := compileUnrefused(t, admissionSpec("3.1.0", paths,
			"Doc: {type: object, description: the target's own, properties: {a: {type: string}}}",
			"Holder: {type: object, properties: {p: "+schema+"}}"))
		requireErrorsAndWarnings(t, diags, nil)
		p, ok := propByWire(requireModelNode(t, doc, componentID("Holder")), "p")
		require.True(t, ok)
		assert.Equal(t, componentID("Doc"), p.Type.Target, schema)
		assert.Equal(t, "the target's own", p.Docs.Description, schema)
		op := operationAt(t, doc, "GET", "/a")
		require.Len(t, op.Params, 1)
		assert.Equal(t, componentID("Doc"), op.Params[0].Type.Target, schema)
		params = append(params, op.Params[0])
	}
	assert.Equal(t, params[0].Docs, params[1].Docs, "the parameter reads the docs alike in either spelling")
}

// TestRefConjunction_AKeywordBesideAValueSetIsKeptOnceOnItsNode holds that a
// keyword no Enum holds, written beside a value set narrowing a $ref, is kept
// once on the Enum the position lowers to, whether the value set is written
// beside the $ref, beside an allOf of it or on an entry of that allOf, at a
// component and at a property.
func TestRefConjunction_AKeywordBesideAValueSetIsKeptOnceOnItsNode(t *testing.T) {
	t.Parallel()
	for name, schema := range map[string]string{
		"beside the $ref":   "{" + refTo("Str") + ", enum: [a, b], pattern: '^a'}",
		"beside an allOf":   "{allOf: [{" + refTo("Str") + "}], enum: [a, b], pattern: '^a'}",
		"on an allOf entry": "{allOf: [{" + refTo("Str") + "}, {enum: [a, b]}], pattern: '^a'}",
	} {
		for _, site := range conjSites[:2] {
			doc, diags := compileUnrefused(t, site.write("3.1.0", schema, conjTargets["Str"]))
			at := name + " at " + site.name
			id := ir.TypeID("t/anon" + site.pointer)
			if site.name == "component" {
				id = componentID("Use")
			}
			enum, ok := kindNode(t, doc, id).(*ir.Enum)
			require.True(t, ok, at)
			assert.ElementsMatch(t, []string{"openapi:$ref", "openapi:pattern"}, slices.Collect(maps.Keys(enum.Unmodeled)), at)
			assert.JSONEq(t, `"^a"`, string(enum.Unmodeled["openapi:pattern"].Value), at)
			assert.Equal(t, 2, degradedInfosUnder(diags, site.pointer), "%s: one report for each keyword kept", at)
		}
	}
}

// TestRefConjunction_AKeptRefsSiblingsAreKeptEachUnderItsOwnKey holds that a
// $ref a composition or a value set passes over is kept under openapi:$ref as
// the reference alone, and that each keyword written beside it, beside the
// $ref or beside it on an allOf entry, is a conjunct kept under its own key
// with its own reason: a note is the node's, a bound and a format no node
// holds degraded, an unknown keyword out of scope, an extension a vendor's.
func TestRefConjunction_AKeptRefsSiblingsAreKeptEachUnderItsOwnKey(t *testing.T) {
	t.Parallel()
	const siblings = "description: d, maxLength: 9, format: email, foo: bar, x-v: 1"
	ref := refTo("Str")
	object := "{type: object, properties: {p: {type: string}}}"
	for name, schema := range map[string]string{
		"a value set, beside the $ref":   "{" + ref + ", " + siblings + ", enum: [a]}",
		"a value set, on an entry":       "{allOf: [{" + ref + ", " + siblings + "}], enum: [a]}",
		"a composition, beside the $ref": "{" + ref + ", " + siblings + ", allOf: [" + object + "]}",
		"a composition, on an entry":     "{allOf: [{" + ref + ", " + siblings + "}, " + object + "]}",
	} {
		doc, _ := compileAnnotationSpec(t, "kept", admissionSpec("3.1.0", "", conjTargets["Str"], "Use: "+schema))
		node := kindNode(t, doc, componentID("Use"))
		kept := node.Common().Unmodeled
		want := map[string]ir.UnmodeledReason{
			"openapi:$ref": ir.ReasonDegradedLowering, "openapi:maxLength": ir.ReasonDegradedLowering,
			"openapi:format": ir.ReasonDegradedLowering, "openapi:foo": ir.ReasonOutOfScope,
			"openapi:x-v": ir.ReasonVendorExtension,
		}
		got := map[string]ir.UnmodeledReason{}
		for key, entry := range kept {
			got[key] = entry.Reason
		}
		assert.Equal(t, want, got, name)
		assert.JSONEq(t, `{"$ref":"#/components/schemas/Str"}`, string(kept["openapi:$ref"].Value), "%s: the reference alone", name)
		assert.Equal(t, "d", node.Common().Docs.Description, "%s: the note is the node's", name)
	}
}
