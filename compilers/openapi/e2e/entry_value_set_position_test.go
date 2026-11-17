// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test

import (
	"encoding/json/jsontext"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
)

// entryRef is the $ref conjunct a value set narrows in the cases below.
const entryRef = "{$ref: '#/components/schemas/T'}"

// entryValueSet is a value set a position writes, and what it lowers to where
// it is the position's own.
type entryValueSet struct {
	name    string
	target  string // the component T the $ref names, one line of YAML
	keyword string // the value set as one YAML member
	only31  bool   // the keyword is a 3.1 one
	kind    ir.TypeKind
	check   func(t *testing.T, doc *ir.Document, node ir.TypeDef)
}

// entryValueSets are the value sets the cases write: a string enum and const
// over a string target, an enum outside the values its target holds, and an
// enum and a const of objects over an object target.
func entryValueSets() []entryValueSet {
	str := "T: {type: string}"
	obj := "T: {type: object, properties: {a: {type: string}}}"
	return []entryValueSet{
		{name: "enum", target: str, keyword: "enum: [a]", kind: ir.KindEnum, check: requireEntryEnum("a")},
		{name: "const", target: str, keyword: "const: a", only31: true, kind: ir.KindLiteral, check: requireEntryLiteral("a")},
		{
			name: "enum outside the target", target: "T: {type: string, enum: [low, mid]}", keyword: "enum: [other]",
			kind: ir.KindEnum, check: requireEntryEnum("other"),
		},
		{name: "object enum", target: obj, keyword: "enum: [{a: x}]", kind: ir.KindUnion, check: requireEntryObjectUnion},
		{name: "object const", target: obj, keyword: "const: {a: x}", only31: true, kind: ir.KindLiteral, check: requireEntryObjectLiteral},
	}
}

// requireEntryEnum holds a node to be an enum of the one string member given.
func requireEntryEnum(member string) func(t *testing.T, doc *ir.Document, node ir.TypeDef) {
	return func(t *testing.T, _ *ir.Document, node ir.TypeDef) {
		t.Helper()
		enum, ok := node.(*ir.Enum)
		require.True(t, ok, "an enum")
		require.Len(t, enum.Members, 1)
		assert.Equal(t, member, enum.Members[0].Value.Str)
		assert.Equal(t, ir.PrimString, enum.ValueType)
	}
}

// requireEntryLiteral holds a node to be a literal of the string given.
func requireEntryLiteral(value string) func(t *testing.T, doc *ir.Document, node ir.TypeDef) {
	return func(t *testing.T, _ *ir.Document, node ir.TypeDef) {
		t.Helper()
		literal, ok := node.(*ir.Literal)
		require.True(t, ok, "a literal")
		assert.Equal(t, value, literal.Value.Str)
		assert.Equal(t, ir.PrimString, literal.ValueType)
	}
}

// requireEntryObjectLiteral holds a node to be the literal {a: x}.
func requireEntryObjectLiteral(t *testing.T, _ *ir.Document, node ir.TypeDef) {
	t.Helper()
	literal, ok := node.(*ir.Literal)
	require.True(t, ok, "a literal")
	assert.Equal(t, ir.ValueObject, literal.Value.Kind)
	require.Len(t, literal.Value.Object, 1)
	assert.Equal(t, "a", literal.Value.Object[0].Name)
	assert.Equal(t, "x", literal.Value.Object[0].Value.Str)
}

// requireEntryObjectUnion holds a node to be the union of the one literal
// {a: x}, which is how an enum of objects lowers.
func requireEntryObjectUnion(t *testing.T, doc *ir.Document, node ir.TypeDef) {
	t.Helper()
	union, ok := node.(*ir.Union)
	require.True(t, ok, "a union of literals")
	require.Len(t, union.Variants, 1)
	requireEntryObjectLiteral(t, doc, kindNode(t, doc, union.Variants[0].Type.Target))
}

// entryIndexAnywhere matches the entry of an allOf a pointer passes through.
var entryIndexAnywhere = regexp.MustCompile(`/allOf/[0-9]+`)

// entryIndexBeneath matches the entry of an allOf a pointer passes through
// to something written beneath it.
var entryIndexBeneath = regexp.MustCompile(`/allOf/[0-9]+/`)

