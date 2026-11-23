// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test

import (
	"encoding/json/v2"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/ir"
)

// exampleOutsideType is the code pass.Validate reports an example its
// position's type does not admit under.
const exampleOutsideType = "pass/example-outside-type"

// foldedExampleSpec is a document whose Coll composes Base, declaring
// result_info as base, and an entry redeclaring it as inline. inlineFirst
// writes that entry before the $ref, and collFirst writes Coll before the
// schemas it names, so the four layouts declare the same in every order.
func foldedExampleSpec(base, inline string, inlineFirst, collFirst bool) string {
	entries := []string{
		"        - {$ref: '#/components/schemas/Base'}\n",
		"        - {properties: {result_info: " + inline + "}}\n",
	}
	if inlineFirst {
		entries[0], entries[1] = entries[1], entries[0]
	}
	coll := "    Coll:\n      allOf:\n" + entries[0] + entries[1]
	named := "    Info: {type: object, properties: {count: {type: integer}, page: {type: integer}}}\n" +
		"    Base: {type: object, properties: {result_info: " + base + "}}\n"
	if collFirst {
		return openapitest.ComponentSpec(coll + named)
	}
	return openapitest.ComponentSpec(named + coll)
}

// collField compiles src with the default passes and returns Coll's
// result_info, what the run reports, and the document.
func collField(t *testing.T, src string) (ir.Property, []ir.Diagnostic, *ir.Document) {
	t.Helper()
	doc, diags := parseVerified(t, src)
	require.NotNil(t, doc)
	model, ok := typeByName(doc, "Coll").(*ir.Model)
	require.True(t, ok, "Coll is a model")
	field, ok := propertyByWire(model, "result_info")
	require.True(t, ok, "Coll holds result_info")
	return field, diags, doc
}

// examplesJSON renders examples as JSON, so a comparison reads values rather
// than pointers.
func examplesJSON(t *testing.T, examples []ir.Example) string {
	t.Helper()
	raw, err := json.Marshal(examples, json.Deterministic(true))
	require.NoError(t, err)
	return string(raw)
}

// codeCounts counts diags by code.
func codeCounts(diags []ir.Diagnostic) map[string]int {
	codes := map[string]int{}
	for _, diagnostic := range diags {
		codes[diagnostic.Code]++
	}
	return codes
}

// TestAllOfFold_AnExampleOfTheFieldsOwnDeclarationIsTyped pins that an example
// a model's own allOf entry writes beside a redeclared field lands typed on the
// field where the field is a model, whose values the merge has no judge for,
// and takes no other example: written beside a bare object, an object with
// properties of its own, or a second inline object, and in every order of the
// entries and of the components. The field keeps no verbatim copy and the fold
// reports nothing, while pass/example-outside-type holds the example to the
// field's type, so one of another kind is typed and reported there, once
// (docs/compilers/openapi.md#allof-redeclared-property).
func TestAllOfFold_AnExampleOfTheFieldsOwnDeclarationIsTyped(t *testing.T) {
	t.Parallel()
	const ref = "{$ref: '#/components/schemas/Info'}"
	for _, tc := range []struct {
		name, base, inline, want string
		outside                  int
	}{
		{"a bare object over a reference", ref, "{type: object, example: {count: 1, page: 1}}",
			`[{"value":{"kind":"object","object":[{"name":"count","value":{"kind":"number","number":"1"}},` +
				`{"name":"page","value":{"kind":"number","number":"1"}}]}}]`, 0},
		{"an object with properties over a reference", ref,
			"{type: object, properties: {count: {type: integer}}, example: {count: 1}}",
			`[{"value":{"kind":"object","object":[{"name":"count","value":{"kind":"number","number":"1"}}]}}]`, 0},
		{"a bare object over an inline object", "{type: object, properties: {count: {type: integer}}}",
			"{type: object, example: {count: 1}}",
			`[{"value":{"kind":"object","object":[{"name":"count","value":{"kind":"number","number":"1"}}]}}]`, 0},
		{"an example of another kind", ref, "{type: object, example: abc}",
			`[{"value":{"kind":"string","string":"abc"}}]`, 1},
	} {
		for _, layout := range [][2]bool{{false, false}, {true, false}, {false, true}, {true, true}} {
			field, diags, _ := collField(t, foldedExampleSpec(tc.base, tc.inline, layout[0], layout[1]))
			label := tc.name + " " + map[bool]string{false: "ref first", true: "entry first"}[layout[0]] +
				map[bool]string{false: ", Coll last", true: ", Coll first"}[layout[1]]

			assert.Equal(t, tc.want, examplesJSON(t, field.Examples), "%s: the example is typed on the field", label)
			assert.Empty(t, field.Unmodeled, "%s: and kept nowhere verbatim", label)
			want := map[string]int{}
			if tc.outside > 0 {
				want[exampleOutsideType] = tc.outside
			}
			assert.Equal(t, want, codeCounts(diags), "%s: the fold reports nothing, and the pass holds the example to the field's type", label)
		}
	}
}

