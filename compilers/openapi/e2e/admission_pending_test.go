// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

//go:build pending

package e2e_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/ir"
)

// TestCompile_AUnionConjunctSharingNoKindWithTheOtherIsReported holds that an
// allOf of a union of a string and an array and of an object is reported once,
// at the schema, naming what each conjunct admits, whichever order the
// components and the conjuncts are written in: a union is read as the join of
// its branches' kinds, not passed over.
func TestCompile_AUnionConjunctSharingNoKindWithTheOtherIsReported(t *testing.T) {
	t.Parallel()
	for _, swapped := range []bool{false, true} {
		entries := "{$ref: '#/components/schemas/TextOrList'}, " + objRef
		if swapped {
			entries = objRef + ", {$ref: '#/components/schemas/TextOrList'}"
		}
		schemas := []string{
			"UnionAndObject: {allOf: [" + entries + "]}",
			"TextOrList: {oneOf: [{type: string}, {type: array, items: {type: string}}]}",
			"Obj: {type: object, properties: {k: {type: string}}}",
		}
		t.Run(fmt.Sprintf("entries-swapped-%t", swapped), func(t *testing.T) {
			t.Parallel()
			inBothOrders(t, schemas, func(t *testing.T, schemas []string) {
				_, diags := compileAnnotationSpec(t, "union", admissionSpec("3.1.0", "", schemas...))
				requireErrorsAndWarnings(t, diags, []wantFinding{{
					code: diag.DegradedConstruct, pointer: "/components/schemas/UnionAndObject",
					contains: []string{disjoint, "array or string", "object"},
				}})
			})
		})
	}
}

// TestCompile_ANullAliasConjunctIsReadAsTheNullItAliases holds that an allOf of
// a null and a string draws the same report whether the null is named directly
// or through an alias of it, in both orders: the alias is the same values.
func TestCompile_ANullAliasConjunctIsReadAsTheNullItAliases(t *testing.T) {
	t.Parallel()
	schemas := []string{
		"NullAndString: {allOf: [{$ref: '#/components/schemas/Nil'}, " + strRef + "]}",
		"NullAliasAndString: {allOf: [{$ref: '#/components/schemas/NilAlias'}, " + strRef + "]}",
		"Str: {type: string}",
		"Nil: {type: 'null'}",
		"NilAlias: {allOf: [{$ref: '#/components/schemas/Nil'}]}",
	}
	inBothOrders(t, schemas, func(t *testing.T, schemas []string) {
		_, diags := compileAnnotationSpec(t, "nil", admissionSpec("3.1.0", "", schemas...))
		requireErrorsAndWarnings(t, diags, []wantFinding{
			{code: diag.DegradedConstruct, pointer: "/components/schemas/NullAndString", contains: []string{disjoint, "null", "string"}},
			{code: diag.DegradedConstruct, pointer: "/components/schemas/NullAliasAndString", contains: []string{disjoint, "null", "string"}},
		})
		var texts []string
		for _, d := range diags {
			if d.Severity == ir.SeverityWarning {
				texts = append(texts, d.Message)
			}
		}
		require.Len(t, texts, 2)
		assert.Equal(t, texts[0], texts[1], "the alias draws the text the null itself draws")
	})
}

// TestCompile_AnEnumAndATypeSharingNoKindAreReportedAtTheSchema holds that an
// allOf of an enum of integers and a string type is reported once, at the
// schema, where the contradiction completes, and that the branch writing the
// type is an info and no warning of its own.
func TestCompile_AnEnumAndATypeSharingNoKindAreReportedAtTheSchema(t *testing.T) {
	t.Parallel()
	inBothOrders(t, []string{"E1: {allOf: [{enum: [1, 2]}, {type: string}]}"}, func(t *testing.T, schemas []string) {
		_, diags := compileAnnotationSpec(t, "enum", admissionSpec("3.1.0", "", schemas...))
		requireErrorsAndWarnings(t, diags, []wantFinding{{
			code: diag.DegradedConstruct, pointer: "/components/schemas/E1", contains: []string{disjoint, "integer", "string"},
		}})
		assert.Equal(t, 1, infosAt(diags, diag.DegradedConstruct, "/components/schemas/E1/allOf/1", ""),
			"the branch writing the type is an info")
	})
	t.Run("entries-swapped", func(t *testing.T) {
		t.Parallel()
		_, diags := compileAnnotationSpec(t, "enum",
			admissionSpec("3.1.0", "", "E1: {allOf: [{type: string}, {enum: [1, 2]}]}"))
		requireErrorsAndWarnings(t, diags, []wantFinding{{
			code: diag.DegradedConstruct, pointer: "/components/schemas/E1", contains: []string{disjoint},
		}})
	})
}

// TestCompile_AKeywordBesideARefIsReadByTheDisjointReportAtEveryVersion holds
// that an allOf whose conjuncts contradict only through the type written
// beside a $ref is reported at the schema at 3.1 and at 3.0 alike, since the
// lowering applies that type at both. At 3.0 the later-version warning stays.
func TestCompile_AKeywordBesideARefIsReadByTheDisjointReportAtEveryVersion(t *testing.T) {
	t.Parallel()
	schemas := []string{
		"Str: {type: string}",
		"Props: {properties: {a: {type: string}}}",
		"S: {allOf: [{$ref: '#/components/schemas/Str', type: object}]}",
		"T: {allOf: [{$ref: '#/components/schemas/Props', type: string}, {type: object}]}",
	}
	for _, version := range []string{"3.1.0", "3.0.3"} {
		t.Run(version, func(t *testing.T) {
			t.Parallel()
			inBothOrders(t, schemas, func(t *testing.T, schemas []string) {
				_, diags := compileAnnotationSpec(t, "beside", admissionSpec(version, "", schemas...))
				want := []wantFinding{
					{code: diag.DegradedConstruct, pointer: "/components/schemas/S", contains: []string{disjoint}},
					{code: diag.DegradedConstruct, pointer: "/components/schemas/T", contains: []string{disjoint}},
				}
				if version == "3.0.3" {
					want = append(want,
						wantFinding{code: diag.LaterVersionConstruct, pointer: "/components/schemas/S/allOf/0/type"},
						wantFinding{code: diag.LaterVersionConstruct, pointer: "/components/schemas/T/allOf/0/type"})
				}
				requireErrorsAndWarnings(t, diags, want)
			})
		})
	}
}