// entryPositionID is the ID of the node a schema written at at owns: the
// component's own for a component, the anonymous one at the pointer for any
// other position.
func entryPositionID(at jsontext.Pointer) ir.TypeID {
	if name, ok := strings.CutPrefix(string(at), "/components/schemas/"); ok && !strings.Contains(name, "/") {
		return componentID(name)
	}
	return ir.TypeID("t/anon" + string(at))
}

// entryTree is doc as its JSON encodes it, without the hash of its source and
// with each finding rendered as a line, sorted, and every pointer in a value
// written as if no allOf entry stood on it: two documents compiled from one
// conjunction spelt another way then compare equal where they hold the same
// facts. A node keeps the pointer it is written at beneath an entry, so a key
// loses the entries it passes through, but one ending at an entry is left as
// it is, and a node standing at an entry still differs.
func entryTree(t *testing.T, doc *ir.Document) string {
	t.Helper()
	tree, ok := documentTree(t, doc).(map[string]any)
	require.True(t, ok, "a document encodes as an object")
	delete(tree, "sources")
	var findings []string
	if list, ok := tree["diagnostics"].([]any); ok {
		for _, d := range list {
			findings = append(findings, fmt.Sprint(entryIndexless(d)))
		}
	}
	slices.Sort(findings)
	delete(tree, "diagnostics")
	return fmt.Sprint(entryIndexless(tree)) + "\n" + strings.Join(findings, "\n")
}

// entryIndexless is v with every allOf entry its strings name left out, and
// every one its keys pass through, maps and slices copied.
func entryIndexless(v any) any {
	switch v := v.(type) {
	case string:
		return entryIndexAnywhere.ReplaceAllString(v, "")
	case []any:
		out := make([]any, len(v))
		for i, e := range v {
			out[i] = entryIndexless(e)
		}
		return out
	case map[string]any:
		out := make(map[string]any, len(v))
		for k, e := range v {
			out[entryIndexBeneath.ReplaceAllString(k, "/")] = entryIndexless(e)
		}
		return out
	default:
		return v
	}
}

// requireEntryModelOf holds the component named to be a Model and returns it.
func requireEntryModelOf(t *testing.T, doc *ir.Document, name string) *ir.Model {
	t.Helper()
	model, ok := kindNode(t, doc, componentID(name)).(*ir.Model)
	require.True(t, ok, "%s is a model", name)
	return model
}

// requireEntryScalarOver holds the component named to be a Scalar whose Base
// is target, and returns it.
func requireEntryScalarOver(t *testing.T, doc *ir.Document, name string, target ir.TypeID) *ir.Scalar {
	t.Helper()
	scalar, ok := kindNode(t, doc, componentID(name)).(*ir.Scalar)
	require.True(t, ok, "%s is a use of its target", name)
	require.NotNil(t, scalar.Base, name)
	assert.Equal(t, target, scalar.Base.Target, name)
	return scalar
}

// entrySpelling is one way to write a value set that narrows a $ref.
type entrySpelling struct {
	name   string
	schema string
	ref    int // the index of the allOf entry holding the $ref, -1 where it is written on the position
}

// entrySpellings writes keyword beside a $ref in an allOf's entry, whether
// ahead of it or after it or behind another, and beside the allOf.
func entrySpellings(keyword string) []entrySpelling {
	return []entrySpelling{
		{name: "entry after the ref", schema: "{allOf: [" + entryRef + ", {" + keyword + "}]}", ref: 0},
		{name: "entry before the ref", schema: "{allOf: [{" + keyword + "}, " + entryRef + "]}", ref: 1},
		{name: "entry behind another", schema: "{allOf: [{description: d}, " + entryRef + ", {" + keyword + "}]}", ref: 1},
		{name: "beside the allOf", schema: "{allOf: [" + entryRef + "], " + keyword + "}", ref: 0},
	}
}

// entryVersions are the versions of the cases, a 3.1 keyword's among them
// where the case names one.
func entryVersions(vs entryValueSet) []string {
	if vs.only31 {
		return []string{"3.1.0"}
	}
	return []string{"3.0.3", "3.1.0"}
}

