// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema_test

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/ir"
)

// undecidedSpec writes entries a and b, in that order, as the allOf of the
// component C, of Holder's property p and of the operation's query parameter q.
func undecidedSpec(a, b string) string {
	all := "{allOf: [" + a + ", " + b + "]}"
	return `openapi: 3.1.0
info: {title: t, version: "1"}
paths:
  /p:
    get:
      parameters:
        - {name: q, in: query, schema: ` + all + `}
      responses:
        '200': {description: ok}
components:
  schemas:
    C: ` + all + `
    Holder: {type: object, properties: {p: ` + all + `}}
`
}

// undecidedTargets returns what the component C, the property p and the
// parameter q lowered to, C as what its alias is a use of.
func undecidedTargets(t *testing.T, doc *ir.Document) []ir.TypeRef {
	t.Helper()
	q, ok := openapitest.ParamBySource(openapitest.FirstOp(t, doc.Services[0]), "q")
	require.True(t, ok)
	c := typeByName(doc, "C")
	if sc, isScalar := c.(*ir.Scalar); isScalar && sc.Base != nil {
		return []ir.TypeRef{*sc.Base, propertyOf(t, doc, "Holder", "p").Type, q.Type}
	}
	return []ir.TypeRef{{Target: c.Common().ID}, propertyOf(t, doc, "Holder", "p").Type, q.Type}
}

// TestRefinementUndecided_ThePositionIsTheKindsItsEntriesAdmit pins an allOf
// electing no entry, with no keyword beside it naming a kind: in either order
// of its entries, at a component, a property and a parameter, it is the
// primitive its entries admit together, where it was the top type, each entry
// kept with one report at it, as before.
func TestRefinementUndecided_ThePositionIsTheKindsItsEntriesAdmit(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		a, b string
		prim ir.PrimKind
	}{
		"two formats":                   {"{type: string, format: email}", "{type: string, format: uuid}", ir.PrimString},
		"two overlapping enums":         {"{enum: [a, b]}", "{enum: [b, c]}", ir.PrimString},
		"two type lists sharing a kind": {"{type: [string, integer]}", "{type: [string, boolean]}", ir.PrimString},
		"two integer formats":           {"{type: integer, format: int32}", "{type: integer, format: uint32}", ir.PrimInteger},
		"two sets of fractions":         {"{enum: [1.5, 2.5]}", "{enum: [2.5, 3.5]}", ir.PrimNumber},
		"two type lists sharing a flag": {"{type: [boolean, string], format: a}", "{type: [boolean, integer], format: b}",
			ir.PrimBool},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			for _, order := range [][2]string{{tc.a, tc.b}, {tc.b, tc.a}} {
				doc, diags := parseFull(t, undecidedSpec(order[0], order[1]))
				openapitest.RequireNoErrorDiags(t, diags)
				for _, ref := range undecidedTargets(t, doc) {
					assert.Equal(t, ir.PrimTypeID(tc.prim), ref.Target, "%v", order)
				}
				kept := typeByName(doc, "C").Common().Unmodeled
				assert.Equal(t, []string{"openapi:allOf/0", "openapi:allOf/1"}, unmodeledKeys(kept), "%v", order)
				for _, at := range []string{"/components/schemas/C/allOf/0", "/components/schemas/C/allOf/1"} {
					assert.Len(t, diagsAtPointer(diags, diag.DegradedConstruct, at), 1, "%v %s", order, at)
				}
			}
		})
	}
}

// TestRefinementUndecided_SeveralSharedKindsAreTheirUnion pins two type lists
// sharing two kinds, and two value sets holding members of two: the position
// is the Union of those primitives, in one order whichever order the entries
// are written in, holding both entries.
func TestRefinementUndecided_SeveralSharedKindsAreTheirUnion(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string][2]string{
		"type lists": {"{type: [string, integer, boolean], format: a}", "{type: [string, integer, 'null'], format: b}"},
		"value sets": {"{enum: [a, 1, x]}", "{enum: [a, 1, y]}"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			for _, order := range [][2]string{tc, {tc[1], tc[0]}} {
				doc, diags := parseFull(t, undecidedSpec(order[0], order[1]))
				openapitest.RequireNoErrorDiags(t, diags)
				u, ok := typeByName(doc, "C").(*ir.Union)
				require.True(t, ok, "%v: got %T", order, typeByName(doc, "C"))
				var variants []ir.TypeID
				for _, v := range u.Variants {
					variants = append(variants, v.Type.Target)
				}
				assert.Equal(t, []ir.TypeID{ir.PrimTypeID(ir.PrimString), ir.PrimTypeID(ir.PrimInteger)}, variants, "%v", order)
				assert.Equal(t, []string{"openapi:allOf/0", "openapi:allOf/1"}, unmodeledKeys(u.Unmodeled), "%v", order)
			}
		})
	}
}

// TestRefinementUndecided_ANullTheEntriesAllAdmitStays pins that the kinds a
// position electing no entry is typed by leave null aside: one every entry
// admits makes its use nullable, and one only an entry admits does not.
func TestRefinementUndecided_ANullTheEntriesAllAdmitStays(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, openapitest.ComponentSpecVer("3.1.0", "    Holder:\n      type: object\n      properties:\n"+
		"        both: {allOf: [{type: [string, 'null'], format: email}, {type: [string, 'null'], format: uuid}]}\n"+
		"        one: {allOf: [{type: [string, 'null'], format: email}, {type: string, format: uuid}]}\n"))
	openapitest.RequireNoErrorDiags(t, diags)
	assert.Equal(t, ir.TypeRef{Target: ir.PrimTypeID(ir.PrimString), Nullable: true}, propertyOf(t, doc, "Holder", "both").Type)
	assert.Equal(t, ir.TypeRef{Target: ir.PrimTypeID(ir.PrimString)}, propertyOf(t, doc, "Holder", "one").Type)
}

// TestRefinementUndecided_AnElectionOrNoSharedValueIsUnchanged pins what is no
// election of none: an integer and a number elect the integer, and two kinds
// sharing no value compose with the warning saying so, in either order.
func TestRefinementUndecided_AnElectionOrNoSharedValueIsUnchanged(t *testing.T) {
	t.Parallel()
	for _, order := range [][2]string{{"{type: integer}", "{type: number}"}, {"{type: number}", "{type: integer}"}} {
		doc, diags := parseFull(t, undecidedSpec(order[0], order[1]))
		openapitest.RequireNoErrorDiags(t, diags)
		for _, ref := range undecidedTargets(t, doc) {
			assert.Equal(t, ir.PrimTypeID(ir.PrimInteger), ref.Target, "%v", order)
		}
	}
	for _, order := range [][2]string{{"{type: string}", "{type: integer}"}, {"{type: integer}", "{type: string}"}} {
		doc, diags := parseFull(t, undecidedSpec(order[0], order[1]))
		openapitest.RequireNoErrorDiags(t, diags)
		_, model := typeByName(doc, "C").(*ir.Model)
		assert.True(t, model, "%v", order)
		warned := slices.ContainsFunc(diagsAtPointer(diags, diag.DegradedConstruct, "/components/schemas/C"),
			func(d ir.Diagnostic) bool { return d.Severity == ir.SeverityWarning })
		assert.True(t, warned, "%v", order)
	}
}
