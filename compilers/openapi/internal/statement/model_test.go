// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package statement_test

import (
	"encoding/json/jsontext"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/speakeasy-api/openapi/jsonschema/oas3"
	"github.com/speakeasy-api/openapi/marshaller"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/bind"
	"github.com/dexpace/morphic/compilers/openapi/internal/bind/typed"
	"github.com/dexpace/morphic/compilers/openapi/internal/oas"
	"github.com/dexpace/morphic/compilers/openapi/internal/statement"
	"github.com/dexpace/morphic/ir/irtest"
)

// text returns the scalar n spells, or "<nil>".
func text(n *yaml.Node) string {
	if n == nil {
		return "<nil>"
	}
	return n.Value
}

// texts returns the scalars ns spell.
func texts(ns []*yaml.Node) []string {
	out := make([]string, len(ns))
	for i, n := range ns {
		out[i] = text(n)
	}
	return out
}

// kinds returns the kinds the schema sub names writes.
func kinds(t *testing.T, table *statement.Table, sub statement.Sub) []statement.Kind {
	t.Helper()
	st := table.Sub(sub)
	require.Equal(t, statement.Stated, st.Truth, "%+v", sub)
	require.NotNil(t, st.Kinds, "%+v", sub)
	return st.Kinds.Types
}

// int64s returns the counts ps point at, -1 for a nil one.
func int64s(ps ...*int64) []int64 {
	out := make([]int64, len(ps))
	for i, p := range ps {
		out[i] = -1
		if p != nil {
			out[i] = *p
		}
	}
	return out
}

// bools returns the flags ps point at, "unset" for a nil one.
func bools(ps ...*bool) []string {
	out := make([]string, len(ps))
	for i, p := range ps {
		switch {
		case p == nil:
			out[i] = "unset"
		case *p:
			out[i] = "true"
		default:
			out[i] = "false"
		}
	}
	return out
}