// requireEntryNodeAt holds what a position writing a value set over a $ref
// lowers to, where its schema is written at at: the one node the carrier
// names stands at the position, holds the value set, and holds the $ref the
// set narrows under openapi:$ref, which no other node holds and one info
// reports at the entry writing it.
func requireEntryNodeAt(t *testing.T, doc *ir.Document, diags []ir.Diagnostic, at jsontext.Pointer, vs entryValueSet, ref int) {
	t.Helper()
	id := usedAt(t, doc, at)
	require.Equal(t, entryPositionID(at), id, "the node the position names stands at the position")
	node := kindNode(t, doc, id)
	assert.Equal(t, vs.kind, node.Kind(), at)
	assert.Equal(t, at, node.Common().Provenance.Pointer, "the node is lowered from the position")
	vs.check(t, doc, node)

	assert.Equal(t, entryKept("openapi:$ref"), keptOn(node), "what the node keeps at %s", at)
	entry, held := node.Common().Unmodeled["openapi:$ref"]
	require.True(t, held, at)
	assert.JSONEq(t, `{"$ref": "#/components/schemas/T"}`, string(entry.Value))
	assert.Len(t, entryKeptUnder(doc, at), 1, "no other node holds what the position keeps at %s", at)
	if ref < 0 {
		return
	}
	refAt := at + jsontext.Pointer(fmt.Sprintf("/allOf/%d", ref))
	assert.Equal(t, refAt, entry.Provenance.Pointer, "the entry kept is the one writing the $ref")
	n := 0
	for _, p := range degradedUnder(diags, at) {
		if p == refAt {
			n++
		}
	}
	assert.Equal(t, 1, n, "one info at the $ref the value set narrows, at %s", at)
}

// TestEntryValueSet_TheNodeStandsAtThePositionInEverySpelling holds that where
// a value set narrows a $ref, the enum or literal is the position's own node,
// whether the value set is written beside the allOf or in an entry ahead of
// the $ref, after it or behind another: at a component, a property, a list's
// element and a parameter's schema its ID and its pointer are the position's,
// the kept $ref is on it and on no other node, and no type of the document
// stands at an entry of an allOf.
func TestEntryValueSet_TheNodeStandsAtThePositionInEverySpelling(t *testing.T) {
	t.Parallel()
	for _, vs := range entryValueSets() {
		for _, version := range entryVersions(vs) {
			for _, sp := range entrySpellings(vs.keyword) {
				t.Run(fmt.Sprintf("%s/%s/%s", version, vs.name, sp.name), func(t *testing.T) {
					t.Parallel()
					inBothMappingOrders(t, conjunctionSpec(version, sp.schema, vs.target), func(t *testing.T, spec string) {
						doc, diags := compileAnnotationSpec(t, "stands", spec)
						requireErrorsAndWarnings(t, diags, nil)
						requireNoEntryIndexedType(t, doc)
						for _, at := range conjunctionPositions {
							requireEntryNodeAt(t, doc, diags, at, vs, sp.ref)
						}
					})
				})
			}
		}
	}
}

// TestEntryValueSet_TheEntryOrderLeavesTheDocumentAlone holds that the same
// value set and $ref written in the one order and in the other compile to one
// document but for the index each entry is written at: the nodes keep their
// IDs, since none stands at an entry, and what is kept is keyed by what it is.
func TestEntryValueSet_TheEntryOrderLeavesTheDocumentAlone(t *testing.T) {
	t.Parallel()
	for _, vs := range entryValueSets() {
		for _, version := range entryVersions(vs) {
			t.Run(fmt.Sprintf("%s/%s", version, vs.name), func(t *testing.T) {
				t.Parallel()
				after := entrySpellings(vs.keyword)[0]
				before := entrySpellings(vs.keyword)[1]
				first, _ := compileAnnotationSpec(t, "order", conjunctionSpec(version, after.schema, vs.target))
				second, _ := compileAnnotationSpec(t, "order", conjunctionSpec(version, before.schema, vs.target))
				assert.Equal(t, entryTree(t, first), entryTree(t, second), "the entry order decided the document")
			})
		}
	}
}

// TestEntryValueSet_TheWrapAndTheEntrySpellingsCompileAlike holds that a value
// set written beside an allOf over a $ref and one written in an entry after
// the $ref compile to one document, since both are the one conjunction.
func TestEntryValueSet_TheWrapAndTheEntrySpellingsCompileAlike(t *testing.T) {
	t.Parallel()
	for _, vs := range entryValueSets() {
		for _, version := range entryVersions(vs) {
			t.Run(fmt.Sprintf("%s/%s", version, vs.name), func(t *testing.T) {
				t.Parallel()
				spellings := entrySpellings(vs.keyword)
				entry, _ := compileAnnotationSpec(t, "alike", conjunctionSpec(version, spellings[0].schema, vs.target))
				wrap, _ := compileAnnotationSpec(t, "alike", conjunctionSpec(version, spellings[3].schema, vs.target))
				assert.Equal(t, entryTree(t, wrap), entryTree(t, entry), "the spelling decided the document")
			})
		}
	}
}

