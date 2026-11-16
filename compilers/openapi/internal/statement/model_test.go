// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package statement_test

import (
	"encoding/json/jsontext"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/speakeasy-api/openapi/jsonschema/oas3"
	"github.com/speakeasy-api/openapi/marshaller"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/annotation"
	"github.com/dexpace/morphic/compilers/openapi/internal/bind"
	"github.com/dexpace/morphic/compilers/openapi/internal/bind/typed"
	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
	"github.com/dexpace/morphic/compilers/openapi/internal/oas"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
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
		`Flags: {deprecated: true, readOnly: false}`,
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
	flags := at("Flags").Notes
	assert.Equal(t, []string{"true", "false"}, bools(flags.Deprecated, flags.ReadOnly), "each flag from its own key")
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
// each is read into its keyword's slot all the same, since a policy reads a
// 3.0 `nullable` in a 3.1 schema (docs/compilers/openapi.md#nullable), and an
// extension fills none.
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
			return st.Kinds == nil && st.Values == nil && st.Format == nil && st.Number == nil && st.Text == nil &&
				st.Array == nil && st.Object == nil && st.Choice == nil && st.Logic == nil && st.Notes == nil
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
		assert.True(t, tc.read(st), "%s: what the slots hold", tc.key)
	}
}

// TestRead_ANullKeywordIsWrittenAndLeftOut reads the schemas of the
// null-keywords case: each null keyword is written and its key left out, bar
// `const`, whose null is the instance it holds
// (docs/compilers/openapi.md#null-keywords), and its slot holds what the
// parser's model reads of it, which is nothing but for `type`.
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
	assert.Equal(t, []statement.Kind{""}, kinded.Kinds.Types,
		"the model's reading of a null type (docs/parser-library.md#null-type-is-empty)")

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

// TestRead_AKeywordMergedPastTheBoundHasItsValueAndNoKey reads a schema whose
// `<<` chain runs past nodeview.MergeDepthLimit: its keys list only what it
// writes itself, as the raw mapping reads it, while each typed value merged
// past the bound, one keyword of each slot, is the parser's model's, which
// expands the whole chain, as is its list of keys it holds no field for. A
// bound, kept as the node it was written as, comes from its key, so it is not
// there.
func TestRead_AKeywordMergedPastTheBoundHasItsValueAndNoKey(t *testing.T) {
	t.Parallel()
	var spec strings.Builder
	spec.WriteString("openapi: 3.1.0\ninfo: {title: t, version: '1'}\npaths: {}\n" +
		"x-c0: &c0 {allOf: [{}], type: object, enum: [{}], format: f, minLength: 1, minItems: 2, maximum: 9, " +
		"discriminator: {propertyName: k, x: 1}, oneOf: [{}], not: {}, description: d, foo: 1}\n")
	for i := 1; i <= nodeview.MergeDepthLimit; i++ {
		fmt.Fprintf(&spec, "x-c%d: &c%d {<<: *c%d}\n", i, i, i-1)
	}
	fmt.Fprintf(&spec, "components:\n  schemas:\n    P: {<<: *c%d, title: P}\n", nodeview.MergeDepthLimit)
	table, c := tableOver(t, spec.String(), rulesAt(t, "3.1.0", ""))
	p := table.At(component(c, "P"))
	require.Equal(t, statement.Stated, p.Truth)

	names := make([]string, len(p.Keys))
	for i, k := range p.Keys {
		names[i] = k.Name
	}
	assert.Equal(t, []string{"title"}, names, "the raw mapping lists nothing past the bound")
	_, read := p.Key("discriminator")
	assert.False(t, read)

	require.NotNil(t, p.Object)
	require.NotNil(t, p.Object.Discriminator, "the model's discriminator")
	assert.Equal(t, "k", p.Object.Discriminator.PropertyName)
	assert.Equal(t, []string{"x"}, p.Object.Discriminator.Unknown)
	assert.Equal(t, []string{"foo"}, p.Unknown, "the model's list names a key it holds no field for")
	assert.Len(t, p.AllOf, 1)
	assert.Len(t, p.Entries, 1)
	assert.Equal(t, []statement.Kind{statement.KindObject}, p.Kinds.Types)
	assert.Len(t, p.Values.Enum, 1)
	assert.Equal(t, "f", p.Format.Name)
	assert.Equal(t, []int64{1, 2}, int64s(p.Text.MinLength, p.Array.MinItems))
	assert.Len(t, p.Choice.OneOf, 1)
	assert.Equal(t, statement.Stated, table.Sub(p.Logic.Not).Truth)
	assert.Equal(t, []string{"P", "d"}, []string{p.Notes.Title, p.Notes.Description})
	assert.Nil(t, p.Number, "a bound comes from its key")
	assert.NotNil(t, c.Bindings().At(component(c, "P")).GetSchema().Maximum, "though the model holds it")
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
		`Nulls: {nullable: null, format: null, maxLength: null, anyOf: null}`,
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
	nulls := table.At(component(c, "Nulls"))
	require.NotNil(t, nulls.Kinds, "a key the model holds nothing for opens its slot")
	assert.Nil(t, nulls.Kinds.Nullable)
	require.NotNil(t, nulls.Format)
	assert.Empty(t, nulls.Format.Name)
	require.NotNil(t, nulls.Text)
	assert.Nil(t, nulls.Text.MaxLength)
	require.NotNil(t, nulls.Choice)
	assert.Nil(t, nulls.Choice.AnyOf)
	bare := table.At(component(c, "Bare"))
	assert.Equal(t, statement.Stated, bare.Truth)
	assert.Empty(t, bare.Keys)

	var misshapen yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte(`{xml: a, externalDocs: a, discriminator: a}`), &misshapen))
	decoded := &oas3.JSONSchema[oas3.Referenceable]{}
	_, err := marshaller.UnmarshalNode(t.Context(), "", misshapen.Content[0], decoded)
	require.NoError(t, err)
	unchecked := statement.NewTable(statement.Source{Rules: everywhere(rulesAt(t, "3.1.0", ""))}).Of(decoded)
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
	unknown := statement.NewTable(statement.Source{Rules: everywhere(rulesAt(t, "3.1.0", ""))}).Of(js)
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

