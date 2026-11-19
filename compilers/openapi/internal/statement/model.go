// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package statement

import (
	"iter"
	"slices"

	"github.com/speakeasy-api/openapi/jsonschema/oas3"
	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
	"github.com/dexpace/morphic/compilers/openapi/internal/reftable"
	"github.com/dexpace/morphic/compilers/openapi/internal/specrules"
)

// model is the parser's schema value, which this file alone reads.
type model = oas3.JSONSchema[oas3.Referenceable]

// Of returns the statement of js, read on the first ask and remembered: its
// schema object, the reference that object writes beside other keys, and its
// inline allOf entries, flattened. A nil js reads as Unwritten.
func (t *MemoizingSchemaReader) Of(js *model) *Statement {
	t.lookups++
	if st, ok := t.statements[js]; ok {
		return st
	}
	st := &Statement{Schema: t.own(js)}
	if st.BesideRef != nil {
		st.Entries = []AllOfConjunct{{Schema: st.BesideRef}}
	}
	st.of = st
	t.flatten(st)
	t.statements[js] = st
	return st
}

// Parsed returns the parser's schema object schema was read from, nil for a nil schema
// and for a value that is no schema object: what the operation walk reads
// beside the statement (GitHub #1991). For a Truncated schema it is the copy fill
// read, holding only what schema's keys list (listedOnly), so the walk reads what
// the statement holds. It goes once that walk reads none.
func Parsed(schema *Schema) *oas3.Schema {
	if schema == nil {
		return nil
	}
	if schema.Truncated {
		return listedOnly(schema.m.GetSchema(), schema)
	}
	return schema.m.GetSchema()
}

// ParsedAt returns the parser's value st was read from, nil for a nil st: what
// the operation walk reads beside the statement (GitHub #1991). It goes once
// that walk reads none.
func ParsedAt(st *Statement) *model {
	if st == nil {
		return nil
	}
	return st.m
}

// read returns the schema object js holds, read on the first ask and
// remembered, which reads no other object: where the bindings read it, its
// node, its truth and, for a stated one, its keys in their slots, its
// reference and each slot's values.
func (t *MemoizingSchemaReader) read(js *model) *Schema {
	if schema, ok := t.schemas[js]; ok {
		return schema
	}
	t.reads++
	schemaValue := &Schema{m: js, Truth: truthOf(js)}
	t.schemas[js] = schemaValue
	if js == nil {
		return schemaValue
	}
	schemaValue.At, _ = t.src.Bindings.Site(js)
	if schemaValue.Truth != Stated {
		schemaValue.Node = nodeview.Deref(js.GetCore().GetRootNode())
		return schemaValue
	}
	m := js.GetSchema()
	schemaValue.Node = nodeview.Deref(nodeview.DocumentRoot(m.GetRootNode()))
	schemaValue.Unknown = m.GetCore().GetUnknownProperties()
	schemaValue.Extensions = listedExtensions(m.GetExtensions().Len(), m.GetExtensions().All())
	readKeys(schemaValue, t.src.Rules.At(schemaValue.At))
	if schemaValue.Truncated {
		dropOutranked(schemaValue, m)
	}
	if kw := t.src.Bindings.RefKeyword(js); kw != "" {
		referenceSite, bound := t.src.Bindings.Entry(js)
		schemaValue.Ref = &Reference{Keyword: kw, Text: string(m.GetRef()), Entry: referenceSite, Bound: bound}
	}
	fill(schemaValue, m)
	return schemaValue
}

// SchemaValueForm returns what the schema s names states, as MemoizingSchemaReader.Subschema reads it, with no
// lookup.
func (s Subschema) SchemaValueForm() SchemaValueForm {
	return truthOf(s.m)
}

// Held reports whether the parser's model holds a value here, a value written
// as no schema among them, which Truth reads Unwritten as it reads none
// (docs/parser-library.md#null-schema-is-false). It is false for a nil s.
func (s *Schema) Held() bool {
	return s != nil && s.m != nil
}