// TestAllOfFold_AnExampleTheFieldTakesKeepsAnotherOut pins that an example the
// field takes keeps out one it refuses, in either order of the entries: of an
// integer's and a number's, the example the integer type refuses is kept
// verbatim at its declaration, never put on the field over the one it takes.
func TestAllOfFold_AnExampleTheFieldTakesKeepsAnotherOut(t *testing.T) {
	t.Parallel()
	integer := "        - {properties: {result_info: {type: integer, example: 1}}}\n"
	number := "        - {properties: {result_info: {type: number, example: 1.5}}}\n"
	for name, tc := range map[string]struct {
		entries, refused string
	}{
		"the integer's first": {integer + number, "/allOf/1/properties/result_info"},
		"the integer's last":  {number + integer, "/allOf/0/properties/result_info"},
	} {
		field, diags, _ := collField(t, openapitest.ComponentSpec("    Coll:\n      allOf:\n"+tc.entries))

		assert.Equal(t, `[{"value":{"kind":"number","number":"1"}}]`, examplesJSON(t, field.Examples), "%s: the example the field takes", name)
		require.Len(t, field.Unmodeled, 1, name)
		entry := unmodeledEntry(t, field.Unmodeled, "openapi:conflicting-redeclaration/components/schemas/Coll"+tc.refused)
		assert.Contains(t, string(entry.Value), "1.5", "%s: the refused one is kept at its declaration", name)
		assert.Equal(t, 1, countCode(diags, diag.DegradedConstruct, "/components/schemas/Coll"+tc.refused), name)
		assert.Zero(t, codeCounts(diags)[exampleOutsideType], "%s: the field holds no example outside its type", name)
	}
}

// TestAllOfFold_AnExampleAnotherModelDeclaresStaysThere pins that an example
// a composed model declares beside the field, which the field's type is not
// shown to admit, stays typed on that model and is not put on the composing
// model's field, whichever entry comes first: the model holding it still
// declares it.
func TestAllOfFold_AnExampleAnotherModelDeclaresStaysThere(t *testing.T) {
	t.Parallel()
	for _, inlineFirst := range []bool{false, true} {
		field, _, doc := collField(t, foldedExampleSpec("{type: object, example: {count: 2}}",
			"{$ref: '#/components/schemas/Info'}", inlineFirst, false))

		assert.Empty(t, field.Examples, "entry first %v: the composing field adopts no example", inlineFirst)
		assert.Empty(t, field.Unmodeled, "entry first %v", inlineFirst)
		base, ok := typeByName(doc, "Base").(*ir.Model)
		require.True(t, ok)
		declared, ok := propertyByWire(base, "result_info")
		require.True(t, ok)
		assert.Equal(t, `[{"value":{"kind":"object","object":[{"name":"count","value":{"kind":"number","number":"2"}}]}}]`,
			examplesJSON(t, declared.Examples), "entry first %v: the declaring model keeps it", inlineFirst)
	}
}

