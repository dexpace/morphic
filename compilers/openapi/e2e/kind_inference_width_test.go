// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test

import (
	"encoding/json/jsontext"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
)

// widthPositions are where widthSpec writes the schema under test: a
// component, a property, a union's branch and a request body.
var widthPositions = []jsontext.Pointer{
	"/components/schemas/Pos",
	"/components/schemas/Holder/properties/p",
	"/components/schemas/Br/oneOf/0",
	"/paths/~1x/post/requestBody/content/application~1json/schema",
}

// widthSpec is a document of the version given writing schema, one line of
// flow YAML, at each of widthPositions.
func widthSpec(version, schema string) string {
	paths := "paths:\n  /x:\n    post:\n      operationId: x\n      requestBody:\n        content:\n" +
		"          application/json:\n            schema: " + schema + "\n" +
		"      responses: {\"204\": {description: ok}}\n"
	return admissionSpec(version, paths,
		"Pos: "+schema,
		"Holder: {type: object, properties: {p: "+schema+"}}",
		"Br: {oneOf: ["+schema+", {type: integer}]}")
}

// nodeAtWidthPosition returns the node the position written at at lowers to.
func nodeAtWidthPosition(t *testing.T, doc *ir.Document, at jsontext.Pointer) ir.TypeDef {
	t.Helper()
	switch at {
	case widthPositions[0]:
		return kindNode(t, doc, componentID("Pos"))
	case widthPositions[1]:
		holder, ok := kindNode(t, doc, componentID("Holder")).(*ir.Model)
		require.True(t, ok)
		p, ok := propByWire(holder, "p")
		require.True(t, ok)
		return kindNode(t, doc, p.Type.Target)
	case widthPositions[2]:
		union, ok := kindNode(t, doc, componentID("Br")).(*ir.Union)
		require.True(t, ok)
		require.Len(t, union.Variants, 2)
		return kindNode(t, doc, union.Variants[0].Type.Target)
	default:
		return kindNode(t, doc, requestContent(t, operationAt(t, doc, "POST", "/x")).Type.Target)
	}
}

// widthTable is each keyword a schema naming no kind may write alone, with the
// kind it infers where it gives the type of a member, an element or a value,
// and the empty kind where it speaks of a kind and types nothing.
var widthTable = []struct {
	keyword, schema string
	infers          ir.TypeKind
	only31          bool
}{
	{"properties", "{properties: {a: {type: string}}}", ir.KindModel, false},
	{"patternProperties", "{patternProperties: {'^x': {type: string}}}", ir.KindModel, true},
	{"additionalProperties", "{additionalProperties: {type: string}}", ir.KindMap, false},
	{"additionalProperties", "{additionalProperties: true}", "", false},
	{"additionalProperties", "{additionalProperties: {}}", "", false},
	{"additionalProperties", "{additionalProperties: false}", "", false},
	{"required", "{required: [a]}", "", false},
	{"discriminator", "{discriminator: {propertyName: k}}", "", false},
	{"items", "{items: {type: string}}", ir.KindList, false},
	{"items", "{items: true}", "", true},
	{"items", "{items: {}}", "", false},
	{"items", "{items: false}", "", true},
	{"prefixItems", "{prefixItems: [{type: string}]}", ir.KindTuple, true},
}

// TestKindInference_OnlyAKeywordGivingATypeInfersItsKind holds the width of
// the policy: a schema writing no type infers the kind of a keyword that
// types a member, an element or a value, and is marked; one whose keyword
// types nothing, a `required`, a `discriminator`, a closure or a catch-all of
// `true` or `{}`, is the top type keeping the keyword with one info, as with
// the policy off, at a component, a property, a union's branch and a body.
func TestKindInference_OnlyAKeywordGivingATypeInfersItsKind(t *testing.T) {
	t.Parallel()
	for _, version := range []string{"3.0.3", "3.1.0"} {
		for _, tc := range widthTable {
			if tc.only31 && version == "3.0.3" {
				continue
			}
			t.Run(version+"/"+tc.schema, func(t *testing.T) {
				t.Parallel()
				for _, off := range []bool{false, true} {
					doc, diags := compileKindInference(t, "width", widthSpec(version, tc.schema), off)
					requireErrorsAndWarnings(t, diags, nil)
					for _, at := range widthPositions {
						assertWidthOutcome(t, doc, diags, at, tc.keyword, tc.infers, off)
					}
				}
			})
		}
	}
}