// Property returns the schema s writes for the property name, as the parser's
// model holds it, by one lookup in the model's map: the Sub
// Object.Properties lists under that name. It is false for a name s does not
// write, for a nil or unstated s, and for a `properties` its keys do not list
// (listedOnly).
func (s *Schema) Property(name string) (Subschema, bool) {
	if s == nil || s.Object == nil || len(s.Object.Properties) == 0 {
		return Subschema{}, false
	}
	p, ok := s.m.GetSchema().GetProperties().Get(name)
	if !ok {
		return Subschema{}, false
	}
	return Subschema{Name: name, m: p}, true
}

// truthOf returns what js states. The parser reads a value written as no
// schema as the `false` schema, which truthOf reads as Unwritten by its node
// (docs/parser-library.md#null-schema-is-false), and a mapping it fails
// beneath as a boolean, which truthOf reads as Unread
// (docs/parser-library.md#failure-beneath-itself).
func truthOf(js *model) SchemaValueForm {
	if js == nil {
		return Unwritten
	}
	node := nodeview.Deref(js.GetCore().GetRootNode())
	if node != nil && node.Kind != yaml.MappingNode && (node.Kind != yaml.ScalarNode || node.ShortTag() != "!!bool") {
		return Unwritten
	}
	switch {
	case !js.IsBool():
		return Stated
	case node != nil && node.Kind == yaml.MappingNode:
		return Unread
	case js.GetBool() != nil && !*js.GetBool():
		return Nothing
	default:
		return Anything
	}
}

// fill reads each slot's values. A typed value is m's, the parser's model,
// as the walk reads the model; for a Truncated schema, only where schema's keys list
// its keyword (listedOnly). A value kept as the node it was written as is its
// key's. Each keyword lands in its own slot (specrules.SlotOf) whatever the rows
// say: a policy reads a 3.0 `nullable` in a 3.1 schema
// (docs/compilers/openapi.md#nullable), and the key's Slot carries the rows'
// verdict. A listed key whose value is not read opens its slot and fills
// nothing.
func fill(schema *Schema, m *oas3.Schema) {
	if schema.Truncated {
		m = listedOnly(m, schema)
	}
	schema.AllOf = listed(m.AllOf)
	schema.Kinds = kindsOf(m)
	schema.Number = numberOf(m)
	schema.Values = valuesOf(m)
	schema.Format = formatOf(m)
	schema.Text = textOf(m)
	schema.Array = arrayOf(m)
	schema.Object = objectOf(m, schema.At)
	schema.Choice = choiceOf(m)
	schema.Logic = logicOf(m)
	schema.Notes = notesOf(m)
	for _, key := range schema.Keys {
		open(schema, specrules.SlotOf(key.Name))
		if key.read() {
			fillNode(schema, key)
		}
	}
}

// fillNode reads key, a key of a slot open in schema, into the field the statement
// keeps as the node it was written as, which its row of fillKeywords sets: a
// numeric bound, which the model holds as a float, and a keyword the model
// holds no field for. A key of no such field fills nothing.
func fillNode(schema *Schema, key Key) {
	for _, fillKeyword := range fillKeywords {
		if fillKeyword.set != nil && fillKeyword.name == key.Name {
			fillKeyword.set(schema, key.Node)
			return
		}
	}
}

// fillKeyword is one keyword fill reads: from the parser's model, whose field
// clear empties on a copy of it, or from the node its key holds, which set
// keeps, or both. node returns the node the parser read the keyword's value
// from, and is nil for a keyword the parser holds no field for.
type fillKeyword struct {
	name  string
	node  func(*oas3.Schema) *yaml.Node
	clear func(*oas3.Schema)
	set   func(*Schema, *yaml.Node)
}