// TestEntryValueSet_AReferenceToThePositionReadsItsNode holds that a $ref to
// the pointer of a position holding a narrowing, and a $ref to the component
// holding one, name the enum standing at that position, as a use does, in
// either order of the components.
func TestEntryValueSet_AReferenceToThePositionReadsItsNode(t *testing.T) {
	t.Parallel()
	const at = "/components/schemas/Holder/properties/p"
	for _, version := range []string{"3.0.3", "3.1.0"} {
		t.Run(version, func(t *testing.T) {
			t.Parallel()
			inBothOrders(t, []string{
				"T: {type: string}",
				"Comp: {allOf: [" + entryRef + ", {enum: [a]}]}",
				"Holder: {type: object, properties: {p: {allOf: [" + entryRef + ", {enum: [a]}]}}}",
				"ToPosition: {$ref: '#/components/schemas/Holder/properties/p'}",
				"ToComp: {$ref: '#/components/schemas/Comp'}",
				"Uses: {type: object, properties: {q: {$ref: '#/components/schemas/Holder/properties/p'}, r: {$ref: '#/components/schemas/Comp'}}}",
			}, func(t *testing.T, schemas []string) {
				doc, diags := compileAnnotationSpec(t, "reads", admissionSpec(version, "", schemas...))
				requireErrorsAndWarnings(t, diags, nil)
				requireNoEntryIndexedType(t, doc)

				position := entryPositionID(at)
				assert.Equal(t, ir.KindEnum, kindNode(t, doc, position).Kind(), "the position's node")
				assert.Equal(t, ir.KindEnum, kindNode(t, doc, componentID("Comp")).Kind(), "the component is the enum")
				holder := requireEntryModelOf(t, doc, "Holder")
				p, ok := propByWire(holder, "p")
				require.True(t, ok)
				assert.Equal(t, position, p.Type.Target)
				assert.Empty(t, p.Unmodeled, "the property keeps nothing the enum holds")

				requireEntryScalarOver(t, doc, "ToPosition", position)
				requireEntryScalarOver(t, doc, "ToComp", componentID("Comp"))
				uses := requireEntryModelOf(t, doc, "Uses")
				q, _ := propByWire(uses, "q")
				r, _ := propByWire(uses, "r")
				assert.Equal(t, position, q.Type.Target)
				assert.Equal(t, componentID("Comp"), r.Type.Target)
			})
		})
	}
}

// TestEntryValueSet_AReferenceToAnEntryNamesTheEntryAlone holds that a $ref
// to an entry of an allOf names that entry as a schema of its own, an enum of
// the entry's values with the $ref the position narrows not among them, and
// that it is a second node beside the position's, with an ID of its own, in
// whichever order the entries are written.
func TestEntryValueSet_AReferenceToAnEntryNamesTheEntryAlone(t *testing.T) {
	t.Parallel()
	for _, version := range []string{"3.0.3", "3.1.0"} {
		for _, swapped := range []bool{false, true} {
			list, enumAt := "["+entryRef+", {enum: [a]}]", 1
			if swapped {
				list, enumAt = "[{enum: [a]}, "+entryRef+"]", 0
			}
			t.Run(fmt.Sprintf("%s-swapped-%t", version, swapped), func(t *testing.T) {
				t.Parallel()
				inBothOrders(t, []string{
					"T: {type: string}",
					"Comp: {allOf: " + list + "}",
					"ToEntry: {$ref: '#/components/schemas/Comp/allOf/" + fmt.Sprint(enumAt) + "'}",
				}, func(t *testing.T, schemas []string) {
					doc, _ := compileAnnotationSpec(t, "entry", admissionSpec(version, "", schemas...))

					position := kindNode(t, doc, componentID("Comp"))
					assert.Equal(t, ir.KindEnum, position.Kind(), "the position is the enum")
					assert.Contains(t, keptOn(position), "openapi:$ref")

					entry := ir.TypeID(fmt.Sprintf("t/anon/components/schemas/Comp/allOf/%d", enumAt))
					alias := requireEntryScalarOver(t, doc, "ToEntry", entry)
					require.NotNil(t, alias)
					node := kindNode(t, doc, entry)
					assert.NotEqual(t, componentID("Comp"), entry, "the entry is a node of its own")
					assert.Equal(t, ir.KindEnum, node.Kind())
					assert.Empty(t, keptOn(node), "the entry holds what it writes and nothing narrowed")
				})
			})
		}
	}
}

