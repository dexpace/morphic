// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/ir"
)

// nullAlonePositions writes the schema at every position one can stand at: a
// required and an optional property, a list's element, a map's value, a union
// variant through a $ref, a $ref and a chain of two, a property an allOf
// composes, an allOf of it alone, a required path, an optional query and a
// required query parameter, and an optional and a required response header.
const nullAlonePositions = `openapi: 3.1.0
info: {title: t, version: "1"}
paths:
  /p/{id}:
    get:
      operationId: getP
      parameters:
        - {name: id, in: path, required: true, schema: %[1]s}
        - {name: q, in: query, schema: %[1]s}
        - {name: qr, in: query, required: true, schema: %[1]s}
      responses:
        "200":
          description: ok
          headers:
            X-Opt: {schema: %[1]s}
            X-Req: {required: true, schema: %[1]s}
components:
  schemas:
    Nil: %[1]s
    Chain: {$ref: '#/components/schemas/Nil'}
    AllOfNil: {allOf: [%[1]s]}
    Holder:
      type: object
      required: [req, reqRef]
      properties:
        req: %[1]s
        opt: %[1]s
        reqRef: {$ref: '#/components/schemas/Nil'}
        optChain: {$ref: '#/components/schemas/Chain'}
        list: {type: array, items: %[1]s}
        map: {type: object, additionalProperties: %[1]s}
        variant: {oneOf: [{type: integer}, {$ref: '#/components/schemas/Nil'}]}
        allOfNil: {$ref: '#/components/schemas/AllOfNil'}
    Composed:
      allOf:
        - {$ref: '#/components/schemas/Holder'}
        - {type: object, required: [extraReq], properties: {extraReq: %[1]s, extraOpt: %[1]s}}
`