// TestRead_FillsEachSlotFromItsKeywords reads one schema per slot writing
// every keyword of it, at 3.1, and at 3.0 and in draft 7 for the keywords
// only those define, and holds each typed field to what was written.
func TestRead_FillsEachSlotFromItsKeywords(t *testing.T) {
	t.Parallel()
	table, c := tableOver(t, document("3.1.0",
		`Kinds: {type: [string, "null"]}`,
		`Values: {enum: [a, b], const: c}`,
		`Format: {format: int32}`,
		`Number: {minimum: 1, maximum: 9, exclusiveMinimum: 0, exclusiveMaximum: 10, multipleOf: 2}`,
		`Text: {minLength: 1, maxLength: 5, pattern: '^a', contentEncoding: base64, contentMediaType: image/png, `+
			`contentSchema: {type: string}}`,
		`Array: {items: {type: string}, prefixItems: [{type: integer}], contains: {type: boolean}, minContains: 1, `+
			`maxContains: 2, minItems: 3, maxItems: 4, uniqueItems: true, unevaluatedItems: false}`,
		`Object: {properties: {a: {type: string}}, patternProperties: {'^x': {type: integer}}, additionalProperties: false, `+
			`propertyNames: {type: string}, required: [a], dependentRequired: {a: [b]}, minProperties: 1, maxProperties: 4, `+
			`discriminator: {propertyName: a, mapping: {x: '#/components/schemas/Kinds'}}, unevaluatedProperties: true}`,
		`Choice: {oneOf: [{type: string}], anyOf: [{type: integer}, {type: boolean}]}`,
		`Logic: {not: {type: integer}, if: {type: string}, then: {type: number}, else: {type: boolean}, `+
			`dependentSchemas: {a: {type: object}}}`,
		`Notes: {title: T, description: D, default: d, example: e, examples: [x, y], deprecated: true, readOnly: true, `+
			`writeOnly: false, xml: {name: n, namespace: 'https://example.com/ns', prefix: p, attribute: true, wrapped: false}, `+
			`externalDocs: {description: E, url: 'https://example.com'}, $comment: c}`,
		`Conjunction: {allOf: [{type: string}]}`,
		`Reference: {$ref: '#/components/schemas/Kinds'}`,
	), rulesAt(t, "3.1.0", ""))
	at := func(name string) *statement.Statement {
		st := table.At(component(c, name))
		require.Equal(t, statement.Stated, st.Truth, name)
		return st
	}

	assert.Equal(t, []statement.Kind{statement.KindString, statement.KindNull}, at("Kinds").Kinds.Types)

	values := at("Values").Values
	assert.Equal(t, []string{"a", "b"}, texts(values.Enum))
	assert.Equal(t, "c", text(values.Const))

	assert.Equal(t, "int32", at("Format").Format.Name)

	number := at("Number").Number
	assert.Equal(t, []string{"1", "9", "0", "10", "2"}, texts([]*yaml.Node{number.Minimum, number.Maximum,
		number.ExclusiveMinimum, number.ExclusiveMaximum, number.MultipleOf}))

	textual := at("Text").Text
	assert.Equal(t, []int64{1, 5}, int64s(textual.MinLength, textual.MaxLength))
	assert.Equal(t, []string{"^a", "base64", "image/png"}, []string{textual.Pattern, textual.ContentEncoding,
		textual.ContentMediaType})
	assert.Equal(t, []statement.Kind{statement.KindString}, kinds(t, table, textual.ContentSchema))

	array := at("Array").Array
	assert.Equal(t, []statement.Kind{statement.KindString}, kinds(t, table, array.Items))
	require.Len(t, array.PrefixItems, 1)
	assert.Equal(t, []statement.Kind{statement.KindInteger}, kinds(t, table, array.PrefixItems[0]))
	assert.Equal(t, []statement.Kind{statement.KindBoolean}, kinds(t, table, array.Contains))
	assert.Equal(t, []int64{1, 2, 3, 4}, int64s(array.MinContains, array.MaxContains, array.MinItems, array.MaxItems))
	assert.Equal(t, []string{"true"}, bools(array.UniqueItems))
	assert.Equal(t, statement.Nothing, table.Sub(array.UnevaluatedItems).Truth)
	assert.Nil(t, array.AdditionalItems, "3.1 defines no additionalItems")

	object := at("Object").Object
	require.Len(t, object.Properties, 1)
	assert.Equal(t, "a", object.Properties[0].Name)
	assert.Equal(t, []statement.Kind{statement.KindString}, kinds(t, table, object.Properties[0]))
	require.Len(t, object.PatternProperties, 1)
	assert.Equal(t, "^x", object.PatternProperties[0].Name)
	assert.Equal(t, []statement.Kind{statement.KindInteger}, kinds(t, table, object.PatternProperties[0]))
	assert.Equal(t, statement.Nothing, table.Sub(object.AdditionalProperties).Truth)
	assert.Equal(t, []statement.Kind{statement.KindString}, kinds(t, table, object.PropertyNames))
	assert.Equal(t, statement.Anything, table.Sub(object.UnevaluatedProperties).Truth)
	assert.Equal(t, []string{"a"}, object.Required)
	require.NotNil(t, object.DependentRequired)
	assert.Equal(t, yaml.MappingNode, object.DependentRequired.Kind)
	assert.Equal(t, []int64{1, 4}, int64s(object.MinProperties, object.MaxProperties))
	require.NotNil(t, object.Discriminator)
	assert.Equal(t, "a", object.Discriminator.PropertyName)
	assert.Equal(t, []statement.MappingEntry{{Value: "x", Target: "#/components/schemas/Kinds"}},
		object.Discriminator.Mapping)
	want := component(c, "Object")
	want.Pointer += "/discriminator"
	assert.Equal(t, want, object.Discriminator.At)
	assert.Equal(t, yaml.MappingNode, object.Discriminator.Node.Kind)

	choice := at("Choice").Choice
	require.Len(t, choice.OneOf, 1)
	require.Len(t, choice.AnyOf, 2)
	assert.Equal(t, []int{0, 1}, []int{choice.AnyOf[0].Index, choice.AnyOf[1].Index})
	assert.Equal(t, []statement.Kind{statement.KindBoolean}, kinds(t, table, choice.AnyOf[1]))

	logic := at("Logic").Logic
	assert.Equal(t, []statement.Kind{statement.KindInteger}, kinds(t, table, logic.Not))
	assert.Equal(t, []statement.Kind{statement.KindString}, kinds(t, table, logic.If))
	assert.Equal(t, []statement.Kind{statement.KindNumber}, kinds(t, table, logic.Then))
	assert.Equal(t, []statement.Kind{statement.KindBoolean}, kinds(t, table, logic.Else))
	require.Len(t, logic.DependentSchemas, 1)
	assert.Equal(t, "a", logic.DependentSchemas[0].Name)
	assert.Equal(t, []statement.Kind{statement.KindObject}, kinds(t, table, logic.DependentSchemas[0]))

	notes := at("Notes").Notes
	assert.Equal(t, []string{"T", "D"}, []string{notes.Title, notes.Description})
	assert.Equal(t, []string{"d", "e", "c"}, texts([]*yaml.Node{notes.Default, notes.Example, notes.Comment}))
	assert.Equal(t, []string{"x", "y"}, texts(notes.Examples))
	assert.Equal(t, []string{"true", "true", "false"}, bools(notes.Deprecated, notes.ReadOnly, notes.WriteOnly))
	require.NotNil(t, notes.XML)
	assert.Equal(t, []string{"n", "https://example.com/ns", "p"}, []string{*notes.XML.Name, *notes.XML.Namespace,
		*notes.XML.Prefix})
	assert.Equal(t, []string{"true", "false"}, bools(notes.XML.Attribute, notes.XML.Wrapped))
	require.NotNil(t, notes.ExternalDocs)
	assert.Equal(t, []string{"E", "https://example.com"}, []string{notes.ExternalDocs.Description, notes.ExternalDocs.URL})
	assert.Equal(t, yaml.MappingNode, notes.ExternalDocs.Node.Kind)
	assert.Equal(t, yaml.MappingNode, notes.XML.Node.Kind)

	conjunction := at("Conjunction")
	require.Len(t, conjunction.AllOf, 1)
	assert.Equal(t, []statement.Kind{statement.KindString}, kinds(t, table, conjunction.AllOf[0]))

	ref := at("Reference").Ref
	require.NotNil(t, ref)
	assert.Equal(t, "$ref", ref.Keyword)
	assert.True(t, ref.Bound)
	assert.Equal(t, component(c, "Kinds"), ref.Entry.Hop.Target)

	at30, c30 := tableOver(t, document("3.0.3", `Nullable: {type: string, nullable: true}`), rulesAt(t, "3.0.3", ""))
	assert.Equal(t, []string{"true"}, bools(at30.At(component(c30, "Nullable")).Kinds.Nullable))

	const draft07 = "http://json-schema.org/draft-07/schema#"
	d7, c7 := tableOver(t, "jsonSchemaDialect: '"+draft07+"'\n"+document("3.1.0",
		`Drafted: {additionalItems: false, dependencies: {a: [b]}}`), rulesAt(t, "3.1.0", draft07))
	drafted := d7.At(component(c7, "Drafted"))
	assert.Equal(t, "false", text(drafted.Array.AdditionalItems))
	require.NotNil(t, drafted.Logic.Dependencies)
	assert.Equal(t, yaml.MappingNode, drafted.Logic.Dependencies.Kind)
}

