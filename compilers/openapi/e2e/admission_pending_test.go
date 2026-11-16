// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

//go:build pending

package e2e_test

import (
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/ir"
)

// infosAt counts the infos of code whose pointer is at or under pointer and
// whose message holds word.
func infosAt(diags []ir.Diagnostic, code, pointer, word string) int {
	n := 0
	for _, d := range diags {
		at := string(d.Provenance.Pointer)
		if d.Severity == ir.SeverityInfo && d.Code == code &&
			(at == pointer || strings.HasPrefix(at, pointer+"/")) && strings.Contains(d.Message, word) {
			n++
		}
	}
	return n
}

// walkObjects calls visit on every JSON object under tree.
func walkObjects(tree any, visit func(m map[string]any)) {
	switch v := tree.(type) {
	case map[string]any:
		visit(v)
		for _, child := range v {
			walkObjects(child, visit)
		}
	case []any:
		for _, child := range v {
			walkObjects(child, visit)
		}
	}
}

// boundHomes is the sorted IDs of every node, property and parameter of doc
// whose Constraints hold the member named key (JSON spelling, "min" or
// "minLength"): a bound placed as a constraint.
func boundHomes(t *testing.T, doc *ir.Document, key string) []string {
	t.Helper()
	homes := []string{}
	walkObjects(documentTree(t, doc), func(m map[string]any) {
		c, ok := m["constraints"].(map[string]any)
		if !ok {
			return
		}
		if _, held := c[key]; held {
			id, _ := m["id"].(string)
			homes = append(homes, id)
		}
	})
	sort.Strings(homes)
	return homes
}

// keptEntries is the sorted source pointers of every Unmodeled entry of doc
// under key, wherever it is held: a bound kept verbatim rather than placed.
func keptEntries(t *testing.T, doc *ir.Document, key string) []string {
	t.Helper()
	kept := []string{}
	walkObjects(documentTree(t, doc), func(m map[string]any) {
		held, ok := m["unmodeled"].(map[string]any)
		if !ok {
			return
		}
		entry, ok := held[key].(map[string]any)
		if !ok {
			return
		}
		prov, _ := entry["provenance"].(map[string]any)
		pointer, _ := prov["pointer"].(string)
		assert.Equal(t, "degraded_lowering", entry["reason"], "why %s is kept", key)
		kept = append(kept, pointer)
	})
	sort.Strings(kept)
	return kept
}

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

// TestCompile_ABoundBesideAValueSetOfOtherKindsIsKeptNotPlaced holds that a
// numeric bound beside a value set whose members are strings, whether the set
// is named by a $ref, by an allOf or written at the position, lands in no
// Constraints, at a component, a property and a parameter alike, and is kept
// under its own key once with one info at its position.
func TestCompile_ABoundBesideAValueSetOfOtherKindsIsKeptNotPlaced(t *testing.T) {
	t.Parallel()
	const paths = `paths:
  /p:
    get:
      parameters:
        - name: q
          in: query
          schema: {$ref: '#/components/schemas/Colors', minimum: 1}
      responses: {'204': {description: ok}}
`
	schemas := []string{
		"Colors: {enum: [red, green]}",
		"Shade: {allOf: [{$ref: '#/components/schemas/Colors'}], minimum: 1}",
		"Holder: {type: object, properties: {shade: {$ref: '#/components/schemas/Colors', minimum: 1}, own: {enum: [red, green], minimum: 1}}}",
	}
	positions := []string{
		"/components/schemas/Shade",
		"/components/schemas/Holder/properties/shade",
		"/components/schemas/Holder/properties/own",
		"/paths/~1p/get/parameters/0/schema",
	}
	inBothOrders(t, schemas, func(t *testing.T, schemas []string) {
		doc, diags := compileAnnotationSpec(t, "colors", admissionSpec("3.1.0", paths, schemas...))
		assert.Empty(t, boundHomes(t, doc, "min"), "no Constraints holds the bound")
		wantKept := make([]string, 0, len(positions))
		for _, p := range positions {
			wantKept = append(wantKept, p+"/minimum")
			assert.Equal(t, 1, infosAt(diags, diag.DegradedConstruct, p, "minimum"), "one info at %s", p)
		}
		sort.Strings(wantKept)
		assert.Equal(t, wantKept, keptEntries(t, doc, "openapi:minimum"), "the bound is kept once at each position")
		requireErrorsAndWarnings(t, diags, nil)
	})
}

// TestCompile_ABoundBesideAnInferredObjectIsKeptOnItsHome holds that a string
// bound beside a schema that is an object by its properties, written inline or
// beside a $ref to one, lands in no Constraints: the home is an object, and
// the bound is kept under its own key at its pointer.
func TestCompile_ABoundBesideAnInferredObjectIsKeptOnItsHome(t *testing.T) {
	t.Parallel()
	schemas := []string{
		"Props: {properties: {a: {type: string}}}",
		"B3: {$ref: '#/components/schemas/Props', minLength: 3}",
		"O1: {properties: {a: {type: string}}, minLength: 3}",
	}
	inBothOrders(t, schemas, func(t *testing.T, schemas []string) {
		doc, diags := compileAnnotationSpec(t, "inferred", admissionSpec("3.1.0", "", schemas...))
		assert.Empty(t, boundHomes(t, doc, "minLength"), "no Constraints holds the bound")
		assert.Equal(t, []string{"/components/schemas/B3/minLength", "/components/schemas/O1/minLength"},
			keptEntries(t, doc, "openapi:minLength"))
		requireErrorsAndWarnings(t, diags, nil)
	})
}

// TestCompile_ABoundBesideAWideAllOfOfStringsIsDecidedAtEverySize holds that a
// numeric bound beside an allOf of N references to strings is kept under its
// own key and placed in no Constraints whatever N is, either side of 256: the
// answer does not turn on how many schemas the read took in.
func TestCompile_ABoundBesideAWideAllOfOfStringsIsDecidedAtEverySize(t *testing.T) {
	t.Parallel()
	for _, n := range []int{2, 255, 256, 257, 300} {
		t.Run(fmt.Sprint(n), func(t *testing.T) {
			t.Parallel()
			entries := make([]string, 0, n)
			schemas := make([]string, 1, n+1)
			for i := range n {
				entries = append(entries, fmt.Sprintf("{$ref: '#/components/schemas/S%d'}", i))
				schemas = append(schemas, fmt.Sprintf("S%d: {type: string}", i))
			}
			schemas[0] = "Wide: {allOf: [" + strings.Join(entries, ", ") + "], minimum: 1}"
			inBothOrders(t, schemas, func(t *testing.T, schemas []string) {
				doc, diags := compileAnnotationSpec(t, "wide", admissionSpec("3.1.0", "", schemas...))
				assert.Empty(t, boundHomes(t, doc, "min"), "no Constraints holds the bound")
				assert.Equal(t, []string{"/components/schemas/Wide/minimum"}, keptEntries(t, doc, "openapi:minimum"))
				assert.Equal(t, 1, infosAt(diags, diag.DegradedConstruct, "/components/schemas/Wide", "minimum"))
				requireErrorsAndWarnings(t, diags, nil)
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
