// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test

import (
	"encoding/json/v2"
	"fmt"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/ir"
)

// admissionSpec is a spec of the OpenAPI version whose components hold the
// schemas, each one line of YAML "Name: {...}", in the order given, beside the
// paths section.
func admissionSpec(version, paths string, schemas ...string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "openapi: %s\ninfo: {title: t, version: \"1\"}\n", version)
	if paths == "" {
		paths = "paths: {}\n"
	}
	b.WriteString(paths)
	b.WriteString("components:\n  schemas:\n")
	for _, s := range schemas {
		b.WriteString("    " + s + "\n")
	}
	return b.String()
}

// inBothOrders runs check over the schemas as declared and over the same
// schemas reversed: the second order is the same document with its components
// swapped, so a check that holds for both holds whichever is visited first.
func inBothOrders(t *testing.T, schemas []string, check func(t *testing.T, schemas []string)) {
	t.Helper()
	t.Run("declared", func(t *testing.T) { check(t, schemas) })
	reversed := slices.Clone(schemas)
	slices.Reverse(reversed)
	t.Run("reversed", func(t *testing.T) { check(t, reversed) })
}

// wantFinding is one error or warning a document must draw: the code, the
// pointer it is drawn at, and words its message holds.
type wantFinding struct {
	code, pointer string
	contains      []string
}

// requireErrorsAndWarnings holds the whole list of error and warning findings
// of diags to want, as a set keyed by pointer and code: nothing more is drawn,
// and none is drawn twice. An info is the caller's to ask for.
func requireErrorsAndWarnings(t *testing.T, diags []ir.Diagnostic, want []wantFinding) {
	t.Helper()
	var got []ir.Diagnostic
	for _, diagnostic := range diags {
		if diagnostic.Severity == ir.SeverityError || diagnostic.Severity == ir.SeverityWarning {
			got = append(got, diagnostic)
		}
	}
	key := func(code, pointer string) string { return pointer + " " + code }
	sort.Slice(got, func(i, j int) bool {
		return key(got[i].Code, string(got[i].Provenance.Pointer)) < key(got[j].Code, string(got[j].Provenance.Pointer))
	})
	sort.Slice(want, func(i, j int) bool { return key(want[i].code, want[i].pointer) < key(want[j].code, want[j].pointer) })
	gotKeys, wantKeys := make([]string, 0, len(got)), make([]string, 0, len(want))
	for _, diagnostic := range got {
		gotKeys = append(gotKeys, key(diagnostic.Code, string(diagnostic.Provenance.Pointer)))
	}
	for _, wantFinding := range want {
		wantKeys = append(wantKeys, key(wantFinding.code, wantFinding.pointer))
	}
	require.Equal(t, wantKeys, gotKeys, "the errors and warnings of the document")
	for i, wantFinding := range want {
		for _, word := range wantFinding.contains {
			assert.Contains(t, got[i].Message, word, "the message at %s", wantFinding.pointer)
		}
	}
}

// documentTree is doc as its JSON encodes it, read back as maps and slices, so
// a check can ask every node, property and parameter alike where a fact sits.
func documentTree(t *testing.T, doc *ir.Document) any {
	t.Helper()
	raw, err := json.Marshal(doc)
	require.NoError(t, err)
	var tree any
	require.NoError(t, json.Unmarshal(raw, &tree))
	return tree
}

// comparableDocument is doc without what a swap of declarations moves on its
// own, the hash of the source and the order of the findings, so two orders of
// one document compare equal when they hold the same facts.
func comparableDocument(t *testing.T, doc *ir.Document) string {
	t.Helper()
	tree, ok := documentTree(t, doc).(map[string]any)
	require.True(t, ok)
	delete(tree, "sources")
	findings := []string{}
	if list, ok := tree["diagnostics"].([]any); ok {
		for _, d := range list {
			findings = append(findings, fmt.Sprint(d))
		}
	}
	sort.Strings(findings)
	delete(tree, "diagnostics")
	raw, err := json.Marshal(tree, json.Deterministic(true))
	require.NoError(t, err)
	return string(raw) + "\n" + strings.Join(findings, "\n")
}