// TestRead_AKeyTheDialectDoesNotDefineIsUnknown pins that a key's slot is the
// rows', not its name's: a keyword they do not define is unknown, as an
// extension is, and one they define later is read in its slot. The value of
// each is read into its keyword's slot as the parser read it all the same,
// since a policy reads a 3.0 `nullable` in a 3.1 schema
// (docs/compilers/openapi.md#nullable), and an extension fills none.
func TestRead_AKeyTheDialectDoesNotDefineIsUnknown(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		version, key, value string
		slot                oas.Slot
		later               bool
		read                func(*statement.Statement) bool
	}{
		{"3.1.0", "nullable", "true", oas.SlotUnknown, false, func(st *statement.Statement) bool {
			return st.Kinds != nil && st.Kinds.Nullable != nil && *st.Kinds.Nullable
		}},
		{"3.1.0", "additionalItems", "false", oas.SlotUnknown, false, func(st *statement.Statement) bool {
			return st.Array != nil && text(st.Array.AdditionalItems) == "false"
		}},
		{"3.1.0", "x-a", "1", oas.SlotUnknown, false, func(st *statement.Statement) bool {
			return st.Kinds == nil && st.Array == nil && st.Notes == nil && st.Object == nil
		}},
		{"3.0.3", "prefixItems", "[{}]", oas.SlotArray, true, func(st *statement.Statement) bool {
			return st.Array != nil && len(st.Array.PrefixItems) == 1
		}},
	} {
		table, c := tableOver(t, document(tc.version, "A: {"+tc.key+": "+tc.value+"}"), rulesAt(t, tc.version, ""))
		st := table.At(component(c, "A"))
		k, read := st.Key(tc.key)
		require.True(t, read, tc.key)
		assert.Equal(t, tc.slot, k.Slot, tc.key)
		assert.Equal(t, tc.later, k.Later, tc.key)
		assert.True(t, tc.read(st), "%s is read as the parser reads it", tc.key)
	}
}