// TestLowerNullAlone_EveryPositionNamesAnEnumWithNoMember pins a schema of null
// alone at every position of nullAlonePositions: the reference names an Enum
// with no member, read through the aliases a $ref or an allOf hoists, and its
// Nullable says the null, apart from the position's presence (CLAUDE.md
// invariant 8; docs/compilers/openapi.md#null-alone). A type excluding the null
// its const holds is the control: the same node admitting nothing, so no
// reference is nullable, which gives the other two states. An allOf composes
// the control as a model, a value set admitting no null refining nothing, so
// that position is not asked of it.
func TestLowerNullAlone_EveryPositionNamesAnEnumWithNoMember(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, schema string
		nullable     bool
	}{
		{name: "a type of null", schema: `{type: "null"}`, nullable: true},
		{name: "a type list of null", schema: `{type: ["null"]}`, nullable: true},
		{name: "a const of null a type excludes", schema: `{type: string, const: null}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc, diags := parseFull(t, fmt.Sprintf(nullAlonePositions, tc.schema))
			openapitest.RequireNoErrorDiags(t, diags)
			for _, p := range nullAloneUses(t, doc) {
				if p.label == "allOfNil" && !tc.nullable {
					continue
				}
				assertNullAloneAt(t, doc, p.label, p.ref)
				assert.Equal(t, tc.nullable, p.ref.Nullable, "%s: the reference alone says the null", p.label)
				if p.carried {
					assert.Equal(t, p.wantRequired, p.required, "%s keeps its own presence", p.label)
				}
			}
		})
	}
}

// nullAloneUse is one position of nullAlonePositions: its reference, and its
// presence where the position has one.
type nullAloneUse struct {
	label                           string
	ref                             ir.TypeRef
	carried, required, wantRequired bool
}

// nullAloneUses returns every position of nullAlonePositions as doc holds it.
func nullAloneUses(t *testing.T, doc *ir.Document) []nullAloneUse {
	t.Helper()
	var uses []nullAloneUse
	carried := func(label string, ref ir.TypeRef, required, want bool) {
		uses = append(uses, nullAloneUse{label: label, ref: ref, carried: true, required: required, wantRequired: want})
	}
	for wire, want := range map[string]bool{"req": true, "opt": false, "reqRef": true, "optChain": false, "allOfNil": false} {
		p := propertyOf(t, doc, "Holder", wire)
		carried(wire, p.Type, p.Required, want)
	}
	for wire, want := range map[string]bool{"extraReq": true, "extraOpt": false} {
		p := propertyOf(t, doc, "Composed", wire)
		carried(wire, p.Type, p.Required, want)
	}
	list, ok := doc.Types[propertyOf(t, doc, "Holder", "list").Type.Target].(*ir.List)
	require.True(t, ok)
	uses = append(uses, nullAloneUse{label: "list element", ref: list.Elem})
	m, ok := doc.Types[propertyOf(t, doc, "Holder", "map").Type.Target].(*ir.MapT)
	require.True(t, ok)
	uses = append(uses, nullAloneUse{label: "map value", ref: m.Value})
	u, ok := doc.Types[propertyOf(t, doc, "Holder", "variant").Type.Target].(*ir.Union)
	require.True(t, ok)
	require.Len(t, u.Variants, 2, "a $ref naming a schema of null alone stays a variant")
	uses = append(uses, nullAloneUse{label: "union variant", ref: u.Variants[1].Type})

	op := openapitest.FindOp(t, doc, "getP")
	for name, want := range map[string]bool{"id": true, "q": false, "qr": true} {
		param, found := openapitest.ParamBySource(op, name)
		require.True(t, found, name)
		carried("parameter "+name, param.Type, param.Required, want)
	}
	require.Len(t, op.Responses, 1)
	for _, h := range op.Responses[0].Headers {
		carried("header "+h.WireName, h.Type, h.Required, h.WireName == "X-Req")
	}
	require.Len(t, uses, 15, "every position is read")
	return uses
}

// assertNullAloneAt requires ref, read through the aliases a $ref and an allOf
// hoist, to name a closed Enum with no member.
func assertNullAloneAt(t *testing.T, doc *ir.Document, label string, ref ir.TypeRef) {
	t.Helper()
	td := doc.Types[ref.Target]
	for range 3 {
		alias, ok := td.(*ir.Scalar)
		if !ok || alias.Base == nil {
			break
		}
		td = doc.Types[alias.Base.Target]
	}
	e, ok := td.(*ir.Enum)
	require.True(t, ok, "%s names an Enum, got %T", label, td)
	assert.Empty(t, e.Members, "%s: the Enum has no member", label)
	assert.True(t, e.Closed, label)
}

// TestLowerNullAlone_IsToldFromTheTopType pins that a schema of null alone and
// one admitting every value and null name different nodes: the second the top
// type, nullable, and the first an Enum with no member of its own, where both
// had lowered to the first (GitHub #1830).
func TestLowerNullAlone_IsToldFromTheTopType(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, openapitest.ComponentSpecVer("3.1.0", `    Holder:
      type: object
      properties:
        onlyNull: {type: "null"}
        anything: {anyOf: [{}, {type: "null"}]}
`))
	openapitest.RequireNoErrorDiags(t, diags)
	onlyNull, anything := propertyOf(t, doc, "Holder", "onlyNull"), propertyOf(t, doc, "Holder", "anything")
	assert.Equal(t, ir.TypeRef{Target: "t/prim/any", Nullable: true}, anything.Type)
	assert.Equal(t, ir.TypeRef{Target: "t/anon/components/schemas/Holder/properties/onlyNull", Nullable: true}, onlyNull.Type)
	assertNullAloneAt(t, doc, "onlyNull", onlyNull.Type)
}

// TestLowerNullAlone_KeepsWhatTheEnumCannotHold pins the census beside a schema
// of null alone: an Enum with no member has no home for a property set, an
// element, a format or a bound, so each is kept verbatim with one info naming
// the node, and the type, which the Enum's emptiness says, draws nothing
// (docs/compilers/openapi.md#shape-applicator).
func TestLowerNullAlone_KeepsWhatTheEnumCannotHold(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, schema string
		kept         []string
		infos        int // one for what the census keeps, one for a union kept
	}{
		{name: "a property set", schema: `{type: "null", properties: {a: {type: string}}}`, kept: []string{"properties"}, infos: 1},
		{name: "an element", schema: `{type: "null", items: {type: string}}`, kept: []string{"items"}, infos: 1},
		{name: "a format", schema: `{type: "null", format: uuid}`, kept: []string{"format"}, infos: 1},
		{name: "a bound", schema: `{type: "null", maxLength: 3}`, kept: []string{"maxLength"}, infos: 1},
		{name: "a union of null", schema: `{oneOf: [{type: "null"}], minimum: 3}`, kept: []string{"minimum", "oneOf"}, infos: 2},
		{name: "nothing", schema: `{type: "null", description: d}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc, diags := parseFull(t, openapitest.ComponentSpecVer("3.1.0", "    S: "+tc.schema+"\n"))
			openapitest.RequireNoErrorDiags(t, diags)
			e, ok := doc.Types[componentID("S")].(*ir.Enum)
			require.True(t, ok, "got %T", doc.Types[componentID("S")])
			assert.Empty(t, e.Members)
			var keys []string
			for key, entry := range e.Unmodeled {
				keys = append(keys, key)
				assert.Equal(t, ir.ReasonDegradedLowering, entry.Reason, key)
			}
			want := make([]string, 0, len(tc.kept))
			for _, k := range tc.kept {
				want = append(want, "openapi:"+k)
			}
			assert.ElementsMatch(t, want, keys, "only what the Enum cannot hold is kept, never the type")
			infos := diagsAtPointer(diags, diag.DegradedConstruct, "/components/schemas/S")
			assert.Len(t, infos, tc.infos, "each keeping is reported once")
		})
	}
}