// fillKeywords are the keywords fill reads, one row each.
var fillKeywords = []fillKeyword{
	{name: "allOf", node: func(m *oas3.Schema) *yaml.Node { return m.GetCore().AllOf.ValueNode },
		clear: func(m *oas3.Schema) { m.AllOf = nil }},
	{name: "type", node: func(m *oas3.Schema) *yaml.Node { return m.GetCore().Type.ValueNode },
		clear: func(m *oas3.Schema) { m.Type = nil }},
	{name: "nullable", node: func(m *oas3.Schema) *yaml.Node { return m.GetCore().Nullable.ValueNode },
		clear: func(m *oas3.Schema) { m.Nullable = nil }},
	{name: "exclusiveMinimum", node: func(m *oas3.Schema) *yaml.Node { return m.GetCore().ExclusiveMinimum.ValueNode },
		clear: func(m *oas3.Schema) { m.ExclusiveMinimum = nil },
		set:   func(schema *Schema, node *yaml.Node) { schema.Number.ExclusiveMinimum = node }},
	{name: "exclusiveMaximum", node: func(m *oas3.Schema) *yaml.Node { return m.GetCore().ExclusiveMaximum.ValueNode },
		clear: func(m *oas3.Schema) { m.ExclusiveMaximum = nil },
		set:   func(schema *Schema, node *yaml.Node) { schema.Number.ExclusiveMaximum = node }},
	{name: "enum", node: func(m *oas3.Schema) *yaml.Node { return m.GetCore().Enum.ValueNode },
		clear: func(m *oas3.Schema) { m.Enum = nil }},
	{name: "const", node: func(m *oas3.Schema) *yaml.Node { return m.GetCore().Const.ValueNode },
		clear: func(m *oas3.Schema) { m.Const = nil }},
	{name: "format", node: func(m *oas3.Schema) *yaml.Node { return m.GetCore().Format.ValueNode },
		clear: func(m *oas3.Schema) { m.Format = nil }},
	{name: "minLength", node: func(m *oas3.Schema) *yaml.Node { return m.GetCore().MinLength.ValueNode },
		clear: func(m *oas3.Schema) { m.MinLength = nil }},
	{name: "maxLength", node: func(m *oas3.Schema) *yaml.Node { return m.GetCore().MaxLength.ValueNode },
		clear: func(m *oas3.Schema) { m.MaxLength = nil }},
	{name: "pattern", node: func(m *oas3.Schema) *yaml.Node { return m.GetCore().Pattern.ValueNode },
		clear: func(m *oas3.Schema) { m.Pattern = nil }},
	{name: "contentEncoding", node: func(m *oas3.Schema) *yaml.Node { return m.GetCore().ContentEncoding.ValueNode },
		clear: func(m *oas3.Schema) { m.ContentEncoding = nil }},
	{name: "contentMediaType", node: func(m *oas3.Schema) *yaml.Node { return m.GetCore().ContentMediaType.ValueNode },
		clear: func(m *oas3.Schema) { m.ContentMediaType = nil }},
	{name: "contentSchema", node: func(m *oas3.Schema) *yaml.Node { return m.GetCore().ContentSchema.ValueNode },
		clear: func(m *oas3.Schema) { m.ContentSchema = nil }},
	{name: "items", node: func(m *oas3.Schema) *yaml.Node { return m.GetCore().Items.ValueNode },
		clear: func(m *oas3.Schema) { m.Items = nil }},
	{name: "prefixItems", node: func(m *oas3.Schema) *yaml.Node { return m.GetCore().PrefixItems.ValueNode },
		clear: func(m *oas3.Schema) { m.PrefixItems = nil }},
	{name: "contains", node: func(m *oas3.Schema) *yaml.Node { return m.GetCore().Contains.ValueNode },
		clear: func(m *oas3.Schema) { m.Contains = nil }},
	{name: "minContains", node: func(m *oas3.Schema) *yaml.Node { return m.GetCore().MinContains.ValueNode },
		clear: func(m *oas3.Schema) { m.MinContains = nil }},
	{name: "maxContains", node: func(m *oas3.Schema) *yaml.Node { return m.GetCore().MaxContains.ValueNode },
		clear: func(m *oas3.Schema) { m.MaxContains = nil }},
	{name: "minItems", node: func(m *oas3.Schema) *yaml.Node { return m.GetCore().MinItems.ValueNode },
		clear: func(m *oas3.Schema) { m.MinItems = nil }},
	{name: "maxItems", node: func(m *oas3.Schema) *yaml.Node { return m.GetCore().MaxItems.ValueNode },
		clear: func(m *oas3.Schema) { m.MaxItems = nil }},
	{name: "uniqueItems", node: func(m *oas3.Schema) *yaml.Node { return m.GetCore().UniqueItems.ValueNode },
		clear: func(m *oas3.Schema) { m.UniqueItems = nil }},
	{name: "unevaluatedItems", node: func(m *oas3.Schema) *yaml.Node { return m.GetCore().UnevaluatedItems.ValueNode },
		clear: func(m *oas3.Schema) { m.UnevaluatedItems = nil }},
	{name: "properties", node: func(m *oas3.Schema) *yaml.Node { return m.GetCore().Properties.ValueNode },
		clear: func(m *oas3.Schema) { m.Properties = nil }},
	{name: "patternProperties", node: func(m *oas3.Schema) *yaml.Node { return m.GetCore().PatternProperties.ValueNode },
		clear: func(m *oas3.Schema) { m.PatternProperties = nil }},
	{name: "additionalProperties", node: func(m *oas3.Schema) *yaml.Node { return m.GetCore().AdditionalProperties.ValueNode },
		clear: func(m *oas3.Schema) { m.AdditionalProperties = nil }},
	{name: "propertyNames", node: func(m *oas3.Schema) *yaml.Node { return m.GetCore().PropertyNames.ValueNode },
		clear: func(m *oas3.Schema) { m.PropertyNames = nil }},
	{name: "unevaluatedProperties", node: func(m *oas3.Schema) *yaml.Node { return m.GetCore().UnevaluatedProperties.ValueNode },
		clear: func(m *oas3.Schema) { m.UnevaluatedProperties = nil }},
	{name: "required", node: func(m *oas3.Schema) *yaml.Node { return m.GetCore().Required.ValueNode },
		clear: func(m *oas3.Schema) { m.Required = nil }},
	{name: "minProperties", node: func(m *oas3.Schema) *yaml.Node { return m.GetCore().MinProperties.ValueNode },
		clear: func(m *oas3.Schema) { m.MinProperties = nil }},
	{name: "maxProperties", node: func(m *oas3.Schema) *yaml.Node { return m.GetCore().MaxProperties.ValueNode },
		clear: func(m *oas3.Schema) { m.MaxProperties = nil }},
	{name: "discriminator", node: func(m *oas3.Schema) *yaml.Node { return m.GetCore().Discriminator.ValueNode },
		clear: func(m *oas3.Schema) { m.Discriminator = nil }},
	{name: "oneOf", node: func(m *oas3.Schema) *yaml.Node { return m.GetCore().OneOf.ValueNode },
		clear: func(m *oas3.Schema) { m.OneOf = nil }},
	{name: "anyOf", node: func(m *oas3.Schema) *yaml.Node { return m.GetCore().AnyOf.ValueNode },
		clear: func(m *oas3.Schema) { m.AnyOf = nil }},
	{name: "not", node: func(m *oas3.Schema) *yaml.Node { return m.GetCore().Not.ValueNode },
		clear: func(m *oas3.Schema) { m.Not = nil }},
	{name: "if", node: func(m *oas3.Schema) *yaml.Node { return m.GetCore().If.ValueNode },
		clear: func(m *oas3.Schema) { m.If = nil }},
	{name: "then", node: func(m *oas3.Schema) *yaml.Node { return m.GetCore().Then.ValueNode },
		clear: func(m *oas3.Schema) { m.Then = nil }},
	{name: "else", node: func(m *oas3.Schema) *yaml.Node { return m.GetCore().Else.ValueNode },
		clear: func(m *oas3.Schema) { m.Else = nil }},
	{name: "dependentSchemas", node: func(m *oas3.Schema) *yaml.Node { return m.GetCore().DependentSchemas.ValueNode },
		clear: func(m *oas3.Schema) { m.DependentSchemas = nil }},
	{name: "title", node: func(m *oas3.Schema) *yaml.Node { return m.GetCore().Title.ValueNode },
		clear: func(m *oas3.Schema) { m.Title = nil }},
	{name: "description", node: func(m *oas3.Schema) *yaml.Node { return m.GetCore().Description.ValueNode },
		clear: func(m *oas3.Schema) { m.Description = nil }},
	{name: "default", node: func(m *oas3.Schema) *yaml.Node { return m.GetCore().Default.ValueNode },
		clear: func(m *oas3.Schema) { m.Default = nil }},
	{name: "example", node: func(m *oas3.Schema) *yaml.Node { return m.GetCore().Example.ValueNode },
		clear: func(m *oas3.Schema) { m.Example = nil }},
	{name: "examples", node: func(m *oas3.Schema) *yaml.Node { return m.GetCore().Examples.ValueNode },
		clear: func(m *oas3.Schema) { m.Examples = nil }},
	{name: "deprecated", node: func(m *oas3.Schema) *yaml.Node { return m.GetCore().Deprecated.ValueNode },
		clear: func(m *oas3.Schema) { m.Deprecated = nil }},
	{name: "readOnly", node: func(m *oas3.Schema) *yaml.Node { return m.GetCore().ReadOnly.ValueNode },
		clear: func(m *oas3.Schema) { m.ReadOnly = nil }},
	{name: "writeOnly", node: func(m *oas3.Schema) *yaml.Node { return m.GetCore().WriteOnly.ValueNode },
		clear: func(m *oas3.Schema) { m.WriteOnly = nil }},
	{name: "xml", node: func(m *oas3.Schema) *yaml.Node { return m.GetCore().XML.ValueNode },
		clear: func(m *oas3.Schema) { m.XML = nil }},
	{name: "externalDocs", node: func(m *oas3.Schema) *yaml.Node { return m.GetCore().ExternalDocs.ValueNode },
		clear: func(m *oas3.Schema) { m.ExternalDocs = nil }},
	{name: "minimum", node: func(m *oas3.Schema) *yaml.Node { return m.GetCore().Minimum.ValueNode },
		set: func(schema *Schema, node *yaml.Node) { schema.Number.Minimum = node }},
	{name: "maximum", node: func(m *oas3.Schema) *yaml.Node { return m.GetCore().Maximum.ValueNode },
		set: func(schema *Schema, node *yaml.Node) { schema.Number.Maximum = node }},
	{name: "multipleOf", node: func(m *oas3.Schema) *yaml.Node { return m.GetCore().MultipleOf.ValueNode },
		set: func(schema *Schema, node *yaml.Node) { schema.Number.MultipleOf = node }},
	{name: "additionalItems", set: func(schema *Schema, node *yaml.Node) { schema.Array.AdditionalItems = node }},
	{name: "dependentRequired", set: func(schema *Schema, node *yaml.Node) { schema.Object.DependentRequired = node }},
	{name: "dependencies", set: func(schema *Schema, node *yaml.Node) { schema.Logic.Dependencies = node }},
	{name: "$comment", set: func(schema *Schema, node *yaml.Node) { schema.Notes.Comment = node }},
}