// assertWidthOutcome holds the position written at at to the node kind the
// keyword infers, marked, where it infers one and the policy is on, and to the
// top type keeping the keyword under one info otherwise.
func assertWidthOutcome(t *testing.T, doc *ir.Document, diags []ir.Diagnostic, at jsontext.Pointer,
	keyword string, infers ir.TypeKind, off bool,
) {
	t.Helper()
	td := nodeAtWidthPosition(t, doc, at)
	where := fmt.Sprintf("%s off=%t", at, off)
	if infers != "" && !off {
		assert.Equal(t, infers, td.Kind(), where)
		assert.True(t, markedKindInference(td), where)
		assert.Empty(t, degradedUnder(diags, at), where)
		return
	}
	scalar, ok := td.(*ir.Scalar)
	require.True(t, ok, "%s: the top type, got %s", where, td.Kind())
	require.NotNil(t, scalar.Base, where)
	assert.Equal(t, prim(ir.PrimAny), scalar.Base.Target, where)
	assert.False(t, markedKindInference(td), where)
	assert.Equal(t, map[string]ir.UnmodeledReason{"openapi:" + keyword: ir.ReasonDegradedLowering}, keptOn(td), where)
	assert.Equal(t, []jsontext.Pointer{at}, degradedUnder(diags, at), where)
}

// TestKindInference_AKeywordTypingNothingStillSpeaksOfItsKind holds that a
// keyword inferring no kind still speaks of the one it applies to beside
// conjuncts that declare kinds: a bare `required` beside a union of objects
// distributes across it, beside a union naming a string too it is the top
// type keeping both, and a union of two bare `required`s is a union of two top
// types, each keeping its own, under either policy.
func TestKindInference_AKeywordTypingNothingStillSpeaksOfItsKind(t *testing.T) {
	t.Parallel()
	schemas := []string{
		"A: {type: object, properties: {a: {type: string}}, required: [a], additionalProperties: false}",
		"B: {type: object, properties: {b: {type: string}}, required: [b], additionalProperties: false}",
		"Objects: {oneOf: [{$ref: '#/components/schemas/A'}, {$ref: '#/components/schemas/B'}], required: [a]}",
		"Mixed: {oneOf: [{$ref: '#/components/schemas/A'}, {type: string}], required: [a]}",
		"Narrowings: {anyOf: [{required: [a]}, {required: [b]}]}",
	}
	for _, version := range []string{"3.0.3", "3.1.0"} {
		for _, off := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/off=%t", version, off), func(t *testing.T) {
				t.Parallel()
				inBothOrders(t, schemas, func(t *testing.T, schemas []string) {
					doc, diags := compileKindInference(t, "speaks", admissionSpec(version, "", schemas...), off)
					requireErrorsAndWarnings(t, diags, nil)
					objects, ok := kindNode(t, doc, componentID("Objects")).(*ir.Union)
					require.True(t, ok, "a bare required distributes across objects")
					for _, v := range objects.Variants {
						variant, isModel := kindNode(t, doc, v.Type.Target).(*ir.Model)
						require.True(t, isModel)
						assert.Equal(t, []string{"a"}, variant.Requires)
						assert.False(t, markedKindInference(variant), "the branches declare the object")
					}
					assertUntypedOutcome(t, doc, "Mixed", topKeeping("oneOf", "required"))
					narrowings, ok := kindNode(t, doc, componentID("Narrowings")).(*ir.Union)
					require.True(t, ok)
					require.Len(t, narrowings.Variants, 2)
					for i, v := range narrowings.Variants {
						variant, isScalar := kindNode(t, doc, v.Type.Target).(*ir.Scalar)
						require.True(t, isScalar, "variant %d", i)
						require.NotNil(t, variant.Base)
						assert.Equal(t, prim(ir.PrimAny), variant.Base.Target, "variant %d", i)
						assert.Equal(t, map[string]ir.UnmodeledReason{"openapi:required": ir.ReasonDegradedLowering},
							keptOn(variant), "variant %d", i)
					}
				})
			})
		}
	}
}

// TestKindInference_ANarrowingBesideATypingKeywordStaysOnItsModel holds that a
// keyword typing nothing still binds the model a typing keyword beside it
// infers: `required` names its member and the closure closes it.
func TestKindInference_ANarrowingBesideATypingKeywordStaysOnItsModel(t *testing.T) {
	t.Parallel()
	for _, version := range []string{"3.0.3", "3.1.0"} {
		t.Run(version, func(t *testing.T) {
			t.Parallel()
			doc, diags := compileKindInference(t, "beside", admissionSpec(version, "",
				"Closed: {properties: {a: {type: string}}, required: [a], additionalProperties: false}"), false)
			requireErrorsAndWarnings(t, diags, nil)
			assert.Empty(t, degradedInfoPointers(diags))
			model, ok := kindNode(t, doc, componentID("Closed")).(*ir.Model)
			require.True(t, ok)
			assert.True(t, markedKindInference(model))
			a, ok := propByWire(model, "a")
			require.True(t, ok)
			assert.True(t, a.Required, "required names the member")
			assert.Equal(t, ir.AdditionalClosed, model.Additional)
			assert.Empty(t, keptOn(model))
		})
	}
}