// TestRead_ANullKeywordIsWrittenAndLeftOut reads the schemas of the
// null-keywords case: each null keyword is written and left out, its slot
// holding nothing for it, bar `const`, whose null is the instance it holds
// (docs/compilers/openapi.md#null-keywords).
func TestRead_ANullKeywordIsWrittenAndLeftOut(t *testing.T) {
	t.Parallel()
	src, err := os.ReadFile(filepath.Join(irtest.RepoRoot(t), "testdata", "conformance", "openapi", "null-keywords.yaml"))
	require.NoError(t, err)
	table, c := tableOver(t, string(src), rulesAt(t, "3.1.0", ""))
	at := func(pointer string) *statement.Statement {
		st := table.At(c.Position(jsontext.Pointer(pointer)))
		require.Equal(t, statement.Stated, st.Truth, pointer)
		return st
	}
	leftOut := func(st *statement.Statement, key string) {
		t.Helper()
		k, read := st.Key(key)
		assert.False(t, read, key)
		assert.True(t, k.Null, key)
	}

	described := at("/components/schemas/Described")
	leftOut(described, "description")
	assert.Empty(t, described.Notes.Description)

	bounded := at("/components/schemas/Bounded")
	leftOut(bounded, "maximum")
	assert.Nil(t, bounded.Number.Maximum)

	enumerated := at("/components/schemas/Enumerated")
	leftOut(enumerated, "enum")
	require.NotNil(t, enumerated.Values, "a null enum is written")
	assert.Nil(t, enumerated.Values.Enum, "and read as left out")

	kinded := at("/components/schemas/Typed")
	leftOut(kinded, "type")
	assert.Empty(t, kinded.Kinds.Types)

	identified := at("/components/schemas/Identified")
	leftOut(identified, "$id")
	leftOut(identified, "discriminator")
	leftOut(identified, "readOnly")
	assert.Nil(t, identified.Object.Discriminator)
	assert.Nil(t, identified.Notes.ReadOnly)

	constant := at("/components/schemas/Values/properties/c")
	k, read := constant.Key("const")
	assert.True(t, read, "a null const is the instance it holds")
	assert.True(t, k.Null)
	require.NotNil(t, constant.Values)
}