// extensionNames returns the names list holds, in its order.
func extensionNames(list []statement.Extension) []string {
	out := make([]string, len(list))
	for i, e := range list {
		out[i] = e.Name
	}
	return out
}

// TestRead_ListsExtensionsAsTheModelHoldsThem holds a schema's extensions, and
// those of its xml, discriminator and externalDocs objects, to the parser's
// model: in the order they are written, and one merged in through `<<` from
// past nodeview.MergeDepthLimit, whose pair the schema's keys, and the xml
// object's node as the compiler reads it, do not reach.
func TestRead_ListsExtensionsAsTheModelHoldsThem(t *testing.T) {
	t.Parallel()
	var spec strings.Builder
	spec.WriteString("openapi: 3.1.0\ninfo: {title: t, version: '1'}\npaths: {}\n" +
		"x-c0: &c0 {x-a: 1}\nx-d0: &d0 {x-x: 2}\n")
	for i := 1; i <= nodeview.MergeDepthLimit; i++ {
		fmt.Fprintf(&spec, "x-c%d: &c%d {<<: *c%d}\nx-d%d: &d%d {<<: *d%d}\n", i, i, i-1, i, i, i-1)
	}
	fmt.Fprintf(&spec, "components:\n  schemas:\n    P: {<<: *c%d, title: P, xml: {<<: *d%d, name: n}}\n",
		nodeview.MergeDepthLimit, nodeview.MergeDepthLimit)
	spec.WriteString("    O: {x-b: 1, x-a: 2, type: object, xml: {x-y: 1, x-w: 2}, discriminator: {propertyName: k, x-d: 1}, " +
		"externalDocs: {url: 'https://d.example', x-e: 1}}\n")
	table, c := tableOver(t, spec.String(), rulesAt(t, "3.1.0", ""))

	p := table.At(component(c, "P"))
	require.Equal(t, statement.Stated, p.Truth)
	assert.Equal(t, []string{"x-a"}, extensionNames(p.Extensions), "the model's, past the bound")
	_, read := p.Key("x-a")
	assert.False(t, read, "the keys do not reach it")
	require.NotNil(t, p.Notes.XML)
	assert.Equal(t, []string{"x-x"}, extensionNames(p.Notes.XML.Extensions), "an xml object's, past the bound")
	assert.Nil(t, nodeview.Child(p.Notes.XML.Node, "x-x"), "which its node does not reach")

	o := table.At(component(c, "O"))
	assert.Equal(t, []string{"x-b", "x-a"}, extensionNames(o.Extensions), "in source order")
	assert.Equal(t, "2", text(o.Extensions[1].Node))
	assert.Equal(t, []string{"x-y", "x-w"}, extensionNames(o.Notes.XML.Extensions))
	assert.Equal(t, []string{"x-d"}, extensionNames(o.Object.Discriminator.Extensions))
	assert.Equal(t, []string{"x-e"}, extensionNames(o.Notes.ExternalDocs.Extensions))
	assert.Nil(t, table.At(component(c, "Absent")).Extensions)
}