// TestCompile_AnEmptyConjunctIsItsOwnSchemasToReport holds that an allOf whose
// conjunct admits nothing by itself draws no report of its own: the conjunct
// that contradicts itself is reported where it is written and the allOf
// naming it draws nothing more, in both orders of the components and of the
// conjuncts.
func TestCompile_AnEmptyConjunctIsItsOwnSchemasToReport(t *testing.T) {
	t.Parallel()
	for _, swapped := range []bool{false, true} {
		entries := "{$ref: '#/components/schemas/A'}, " + intRef
		if swapped {
			entries = intRef + ", {$ref: '#/components/schemas/A'}"
		}
		schemas := []string{
			"Str: {type: string}",
			"Obj: {type: object, properties: {k: {type: string}}}",
			"Int: {type: integer}",
			"B: {allOf: [" + entries + "]}",
			"A: {allOf: [" + stringRef + ", " + objRef + "]}",
		}
		t.Run(fmt.Sprintf("entries-swapped-%t", swapped), func(t *testing.T) {
			t.Parallel()
			inBothOrders(t, schemas, func(t *testing.T, schemas []string) {
				_, diags := compileAnnotationSpec(t, "empty", admissionSpec("3.1.0", "", schemas...))
				requireErrorsAndWarnings(t, diags, []wantFinding{{
					code: diag.DegradedConstruct, pointer: "/components/schemas/A", contains: []string{disjoint, "string", "object"},
				}})
			})
		})
	}
}

// TestCompile_AnAllOfCycleBesideABoundIsReadAlikeFromEveryEntry holds that two
// schemas each reaching the other through an allOf, one writing a bound, give
// the same nodes for both whichever is declared first and whichever a
// property enters the cycle through, and the same findings: a read met inside
// an open cycle is no verdict to remember for the schema it started from.
func TestCompile_AnAllOfCycleBesideABoundIsReadAlikeFromEveryEntry(t *testing.T) {
	t.Parallel()
	cycle := []string{
		"A: {allOf: [{$ref: '#/components/schemas/B'}], minLength: 1}",
		"B: {allOf: [{$ref: '#/components/schemas/A'}]}",
	}
	cycleNodes := make([]string, 0, 8)
	for _, entry := range []string{"A", "B"} {
		holder := "Holder: {type: object, properties: {p: {$ref: '#/components/schemas/" + entry + "'}}}"
		whole := make([]string, 0, 2)
		for _, order := range [][]string{cycle, {cycle[1], cycle[0]}} {
			schemas := append([]string{holder}, order...)
			doc, _ := compileAnnotationSpec(t, "cycle", admissionSpec("3.1.0", "", schemas...))
			whole = append(whole, comparableDocument(t, doc))
			for _, id := range []string{"A", "B"} {
				raw, err := json.Marshal(doc.Types[ir.TypeID("type/openapi/components/schemas/"+id)])
				require.NoError(t, err)
				cycleNodes = append(cycleNodes, id+" "+string(raw))
			}
		}
		assert.Equal(t, whole[0], whole[1], "the document does not follow the declaration order, entered at %s", entry)
	}
	for _, id := range []string{"A", "B"} {
		var of []string
		for _, n := range cycleNodes {
			if strings.HasPrefix(n, id+" ") {
				of = append(of, n)
			}
		}
		assert.Len(t, of, 4)
		assert.Len(t, slices.Compact(of), 1, "%s is one node whichever way the cycle is entered", id)
	}
}