// TestAllOfFold_AnExampleAtAReferencedPositionStaysOnItsNode pins that an
// inline schema a $ref elsewhere names keeps its example on its node, which
// every use shares, and the field composes that node with the reference, as
// before the carrier held its schema's notes.
func TestAllOfFold_AnExampleAtAReferencedPositionStaysOnItsNode(t *testing.T) {
	t.Parallel()
	const inline = "/components/schemas/Coll/allOf/1/properties/result_info"
	src := foldedExampleSpec("{$ref: '#/components/schemas/Info'}", "{type: object, example: {count: 1, page: 1}}", false, false) +
		"    Other: {type: object, properties: {x: {$ref: '#" + inline + "'}}}\n"
	field, diags, doc := collField(t, src)

	assert.Empty(t, field.Examples)
	assert.Empty(t, field.Unmodeled)
	assert.Zero(t, codeCounts(diags)[diag.DegradedConstruct])
	composed, ok := doc.Types[field.Type.Target].(*ir.Model)
	require.True(t, ok, "the field is a model")
	require.NotNil(t, composed.Base)
	assert.Equal(t, componentID("Info"), composed.Base.Target, "composing the reference")
	require.Len(t, composed.Mixins, 1)
	node := doc.Types[composed.Mixins[0].Target]
	require.NotNil(t, node)
	assert.True(t, strings.HasSuffix(string(node.Common().Provenance.Pointer), "/allOf/1/properties/result_info"))
	assert.Equal(t, `[{"value":{"kind":"object","object":[{"name":"count","value":{"kind":"number","number":"1"}},`+
		`{"name":"page","value":{"kind":"number","number":"1"}}]}}]`, examplesJSON(t, node.Common().Examples), "with the example on that node")
}

// variantField is what one variant of a distributed union holds for a field:
// its examples as JSON and the keys it keeps verbatim.
type variantField struct {
	examples string
	kept     []string
}

// distributedVariants compiles src and returns, keyed by the one boolean the
// enum each variant narrows field to holds, what the variants of the schema at
// pointer distributed across its oneOf hold for that field, so the result
// reads alike whichever order the variants come in.
func distributedVariants(t *testing.T, src, pointer, field string) (map[bool]variantField, []ir.Diagnostic) {
	t.Helper()
	doc, diags := parseVerified(t, src)
	require.NotNil(t, doc)
	variants := map[bool]variantField{}
	for id, typeDef := range doc.Types {
		model, ok := typeDef.(*ir.Model)
		if !ok || !strings.HasPrefix(string(id), "type/composed"+pointer+"/oneOf/") {
			continue
		}
		property, ok := propertyByWire(model, field)
		require.True(t, ok, "%s holds %s", id, field)
		enum, ok := doc.Types[property.Type.Target].(*ir.Enum)
		require.True(t, ok, "%s narrows %s to an enum", id, field)
		require.Len(t, enum.Members, 1, "%s: of one member", id)
		variant := variantField{examples: examplesJSON(t, property.Examples)}
		for key := range property.Unmodeled {
			variant.kept = append(variant.kept, key)
		}
		variants[enum.Members[0].Value.Bool] = variant
	}
	return variants, diags
}