// TestSub_TruthTellsNoSchemaFromFalse pins what a value written where a schema
// belongs states, read from its Sub and from its statement alike. The parser
// decodes a null, a string, a number or a list as the `false` schema
// (docs/parser-library.md#null-schema-is-false), which admits no value where
// these state nothing, so only the node tells them apart, through an alias
// too; and it decodes a mapping it fails beneath as a boolean
// (docs/parser-library.md#failure-beneath-itself). Only a written `false`, and
// such a mapping, read as the false schema.
func TestSub_TruthTellsNoSchemaFromFalse(t *testing.T) {
	t.Parallel()
	rows := []struct {
		name, value string
		want        statement.Truth
	}{
		{"the word null", "null", statement.Unwritten},
		{"a tilde", "~", statement.Unwritten},
		{"an empty value", "", statement.Unwritten},
		{"a null that is anchored", "&n null", statement.Unwritten},
		{"an alias to an anchored null", "*n", statement.Unwritten},
		{"false", "false", statement.Nothing},
		{"a false that is anchored", "&f false", statement.Nothing},
		{"an alias to an anchored false", "*f", statement.Nothing},
		{"true", "true", statement.Anything},
		{"an object", "{type: string}", statement.Stated},
		{"an empty object", "{}", statement.Stated},
		{"a string", "hello", statement.Unwritten},
		{"a quoted false", `"false"`, statement.Unwritten},
		{"a number", "7", statement.Unwritten},
		{"a list", "[a]", statement.Unwritten},
		{"a string tagged as one", "!!str true", statement.Unwritten},
		{"a boolean tagged outside the ruleset", "!local true", statement.Unwritten},
		{"a list that is anchored", "&l [a]", statement.Unwritten},
		{"an alias to an anchored list", "*l", statement.Unwritten},
		{"a tagged boolean", "!!bool false", statement.Nothing},
	}
	var props strings.Builder
	for i, row := range rows {
		fmt.Fprintf(&props, "        p%d: %s\n", i, row.value)
	}
	depth := typed.MaxMends + 2
	chain := strings.Repeat("{type: array, items: ", depth) + "[{}]" + strings.Repeat("}", depth)
	table, c := tableOver(t, openapitest.ComponentSpecVer("3.1.0",
		"    A:\n      properties:\n"+props.String()+"    Chain: "+chain+"\n"), rulesAt(t, "3.1.0", ""))

	a := table.At(component(c, "A"))
	require.Len(t, a.Object.Properties, len(rows))
	for i, row := range rows {
		sub := a.Object.Properties[i]
		require.Equal(t, fmt.Sprintf("p%d", i), sub.Name)
		assert.Equal(t, row.want, sub.Truth(), "%s: the Sub", row.name)
		assert.Equal(t, row.want, table.Sub(sub).Truth, "%s: its statement", row.name)
		assert.Equal(t, row.want == statement.Nothing, sub.Truth().ReadsFalse(), "%s: read as false", row.name)
	}

	above := table.At(c.Position(jsontext.Pointer("/components/schemas/Chain" + strings.Repeat("/items", depth-2))))
	require.Equal(t, statement.Stated, above.Truth)
	items := above.Array.Items
	assert.Equal(t, statement.Unread, items.Truth(), "a mapping read as a boolean")
	assert.Equal(t, statement.Unread, table.Sub(items).Truth)
	assert.True(t, items.Truth().ReadsFalse(), "the parser reads it as false")

	assert.Equal(t, statement.Unwritten, statement.Sub{}.Truth(), "no value at all")
	built := statement.NewTable(statement.Source{}).Of(oas3.NewJSONSchemaFromBool(false))
	assert.Equal(t, statement.Nothing, built.Truth, "a false schema built in memory, which has no node")
}