// infosAt counts the infos of code whose pointer is at or under pointer and
// whose message holds word.
func infosAt(diags []ir.Diagnostic, code, pointer, word string) int {
	n := 0
	for _, diagnostic := range diags {
		at := string(diagnostic.Provenance.Pointer)
		if diagnostic.Severity == ir.SeverityInfo && diagnostic.Code == code &&
			(at == pointer || strings.HasPrefix(at, pointer+"/")) && strings.Contains(diagnostic.Message, word) {
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
		// A parameter keeps what its schema writes under the schema's scope
		// (docs/ir/unmodeled.md#keys), so the last position's key is scoped.
		atSchemas := wantKept[:len(wantKept)-1]
		sort.Strings(atSchemas)
		assert.Equal(t, atSchemas, keptEntries(t, doc, "openapi:minimum"), "the bound is kept once at each schema")
		assert.Equal(t, wantKept[len(wantKept)-1:], keptEntries(t, doc, "openapi:schema/minimum"),
			"and once at the parameter")
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
// own key and placed in no Constraints whatever N is, either side of 256, and
// at 3.0 as at 3.1: the answer does not turn on how many schemas the read
// took in.
func TestCompile_ABoundBesideAWideAllOfOfStringsIsDecidedAtEverySize(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		version string
		n       int
	}{{"3.1.0", 2}, {"3.1.0", 255}, {"3.1.0", 256}, {"3.1.0", 257}, {"3.1.0", 300}, {"3.0.3", 257}} {
		version, n := tc.version, tc.n
		t.Run(fmt.Sprint(version, " ", n), func(t *testing.T) {
			t.Parallel()
			entries := make([]string, 0, n)
			schemas := make([]string, 1, n+1)
			for i := range n {
				entries = append(entries, fmt.Sprintf("{$ref: '#/components/schemas/S%d'}", i))
				schemas = append(schemas, fmt.Sprintf("S%d: {type: string}", i))
			}
			schemas[0] = "Wide: {allOf: [" + strings.Join(entries, ", ") + "], minimum: 1}"
			inBothOrders(t, schemas, func(t *testing.T, schemas []string) {
				doc, diags := compileAnnotationSpec(t, "wide", admissionSpec(version, "", schemas...))
				assert.Empty(t, boundHomes(t, doc, "min"), "no Constraints holds the bound")
				assert.Equal(t, []string{"/components/schemas/Wide/minimum"}, keptEntries(t, doc, "openapi:minimum"))
				assert.Equal(t, 1, infosAt(diags, diag.DegradedConstruct, "/components/schemas/Wide", "minimum"))
				requireErrorsAndWarnings(t, diags, nil)
			})
		})
	}
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
		"NullAndString: {allOf: [{$ref: '#/components/schemas/Nil'}, " + stringRef + "]}",
		"NullAliasAndString: {allOf: [{$ref: '#/components/schemas/NilAlias'}, " + stringRef + "]}",
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
		for _, diagnostic := range diags {
			if diagnostic.Severity == ir.SeverityWarning {
				texts = append(texts, diagnostic.Message)
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
	model, ok := doc.Types[ir.TypeID(id)].(*ir.Model)
	require.True(t, ok, "%s lowers to a model", id)
	conflicts := 0
	for _, diagnostic := range diags {
		if diagnostic.Code == diag.ConflictingRedecl && strings.HasPrefix(string(diagnostic.Provenance.Pointer), pointer+"/") {
			conflicts++
		}
	}
	for _, property := range model.Properties {
		if property.WireName == wire {
			return property.Type.Target, conflicts
		}
	}
	require.Failf(t, "no such property", "%s has no property %q", id, wire)
	return "", conflicts
}

// TestCompile_AFieldRedeclaredAsAStructureAdmittingItsKindIsNotAConflict holds
// that a field declared integer in one allOf branch and in the other as a
// structure over a schema stating no type, which admits every integer, keeps
// integer with nothing reported, in both branch orders, while a structure
// refusing only strings through a not stays reported as before.
func TestCompile_AFieldRedeclaredAsAStructureAdmittingItsKindIsNotAConflict(t *testing.T) {
	t.Parallel()
	for _, swapped := range []bool{false, true} {
		schemas := []string{
			"Obj: {properties: {b: {type: string}}}",
			"NotStr: {not: {type: string}}",
			"U: " + redeclaredAsStructure("{allOf: [{$ref: '#/components/schemas/Obj'}], properties: {a: {type: string}}}", swapped),
			"T: " + redeclaredAsStructure("{allOf: [{$ref: '#/components/schemas/NotStr'}], properties: {a: {type: string}}}", swapped),
		}
		t.Run(fmt.Sprintf("branches-swapped-%t", swapped), func(t *testing.T) {
			t.Parallel()
			inBothOrders(t, schemas, func(t *testing.T, schemas []string) {
				doc, diags := compileAnnotationSpec(t, "redecl", admissionSpec("3.1.0", "", schemas...))
				requireStructure(t, doc, "U", swapped)
				target, conflicts := redeclaredField(t, doc, diags, "type/openapi/components/schemas/U", "f", "/components/schemas/U")
				assert.Equal(t, ir.TypeID("type/primitive/integer"), target, "U's field takes the narrower, integer")
				assert.Zero(t, conflicts, "no conflict is reported for U")
				assert.Empty(t, keptEntries(t, doc, "openapi:conflicting-redeclaration/components/schemas/U/allOf/0/properties/f"))

				_, conflicts = redeclaredField(t, doc, diags, "type/openapi/components/schemas/T", "f", "/components/schemas/T")
				assert.Equal(t, 1, conflicts, "T keeps its one conflict: a not refuses what the read cannot follow")
			})
		})
	}
}

// TestCompile_AFieldRedeclaredAsAUseOfAUnionHoldingItsKindKeepsTheKind holds
// that a field declared integer in one allOf branch and in the other as an
// allOf naming a union of an object and an integer, with properties beside it,
// keeps integer with nothing reported, in both branch orders: that declaration
// is no structure but a use of the union (docs/compilers/openapi.md#allof-refinement),
// and integer is within one of its variants.
func TestCompile_AFieldRedeclaredAsAUseOfAUnionHoldingItsKindKeepsTheKind(t *testing.T) {
	t.Parallel()
	for _, swapped := range []bool{false, true} {
		schemas := []string{
			"ObjOrInt: {anyOf: [{type: object}, {type: integer}]}",
			"U: " + redeclaredAsStructure("{allOf: [{$ref: '#/components/schemas/ObjOrInt'}], properties: {a: {type: string}}}", swapped),
		}
		t.Run(fmt.Sprintf("branches-swapped-%t", swapped), func(t *testing.T) {
			t.Parallel()
			inBothOrders(t, schemas, func(t *testing.T, schemas []string) {
				doc, diags := compileAnnotationSpec(t, "redecl", admissionSpec("3.1.0", "", schemas...))
				assert.NotContains(t, doc.Types, redeclaredNode("U", swapped), "the declaration is a use, not a node of its own")
				target, conflicts := redeclaredField(t, doc, diags, "type/openapi/components/schemas/U", "f", "/components/schemas/U")
				assert.Equal(t, ir.TypeID("type/primitive/integer"), target, "U's field takes the narrower, integer")
				assert.Zero(t, conflicts, "no conflict is reported for U")
			})
		})
	}
}

// redeclaredAsStructure is an allOf declaring f integer in one branch and as
// declaration in the other, the integer first unless swapped.
func redeclaredAsStructure(declaration string, swapped bool) string {
	a, b := "{properties: {f: {type: integer}}}", "{properties: {f: "+declaration+"}}"
	if swapped {
		a, b = b, a
	}
	return "{allOf: [" + a + ", " + b + "]}"
}

// redeclaredNode is the ID of the node the declaration of f other than
// integer would own in the component name (redeclaredAsStructure).
func redeclaredNode(name string, swapped bool) ir.TypeID {
	branch := "1"
	if swapped {
		branch = "0"
	}
	return ir.TypeID("type/anonymous/components/schemas/" + name + "/allOf/" + branch + "/properties/f")
}

// requireStructure requires the declaration of f other than integer in the
// component name to lower to a Model, the structure the exception reads.
func requireStructure(t *testing.T, doc *ir.Document, name string, swapped bool) {
	t.Helper()
	_, isModel := doc.Types[redeclaredNode(name, swapped)].(*ir.Model)
	require.True(t, isModel, "the redeclaration of %s's f must be a structure", name)
}

// TestCompile_ANullNotOnAStructuresEntryIsTheKeywordLeftOut holds that a field
// redeclared as a structure over a schema stating no type, whose allOf writes
// a null not, is judged as the structure without the not, which the
// validation table warns of once: the field keeps integer, and the only error
// or warning is that one.
func TestCompile_ANullNotOnAStructuresEntryIsTheKeywordLeftOut(t *testing.T) {
	t.Parallel()
	for _, swapped := range []bool{false, true} {
		schemas := []string{
			"Obj: {properties: {b: {type: string}}}",
			"V: " + redeclaredAsStructure("{allOf: [{$ref: '#/components/schemas/Obj'}, {not: null}], properties: {a: {type: string}}}", swapped),
		}
		t.Run(fmt.Sprintf("branches-swapped-%t", swapped), func(t *testing.T) {
			t.Parallel()
			inBothOrders(t, schemas, func(t *testing.T, schemas []string) {
				doc, diags := compileAnnotationSpec(t, "nullnot", admissionSpec("3.1.0", "", schemas...))
				requireStructure(t, doc, "V", swapped)
				target, conflicts := redeclaredField(t, doc, diags, "type/openapi/components/schemas/V", "f", "/components/schemas/V")
				assert.Equal(t, ir.TypeID("type/primitive/integer"), target)
				assert.Zero(t, conflicts)
				var found []string
				for _, diagnostic := range diags {
					if diagnostic.Severity == ir.SeverityWarning {
						found = append(found, diagnostic.Code)
					}
				}
				assert.Equal(t, []string{"openapi/validation/validation-type-mismatch"}, found,
					"the null not is reported by the load phase alone")
			})
		})
	}
}

const (
	stringRef = "{$ref: '#/components/schemas/Str'}"
	objRef    = "{$ref: '#/components/schemas/Obj'}"
	intRef    = "{$ref: '#/components/schemas/Int'}"
	disjoint  = "share no value"
)