// keywordRow returns the row of fillKeywords naming name, and false for a key
// fill does not read.
func keywordRow(name string) (fillKeyword, bool) {
	for _, fillKeyword := range fillKeywords {
		if fillKeyword.name == name {
			return fillKeyword, true
		}
	}
	return fillKeyword{}, false
}

// dropOutranked takes out of the keys of schema, a Truncated schema whose model is
// m, each key of a keyword fill reads whose value the parser read from
// another node: a source past nodeview.MergeDepthLimit writes the keyword too
// and comes first, so the key lists the value that loses. A keyword the parser
// holds no node for is kept only where schema writes it itself, as no merge
// outranks that (docs/parser-library.md#merge-keys); one left out leaves
// schema.Unknown too, so that no census reads it.
func dropOutranked(schema *Schema, m *oas3.Schema) {
	own := nodeview.WrittenChildren(schema.Node)
	kept := make([]Key, 0, len(schema.Keys))
	for _, key := range schema.Keys {
		if !outranked(key, own, m) {
			kept = append(kept, key)
			continue
		}
		schema.Unknown = slices.DeleteFunc(slices.Clone(schema.Unknown), func(name string) bool { return name == key.Name })
	}
	if len(kept) == len(schema.Keys) {
		return
	}
	schema.Keys = kept
	index(schema)
}