// TestRead_AReferenceHoldsTheModelsText pins Reference.Text to the $ref the
// parser's model holds, which the bindings' entry does not say: empty for an
// empty $ref and for a $dynamicRef the bindings read as a $ref, so that
// annotation.At reads either as a declaration, and the $ref itself for one
// naming a schema.
func TestRead_AReferenceHoldsTheModelsText(t *testing.T) {
	t.Parallel()
	table, c := tableOver(t, document("3.1.0",
		`E: {$ref: ''}`, `D: {$dynamicRef: '#a'}`, `R: {$ref: '#/components/schemas/E'}`,
	), rulesAt(t, "3.1.0", ""))
	for _, tc := range []struct {
		name, keyword, text string
		kind                annotation.Kind
	}{
		{"E", "$ref", "", annotation.Declaration},
		{"D", "$dynamicRef", "", annotation.Declaration},
		{"R", "$ref", "#/components/schemas/E", annotation.Reference},
	} {
		st := table.At(component(c, tc.name))
		require.NotNil(t, st.Ref, tc.name)
		assert.Equal(t, tc.keyword, st.Ref.Keyword, tc.name)
		assert.True(t, st.Ref.Bound, "%s: the bindings read it", tc.name)
		assert.Equal(t, tc.text, st.Ref.Text, tc.name)
		assert.Equal(t, tc.kind, annotation.At(st).Kind, tc.name)
	}
}

// TestRead_AnExclusiveBoundHoldsTheArmTheModelRead pins Number's forms to the
// arm the parser's model read each exclusive bound as: a flag, true where the
// model reads a string such as `yes` as true, or a bound.
func TestRead_AnExclusiveBoundHoldsTheArmTheModelRead(t *testing.T) {
	t.Parallel()
	flags, fc := tableOver(t, document("3.0.3",
		`F: {type: number, minimum: 5, exclusiveMinimum: yes, maximum: 9, exclusiveMaximum: false}`,
		`M: {type: number, minimum: 5}`,
	), rulesAt(t, "3.0.3", ""))
	f := flags.At(component(fc, "F"))
	require.NotNil(t, f.Number)
	assert.Equal(t, statement.ExclusiveTrue, f.Number.ExclusiveMinimumForm)
	assert.Equal(t, statement.ExclusiveFalse, f.Number.ExclusiveMaximumForm)
	assert.Equal(t, "5", text(f.Number.Minimum))
	m := flags.At(component(fc, "M"))
	require.NotNil(t, m.Number, "a key opens the slot")
	assert.Equal(t, statement.NoExclusive, m.Number.ExclusiveMinimumForm)

	bounds, bc := tableOver(t, document("3.1.0", `B: {exclusiveMinimum: 5}`), rulesAt(t, "3.1.0", ""))
	b := bounds.At(component(bc, "B"))
	require.NotNil(t, b.Number)
	assert.Equal(t, statement.ExclusiveBound, b.Number.ExclusiveMinimumForm)
	assert.Equal(t, statement.NoExclusive, b.Number.ExclusiveMaximumForm)
	assert.Equal(t, "5", text(b.Number.ExclusiveMinimum))
}