// redeclaredField returns the type a model gives its property wire, and the
// number of conflicting-redeclaration findings drawn under pointer.
func redeclaredField(t *testing.T, doc *ir.Document, diags []ir.Diagnostic, id, wire, pointer string) (ir.TypeID, int) {
	t.Helper()
	m, ok := doc.Types[ir.TypeID(id)].(*ir.Model)
	require.True(t, ok, "%s lowers to a model", id)
	conflicts := 0
	for _, d := range diags {
		if d.Code == diag.ConflictingRedecl && strings.HasPrefix(string(d.Provenance.Pointer), pointer+"/") {
			conflicts++
		}
	}
	for _, p := range m.Properties {
		if p.WireName == wire {
			return p.Type.Target, conflicts
		}
	}
	require.Failf(t, "no such property", "%s has no property %q", id, wire)
	return "", conflicts
}

// TestCompile_AFieldRedeclaredAsAStructureAdmittingItsKindIsNotAConflict holds
// that a field declared integer in one allOf branch and as a structure beside
// a union of an object and an integer in the other keeps integer with nothing
// reported, in both branch orders, while a structure refusing only strings
// through a not stays reported as before.
func TestCompile_AFieldRedeclaredAsAStructureAdmittingItsKindIsNotAConflict(t *testing.T) {
	t.Parallel()
	structure := func(ref string) string {
		return "properties: {f: {allOf: [{$ref: '#/components/schemas/" + ref + "'}], properties: {a: {type: string}}}}"
	}
	for _, swapped := range []bool{false, true} {
		branches := func(ref string) string {
			a, b := "{properties: {f: {type: integer}}}", "{"+structure(ref)+"}"
			if swapped {
				a, b = b, a
			}
			return "{allOf: [" + a + ", " + b + "]}"
		}
		schemas := []string{
			"ObjOrInt: {anyOf: [{type: object}, {type: integer}]}",
			"NotStr: {not: {type: string}}",
			"U: " + branches("ObjOrInt"),
			"T: " + branches("NotStr"),
		}
		t.Run(fmt.Sprintf("branches-swapped-%t", swapped), func(t *testing.T) {
			t.Parallel()
			inBothOrders(t, schemas, func(t *testing.T, schemas []string) {
				doc, diags := compileAnnotationSpec(t, "redecl", admissionSpec("3.1.0", "", schemas...))
				target, conflicts := redeclaredField(t, doc, diags, "t/openapi/components/schemas/U", "f", "/components/schemas/U")
				assert.Equal(t, ir.TypeID("t/prim/integer"), target, "U's field takes the narrower, integer")
				assert.Zero(t, conflicts, "no conflict is reported for U")
				assert.Empty(t, keptEntries(t, doc, "openapi:conflicting-redeclaration/components/schemas/U/allOf/0/properties/f"))

				_, conflicts = redeclaredField(t, doc, diags, "t/openapi/components/schemas/T", "f", "/components/schemas/T")
				assert.Equal(t, 1, conflicts, "T keeps its one conflict: a not refuses what the read cannot follow")
			})
		})
	}
}

// TestCompile_ANullNotBesideAUnionIsTheKeywordLeftOut holds that a field
// redeclared as a structure whose allOf names a union of an object and an
// integer and writes a null not is judged as the structure without the not,
// which the validation table warns of once: the field keeps integer, and the
// only error or warning is that one.
func TestCompile_ANullNotBesideAUnionIsTheKeywordLeftOut(t *testing.T) {
	t.Parallel()
	for _, swapped := range []bool{false, true} {
		a := "{properties: {f: {type: integer}}}"
		b := "{properties: {f: {allOf: [{$ref: '#/components/schemas/ObjOrInt'}, {not: null}], properties: {a: {type: string}}}}}"
		if swapped {
			a, b = b, a
		}
		schemas := []string{
			"ObjOrInt: {anyOf: [{type: object}, {type: integer}]}",
			"V: {allOf: [" + a + ", " + b + "]}",
		}
		t.Run(fmt.Sprintf("branches-swapped-%t", swapped), func(t *testing.T) {
			t.Parallel()
			inBothOrders(t, schemas, func(t *testing.T, schemas []string) {
				doc, diags := compileAnnotationSpec(t, "nullnot", admissionSpec("3.1.0", "", schemas...))
				target, conflicts := redeclaredField(t, doc, diags, "t/openapi/components/schemas/V", "f", "/components/schemas/V")
				assert.Equal(t, ir.TypeID("t/prim/integer"), target)
				assert.Zero(t, conflicts)
				var found []string
				for _, d := range diags {
					if d.Severity == ir.SeverityWarning {
						found = append(found, d.Code)
					}
				}
				assert.Equal(t, []string{"openapi/validation/validation-type-mismatch"}, found,
					"the null not is reported by the load phase alone")
			})
		})
	}
}