// outranked reports whether the value the parser read for key's keyword is not
// key's: key is no pair s writes itself, own, and the parser read the keyword from
// another node, or holds no node to tell. A key fill does not read is never
// outranked.
func outranked(key Key, own map[string]*yaml.Node, m *oas3.Schema) bool {
	fillKeyword, reads := keywordRow(key.Name)
	if !reads || nodeview.Deref(own[key.Name]) == key.Node {
		return false
	}
	return fillKeyword.node == nil || nodeview.Deref(fillKeyword.node(m)) != key.Node
}

// listedOnly returns a copy of m holding no keyword schema's keys do not list: schema
// is Truncated, so the model expands merges its keys stop short of, and a
// schema is read from its keys or not at all, never half from each
// (docs/parser-library.md#merge-keys). dropOutranked has left each keyword
// the keys list only where the parser read it from that key. The model's
// extensions and the keys it holds no field for stay, which the census of
// unknown keys reports.
func listedOnly(m *oas3.Schema, schema *Schema) *oas3.Schema {
	kept := *m
	for _, fillKeyword := range fillKeywords {
		if i, _ := schema.find(fillKeyword.name); i < 0 && fillKeyword.clear != nil {
			fillKeyword.clear(&kept)
		}
	}
	return &kept
}

// kindsOf returns what m's type and nullable say, nil where it holds neither.
func kindsOf(m *oas3.Schema) *Kinds {
	if m.Type == nil && m.Nullable == nil {
		return nil
	}
	kinds := &Kinds{Nullable: m.Nullable}
	for _, t := range m.GetType() {
		kinds.Types = append(kinds.Types, JSONSchemaTypeName(t))
	}
	return kinds
}