// TestAllOfFold_AnExampleAVariantsEnumRefusesStaysRefused pins that a base's
// example a union variant's enum refuses stays off that variant's field, kept
// verbatim at the base's declaration with the fold's info, as the enum judges
// it, while the variant whose enum admits it takes it: in either order of the
// variants and of the schema's keywords, the four results compared, and with
// nothing for pass/example-outside-type to report
// (docs/compilers/openapi.md#allof-redeclared-property).
func TestAllOfFold_AnExampleAVariantsEnumRefusesStaysRefused(t *testing.T) {
	t.Parallel()
	const (
		base  = "      properties: {transferable: {type: boolean, example: false}}\n"
		yes   = "        - {properties: {transferable: {type: boolean, enum: [true]}}}\n"
		no    = "        - {properties: {transferable: {type: boolean, enum: [false]}}}\n"
		where = "/components/schemas/T/properties/transferable"
	)
	var first map[bool]variantField
	for _, layout := range [][2]bool{{false, false}, {true, false}, {false, true}, {true, true}} {
		variants := "      oneOf:\n" + yes + no
		if layout[0] {
			variants = "      oneOf:\n" + no + yes
		}
		schema := base + variants
		if layout[1] {
			schema = variants + base
		}
		label := map[bool]string{false: "true first", true: "false first"}[layout[0]] +
			map[bool]string{false: ", properties first", true: ", oneOf first"}[layout[1]]
		got, diags := distributedVariants(t, openapitest.ComponentSpec("    T:\n"+schema), "/components/schemas/T", "transferable")

		require.Len(t, got, 2, label)
		refusing := got[true]
		assert.Equal(t, "[]", refusing.examples, "%s: the variant whose enum refuses false takes no example", label)
		assert.Equal(t, []string{"openapi:conflicting-redeclaration" + where}, refusing.kept, "%s: the base's declaration is kept verbatim", label)
		admitting := got[false]
		assert.Equal(t, `[{"value":{"kind":"bool"}}]`, admitting.examples, "%s: the variant admitting false takes it", label)
		assert.Empty(t, admitting.kept, label)
		assert.Equal(t, 1, countCode(diags, diag.DegradedConstruct, where), "%s: the fold reports the refusal", label)
		assert.Zero(t, codeCounts(diags)[exampleOutsideType], "%s: no variant holds an example outside its type", label)
		if first == nil {
			first = got
			continue
		}
		assert.Equal(t, first, got, "%s: as in the first layout", label)
	}
}

// TestAllOfFold_AnExampleAFormatRefusesStaysRefused pins that a string example
// a uuid field refuses stays refused where no declaration gives another: off
// the field, kept verbatim at its declaration with the fold's info, as the
// format's grammar judges it, in either order of the entries.
func TestAllOfFold_AnExampleAFormatRefusesStaysRefused(t *testing.T) {
	t.Parallel()
	example := "        - {properties: {id: {type: string, example: hello}}}\n"
	uuid := "        - {properties: {id: {type: string, format: uuid}}}\n"
	for name, tc := range map[string]struct {
		entries, refused string
	}{
		"the example first": {example + uuid, "/components/schemas/T/allOf/0/properties/id"},
		"the uuid first":    {uuid + example, "/components/schemas/T/allOf/1/properties/id"},
	} {
		doc, diags := parseVerified(t, openapitest.ComponentSpec("    T:\n      allOf:\n"+tc.entries))
		require.NotNil(t, doc)
		model, ok := typeByName(doc, "T").(*ir.Model)
		require.True(t, ok, name)
		field, ok := propertyByWire(model, "id")
		require.True(t, ok, name)

		assert.Equal(t, ir.TypeID("type/primitive/uuid"), field.Type.Target, name)
		assert.Empty(t, field.Examples, "%s: the field takes no example its format refuses", name)
		require.Len(t, field.Unmodeled, 1, name)
		entry := unmodeledEntry(t, field.Unmodeled, "openapi:conflicting-redeclaration"+tc.refused)
		assert.Contains(t, string(entry.Value), "hello", "%s: kept at its declaration", name)
		assert.Equal(t, 1, countCode(diags, diag.DegradedConstruct, tc.refused), "%s: and the fold reports it", name)
		assert.Equal(t, map[string]int{diag.DegradedConstruct: 1}, codeCounts(diags), name)
	}
}