// TestEntryValueSet_SeveralRefsAreKeptByTheirReferenceText holds that where a
// value set narrows several $refs, each is kept under openapi:$ref/<i> with i
// the rank of its reference text, so the keys and their values are the same
// whichever order the $refs, the value set and the components are written in.
func TestEntryValueSet_SeveralRefsAreKeptByTheirReferenceText(t *testing.T) {
	t.Parallel()
	const (
		a = "{$ref: '#/components/schemas/A'}"
		b = "{$ref: '#/components/schemas/B'}"
	)
	for _, version := range []string{"3.0.3", "3.1.0"} {
		for name, schema := range map[string]string{
			"entries a b":  "{allOf: [" + a + ", " + b + ", {enum: [a]}]}",
			"entries b a":  "{allOf: [" + b + ", " + a + ", {enum: [a]}]}",
			"entries last": "{allOf: [{enum: [a]}, " + b + ", " + a + "]}",
			"beside a b":   "{allOf: [" + a + ", " + b + "], enum: [a]}",
			"beside b a":   "{allOf: [" + b + ", " + a + "], enum: [a]}",
		} {
			t.Run(fmt.Sprintf("%s/%s", version, name), func(t *testing.T) {
				t.Parallel()
				inBothOrders(t, []string{
					"A: {type: string}",
					"B: {type: string, maxLength: 5}",
					"Pos: " + schema,
				}, func(t *testing.T, schemas []string) {
					doc, diags := compileAnnotationSpec(t, "refs", admissionSpec(version, "", schemas...))
					requireErrorsAndWarnings(t, diags, nil)
					requireNoEntryIndexedType(t, doc)

					node := kindNode(t, doc, componentID("Pos"))
					assert.Equal(t, ir.KindEnum, node.Kind())
					assert.Equal(t, entryKept("openapi:$ref/0", "openapi:$ref/1"), keptOn(node))
					first := unmodeledEntry(t, node.Common().Unmodeled, "openapi:$ref/0")
					second := unmodeledEntry(t, node.Common().Unmodeled, "openapi:$ref/1")
					assert.JSONEq(t, `{"$ref": "#/components/schemas/A"}`, string(first.Value))
					assert.JSONEq(t, `{"$ref": "#/components/schemas/B"}`, string(second.Value))
				})
			})
		}
	}
}

// TestEntryValueSet_AnObjectValueSetIsElectedBeforeTheComposition holds that
// a position whose $ref names an object and whose value set is of objects
// lowers to the value set in every spelling: a union of literals for an enum
// and a literal for a const at the position, never a model whose base is the
// target, the $ref kept under openapi:$ref as for a scalar.
func TestEntryValueSet_AnObjectValueSetIsElectedBeforeTheComposition(t *testing.T) {
	t.Parallel()
	for _, vs := range entryValueSets() {
		if vs.target != entryValueSets()[3].target {
			continue
		}
		for _, version := range entryVersions(vs) {
			for _, sp := range entrySpellings(vs.keyword) {
				t.Run(fmt.Sprintf("%s/%s/%s", version, vs.name, sp.name), func(t *testing.T) {
					t.Parallel()
					doc, _ := compileAnnotationSpec(t, "object", conjunctionSpec(version, sp.schema, vs.target))
					for _, at := range conjunctionPositions {
						node := kindNode(t, doc, usedAt(t, doc, at))
						assert.NotEqual(t, ir.KindModel, node.Kind(), "a model at %s", at)
						assert.NotContains(t, keptOn(node), "openapi:allOf", at)
						assert.Contains(t, keptOn(node), "openapi:$ref", at)
					}
					assert.Equal(t, ir.KindModel, kindNode(t, doc, componentID("T")).Kind(), "the target is still a model")
				})
			}
		}
	}
}