// numberOf returns the forms of m's exclusive bounds, nil where it holds
// neither: the bounds themselves are the nodes their keys hold (fillNode).
func numberOf(m *oas3.Schema) *Number {
	if m.ExclusiveMinimum == nil && m.ExclusiveMaximum == nil {
		return nil
	}
	return &Number{ExclusiveMinimumForm: exclusiveOf(m.ExclusiveMinimum),
		ExclusiveMaximumForm: exclusiveOf(m.ExclusiveMaximum)}
}

// exclusiveOf returns the arm the model read e, either exclusive bound, as.
func exclusiveOf(e oas3.ExclusiveMinimum) Exclusive {
	switch {
	case e == nil:
		return NoExclusive
	case !e.IsLeft():
		return ExclusiveBound
	case e.GetLeft() != nil && *e.GetLeft():
		return ExclusiveTrue
	default:
		return ExclusiveFalse
	}
}

// valuesOf returns what m's enum and const say, nil where it holds neither.
func valuesOf(m *oas3.Schema) *Values {
	if m.Enum == nil && m.Const == nil {
		return nil
	}
	return &Values{Enum: m.Enum, Const: m.Const}
}

// formatOf returns the format m names, nil where it names none.
func formatOf(m *oas3.Schema) *Format {
	if m.Format == nil {
		return nil
	}
	return &Format{Name: *m.Format}
}