// TestRead_ReadsWhatAValueStates pins each truth a value reads as, a value
// the bindings do not know, which is read with no position, and a keyword
// whose value the parser's model does not hold.
func TestRead_ReadsWhatAValueStates(t *testing.T) {
	t.Parallel()
	const draft07 = "http://json-schema.org/draft-07/schema#"
	chain := strings.Repeat("{type: array, items: ", typed.MaxMends+2) + "[{}]" + strings.Repeat("}", typed.MaxMends+2)
	table, c := tableOver(t, "jsonSchemaDialect: '"+draft07+"'\n"+document("3.1.0",
		`Truths: {allOf: [true, false], properties: {none: null}}`,
		`Empty: {properties: {}, $comment: c, x-a: 1}`,
		`Bare: {}`,
		`Disc: {discriminator: {propertyName: a}}`,
		"Chain: "+chain,
	), rulesAt(t, "3.1.0", draft07))

	truths := table.At(component(c, "Truths"))
	require.Len(t, truths.AllOf, 2)
	assert.Equal(t, statement.Anything, table.Sub(truths.AllOf[0]).Truth)
	assert.Equal(t, statement.Nothing, table.Sub(truths.AllOf[1]).Truth)
	none := table.Sub(truths.Object.Properties[0])
	assert.Equal(t, statement.Unwritten, none.Truth, "a null schema is written as none")
	assert.Empty(t, none.Keys)
	assert.Equal(t, yaml.ScalarNode, none.Node.Kind)

	past := c.Position(jsontext.Pointer("/components/schemas/Chain" + strings.Repeat("/items", typed.MaxMends+1)))
	unread := table.At(past)
	assert.Equal(t, statement.Unread, unread.Truth)
	assert.Empty(t, unread.Keys)
	assert.Equal(t, yaml.MappingNode, unread.Node.Kind)

	absent := table.At(component(c, "Nothing"))
	assert.Equal(t, statement.Unwritten, absent.Truth)
	assert.Equal(t, bind.Position{}, absent.At)
	assert.Same(t, absent, table.Read(nil))

	empty := table.At(component(c, "Empty"))
	assert.Nil(t, empty.Object.Properties)
	bare := table.At(component(c, "Bare"))
	assert.Equal(t, statement.Stated, bare.Truth)
	assert.Empty(t, bare.Keys)

	var misshapen yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte(`{xml: a, externalDocs: a, discriminator: a}`), &misshapen))
	decoded := &oas3.JSONSchema[oas3.Referenceable]{}
	_, err := marshaller.UnmarshalNode(t.Context(), "", misshapen.Content[0], decoded)
	require.NoError(t, err)
	unchecked := statement.NewTable(statement.Source{Dialects: rows{rulesAt(t, "3.1.0", "")}}).Of(decoded)
	require.Len(t, unchecked.Keys, 3, "a schema no load checked, decoded alone")
	assert.Nil(t, unchecked.Notes.XML, "a value the parser's model does not hold opens its slot and fills nothing")
	assert.Nil(t, unchecked.Notes.ExternalDocs)
	assert.Nil(t, unchecked.Object.Discriminator)
	comment, _ := empty.Key("$comment")
	extension, _ := empty.Key("x-a")
	assert.False(t, comment.Modeled, "the parser's model holds no $comment")
	assert.True(t, extension.Modeled, "the parser's model holds extensions")
	_, read := empty.Key("absent")
	assert.False(t, read)
	_, read = (*statement.Schema)(nil).Key("type")
	assert.False(t, read, "a nil schema writes nothing")

	js := c.Bindings().At(component(c, "Disc"))
	unknown := statement.NewTable(statement.Source{Dialects: rows{rulesAt(t, "3.1.0", "")}}).Of(js)
	assert.Equal(t, bind.Position{}, unknown.At, "a value the bindings do not know has no position")
	require.NotNil(t, unknown.Object.Discriminator)
	assert.Equal(t, "a", unknown.Object.Discriminator.PropertyName)
	assert.Nil(t, unknown.Object.Discriminator.Mapping)
	assert.Equal(t, bind.Position{}, unknown.Object.Discriminator.At, "nor has its discriminator")

	unruled := statement.NewTable(statement.Source{Bindings: c.Bindings()}).Of(js)
	k, _ := unruled.Key("discriminator")
	assert.Equal(t, oas.SlotUnknown, k.Slot, "no rows place a key")
	require.NotNil(t, unruled.Object, "its value is read all the same")
	assert.Equal(t, "a", unruled.Object.Discriminator.PropertyName)
}