// textOf returns what m's keywords asserting about a string say, nil where
// it holds none of them.
func textOf(m *oas3.Schema) *Text {
	if m.MinLength == nil && m.MaxLength == nil && m.Pattern == nil && m.ContentEncoding == nil &&
		m.ContentMediaType == nil && m.ContentSchema == nil {
		return nil
	}
	return &Text{MinLength: m.MinLength, MaxLength: m.MaxLength, Pattern: m.GetPattern(),
		ContentEncoding: m.GetContentEncoding(), ContentMediaType: m.GetContentMediaType(),
		ContentSchema: Subschema{m: m.ContentSchema}}
}

// arrayOf returns what m's keywords asserting about an array say, nil where
// it holds none of them.
func arrayOf(m *oas3.Schema) *Array {
	if m.Items == nil && m.PrefixItems == nil && m.Contains == nil && m.MinContains == nil &&
		m.MaxContains == nil && m.MinItems == nil && m.MaxItems == nil && m.UniqueItems == nil &&
		m.UnevaluatedItems == nil {
		return nil
	}
	return &Array{Items: Subschema{m: m.Items}, PrefixItems: listed(m.PrefixItems), Contains: Subschema{m: m.Contains},
		MinContains: m.MinContains, MaxContains: m.MaxContains, MinItems: m.MinItems, MaxItems: m.MaxItems,
		UniqueItems: m.UniqueItems, UnevaluatedItems: Subschema{m: m.UnevaluatedItems}}
}

// objectOf returns what m's keywords asserting about an object say, m being
// read at at, nil where it holds none of them.
func objectOf(m *oas3.Schema, at reftable.DocumentPointer) *Object {
	if m.Properties == nil && m.PatternProperties == nil && m.AdditionalProperties == nil &&
		m.PropertyNames == nil && m.UnevaluatedProperties == nil && m.Required == nil &&
		m.MinProperties == nil && m.MaxProperties == nil && m.Discriminator == nil {
		return nil
	}
	return &Object{Properties: named(m.GetProperties().Len(), m.GetProperties().All()),
		PatternProperties:    named(m.GetPatternProperties().Len(), m.GetPatternProperties().All()),
		AdditionalProperties: Subschema{m: m.AdditionalProperties}, PropertyNames: Subschema{m: m.PropertyNames},
		UnevaluatedProperties: Subschema{m: m.UnevaluatedProperties}, Required: m.Required,
		MinProperties: m.MinProperties, MaxProperties: m.MaxProperties, Discriminator: discriminatorOf(m.Discriminator, at)}
}

// discriminatorOf returns what d says, read where the walk reads it: under
// the schema read at at, a position the bindings know only where at is not
// the zero one.
func discriminatorOf(d *oas3.Discriminator, at reftable.DocumentPointer) *Discriminator {
	if d == nil {
		return nil
	}
	out := &Discriminator{PropertyName: d.GetPropertyName(), DefaultMapping: d.GetDefaultMapping(),
		Node: nodeview.Deref(d.GetRootNode()), Unknown: d.GetCore().GetUnknownProperties(),
		Extensions: listedExtensions(d.GetExtensions().Len(), d.GetExtensions().All())}
	if at != (reftable.DocumentPointer{}) {
		out.At = reftable.DocumentPointer{Doc: at.Doc, Pointer: at.Pointer + "/discriminator"}
	}
	if n := d.GetMapping().Len(); n > 0 {
		out.Mapping = make([]MappingEntry, 0, n)
		for value, target := range d.GetMapping().All() {
			out.Mapping = append(out.Mapping, MappingEntry{Value: value, Target: target})
		}
	}
	return out
}

// choiceOf returns what m's oneOf and anyOf say, nil where it holds neither.
func choiceOf(m *oas3.Schema) *Choice {
	if m.OneOf == nil && m.AnyOf == nil {
		return nil
	}
	return &Choice{OneOf: listed(m.OneOf), AnyOf: listed(m.AnyOf)}
}

// logicOf returns what m's in-place applicators say, nil where it holds none
// of them.
func logicOf(m *oas3.Schema) *Logic {
	if m.Not == nil && m.If == nil && m.Then == nil && m.Else == nil && m.DependentSchemas == nil {
		return nil
	}
	return &Logic{Not: Subschema{m: m.Not}, If: Subschema{m: m.If}, Then: Subschema{m: m.Then}, Else: Subschema{m: m.Else},
		DependentSchemas: named(m.GetDependentSchemas().Len(), m.GetDependentSchemas().All())}
}

// notesOf returns what m says of itself, nil where it holds none of it.
func notesOf(m *oas3.Schema) *Notes {
	if m.Title == nil && m.Description == nil && m.Default == nil && m.Example == nil && m.Examples == nil &&
		m.Deprecated == nil && m.ReadOnly == nil && m.WriteOnly == nil && m.XML == nil && m.ExternalDocs == nil {
		return nil
	}
	return &Notes{Title: m.GetTitle(), Description: m.GetDescription(), Default: m.Default, Example: m.Example,
		Examples: m.Examples, Deprecated: m.Deprecated, ReadOnly: m.ReadOnly, WriteOnly: m.WriteOnly,
		XML: xmlOf(m.XML), ExternalDocs: externalDocsOf(m.ExternalDocs)}
}

// xmlOf returns what x says.
func xmlOf(x *oas3.XML) *XML {
	if x == nil {
		return nil
	}
	return &XML{Name: x.Name, Namespace: x.Namespace, Prefix: x.Prefix, Attribute: x.Attribute, Wrapped: x.Wrapped,
		Node: nodeview.Deref(x.GetRootNode()), Unknown: x.GetCore().GetUnknownProperties(),
		Extensions: listedExtensions(x.GetExtensions().Len(), x.GetExtensions().All())}
}

// externalDocsOf returns what d says.
func externalDocsOf(d *oas3.ExternalDocumentation) *ExternalDocs {
	if d == nil {
		return nil
	}
	return &ExternalDocs{Description: d.GetDescription(), URL: d.GetURL(), Node: nodeview.Deref(d.GetRootNode()),
		Unknown:    d.GetCore().GetUnknownProperties(),
		Extensions: listedExtensions(d.GetExtensions().Len(), d.GetExtensions().All())}
}

// listed returns the schemas of a list keyword, each at its index.
func listed(list []*model) []Subschema {
	if len(list) == 0 {
		return nil
	}
	out := make([]Subschema, len(list))
	for i, js := range list {
		out[i] = Subschema{Index: i, m: js}
	}
	return out
}

// listedExtensions returns the n extensions all yields, in its order, nil for
// none. They are the model's past nodeview.MergeDepthLimit too, which
// listedOnly leaves, so the list names a key merged in there, which Keys lack.
func listedExtensions(n int, all iter.Seq2[string, *yaml.Node]) []Extension {
	if n == 0 {
		return nil
	}
	out := make([]Extension, 0, n)
	for name, node := range all {
		out = append(out, Extension{Name: name, Node: node})
	}
	return out
}

// named returns the n schemas of a mapping keyword, each under its name, in
// source order.
func named(n int, all iter.Seq2[string, *model]) []Subschema {
	if n == 0 {
		return nil
	}
	out := make([]Subschema, 0, n)
	for name, js := range all {
		out = append(out, Subschema{Name: name, m: js})
	}
	return out
}
