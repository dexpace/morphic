// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package statement

import (
	"iter"

	"github.com/speakeasy-api/openapi/jsonschema/oas3"
	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/bind"
	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
	"github.com/dexpace/morphic/compilers/openapi/internal/oas"
)

// model is the parser's schema value, which this file alone reads.
type model = oas3.JSONSchema[oas3.Referenceable]

// Of returns the statement of js, read on the first ask and remembered: its
// schema object and its inline allOf entries, flattened. A nil js reads as
// Unwritten.
func (t *Table) Of(js *model) *Statement {
	t.lookups++
	if st, ok := t.statements[js]; ok {
		return st
	}
	st := &Statement{Schema: t.read(js)}
	st.of = st
	t.flatten(st)
	t.statements[js] = st
	return st
}

// Parsed returns the parser's schema object s was read from, nil for a nil s
// and for a value that is no schema object: what a reader not yet reading the
// statement reads (GitHub #1991). It goes once none does.
func Parsed(s *Schema) *oas3.Schema {
	if s == nil {
		return nil
	}
	return s.m.GetSchema()
}

// ParsedAt returns the parser's value st was read from, nil for a nil st: what
// a reader not yet reading the statement reads (GitHub #1991). It goes once
// none does.
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
func (t *Table) read(js *model) *Schema {
	if s, ok := t.schemas[js]; ok {
		return s
	}
	t.reads++
	s := &Schema{m: js, Truth: truthOf(js)}
	t.schemas[js] = s
	if js == nil {
		return s
	}
	s.At, _ = t.src.Bindings.Site(js)
	if s.Truth != Stated {
		s.Node = nodeview.Deref(js.GetCore().GetRootNode())
		return s
	}
	m := js.GetSchema()
	s.Node = nodeview.Deref(nodeview.DocumentRoot(m.GetRootNode()))
	s.Unknown = m.GetCore().GetUnknownProperties()
	s.Extensions = listedExtensions(m.GetExtensions().Len(), m.GetExtensions().All())
	readKeys(s, t.src.Rules.At(s.At))
	if kw := t.src.Bindings.RefKeyword(js); kw != "" {
		e, bound := t.src.Bindings.Entry(js)
		s.Ref = &Reference{Keyword: kw, Text: string(m.GetRef()), Entry: e, Bound: bound}
	}
	fill(s, m)
	return s
}

// Truth returns what the schema s names states, as Table.Sub reads it, with no
// lookup.
func (s Sub) Truth() Truth {
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
// write and for a nil or unstated s.
func (s *Schema) Property(name string) (Sub, bool) {
	if s == nil {
		return Sub{}, false
	}
	p, ok := s.m.GetSchema().GetProperties().Get(name)
	if !ok {
		return Sub{}, false
	}
	return Sub{Name: name, m: p}, true
}

// truthOf returns what js states. The parser reads a value written as no
// schema as the `false` schema, which truthOf reads as Unwritten by its node
// (docs/parser-library.md#null-schema-is-false), and a mapping it fails
// beneath as a boolean, which truthOf reads as Unread
// (docs/parser-library.md#failure-beneath-itself).
func truthOf(js *model) Truth {
	if js == nil {
		return Unwritten
	}
	n := nodeview.Deref(js.GetCore().GetRootNode())
	if n != nil && n.Kind != yaml.MappingNode && (n.Kind != yaml.ScalarNode || n.ShortTag() != "!!bool") {
		return Unwritten
	}
	switch {
	case !js.IsBool():
		return Stated
	case n != nil && n.Kind == yaml.MappingNode:
		return Unread
	case js.GetBool() != nil && !*js.GetBool():
		return Nothing
	default:
		return Anything
	}
}

// fill reads each slot's values. A typed value is m's, the parser's model,
// wherever it holds one, as the walk reads the model, whether or not s's keys
// list its keyword, which they do not past nodeview.MergeDepthLimit
// (docs/parser-library.md#merge-keys); a value kept as the node it was written
// as is its key's. Each keyword lands in its own slot (oas.SlotOf) whatever
// the rows say: a policy reads a 3.0 `nullable` in a 3.1 schema
// (docs/compilers/openapi.md#nullable), and the key's Slot carries the rows'
// verdict. A listed key whose value is not read opens its slot and fills
// nothing.
func fill(s *Schema, m *oas3.Schema) {
	s.AllOf = listed(m.AllOf)
	s.Kinds = kindsOf(m)
	s.Number = numberOf(m)
	s.Values = valuesOf(m)
	s.Format = formatOf(m)
	s.Text = textOf(m)
	s.Array = arrayOf(m)
	s.Object = objectOf(m, s.At)
	s.Choice = choiceOf(m)
	s.Logic = logicOf(m)
	s.Notes = notesOf(m)
	for _, k := range s.Keys {
		open(s, oas.SlotOf(k.Name))
		if k.read() {
			fillNode(s, k)
		}
	}
}

// fillNode reads k, a key of a slot open in s, into the field the statement
// keeps as the node it was written as: a numeric bound, which the model holds
// as a float, and a keyword the model holds no field for. A key of no such
// field fills nothing.
func fillNode(s *Schema, k Key) {
	switch k.Name {
	case "minimum":
		s.Number.Minimum = k.Node
	case "maximum":
		s.Number.Maximum = k.Node
	case "exclusiveMinimum":
		s.Number.ExclusiveMinimum = k.Node
	case "exclusiveMaximum":
		s.Number.ExclusiveMaximum = k.Node
	case "multipleOf":
		s.Number.MultipleOf = k.Node
	case "additionalItems":
		s.Array.AdditionalItems = k.Node
	case "dependentRequired":
		s.Object.DependentRequired = k.Node
	case "dependencies":
		s.Logic.Dependencies = k.Node
	case "$comment":
		s.Notes.Comment = k.Node
	}
}

// kindsOf returns what m's type and nullable say, nil where it holds neither.
func kindsOf(m *oas3.Schema) *Kinds {
	if m.Type == nil && m.Nullable == nil {
		return nil
	}
	kinds := &Kinds{Nullable: m.Nullable}
	for _, t := range m.GetType() {
		kinds.Types = append(kinds.Types, Kind(t))
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
		ContentSchema: Sub{m: m.ContentSchema}}
}

// arrayOf returns what m's keywords asserting about an array say, nil where
// it holds none of them.
func arrayOf(m *oas3.Schema) *Array {
	if m.Items == nil && m.PrefixItems == nil && m.Contains == nil && m.MinContains == nil &&
		m.MaxContains == nil && m.MinItems == nil && m.MaxItems == nil && m.UniqueItems == nil &&
		m.UnevaluatedItems == nil {
		return nil
	}
	return &Array{Items: Sub{m: m.Items}, PrefixItems: listed(m.PrefixItems), Contains: Sub{m: m.Contains},
		MinContains: m.MinContains, MaxContains: m.MaxContains, MinItems: m.MinItems, MaxItems: m.MaxItems,
		UniqueItems: m.UniqueItems, UnevaluatedItems: Sub{m: m.UnevaluatedItems}}
}

// objectOf returns what m's keywords asserting about an object say, m being
// read at at, nil where it holds none of them.
func objectOf(m *oas3.Schema, at bind.Position) *Object {
	if m.Properties == nil && m.PatternProperties == nil && m.AdditionalProperties == nil &&
		m.PropertyNames == nil && m.UnevaluatedProperties == nil && m.Required == nil &&
		m.MinProperties == nil && m.MaxProperties == nil && m.Discriminator == nil {
		return nil
	}
	return &Object{Properties: named(m.GetProperties().Len(), m.GetProperties().All()),
		PatternProperties:    named(m.GetPatternProperties().Len(), m.GetPatternProperties().All()),
		AdditionalProperties: Sub{m: m.AdditionalProperties}, PropertyNames: Sub{m: m.PropertyNames},
		UnevaluatedProperties: Sub{m: m.UnevaluatedProperties}, Required: m.Required,
		MinProperties: m.MinProperties, MaxProperties: m.MaxProperties, Discriminator: discriminatorOf(m.Discriminator, at)}
}

// discriminatorOf returns what d says, read where the walk reads it: under
// the schema read at at, a position the bindings know only where at is not
// the zero one.
func discriminatorOf(d *oas3.Discriminator, at bind.Position) *Discriminator {
	if d == nil {
		return nil
	}
	out := &Discriminator{PropertyName: d.GetPropertyName(), DefaultMapping: d.GetDefaultMapping(),
		Node: nodeview.Deref(d.GetRootNode()), Unknown: d.GetCore().GetUnknownProperties(),
		Extensions: listedExtensions(d.GetExtensions().Len(), d.GetExtensions().All())}
	if at != (bind.Position{}) {
		out.At = bind.Position{Doc: at.Doc, Pointer: at.Pointer + "/discriminator"}
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
	return &Logic{Not: Sub{m: m.Not}, If: Sub{m: m.If}, Then: Sub{m: m.Then}, Else: Sub{m: m.Else},
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
func listed(list []*model) []Sub {
	if len(list) == 0 {
		return nil
	}
	out := make([]Sub, len(list))
	for i, js := range list {
		out[i] = Sub{Index: i, m: js}
	}
	return out
}

// listedExtensions returns the n extensions all yields, in its order, nil for
// none. The model holds them past nodeview.MergeDepthLimit as it does a typed
// value, so the list names a key merged in there, which Keys lack.
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
func named(n int, all iter.Seq2[string, *model]) []Sub {
	if n == 0 {
		return nil
	}
	out := make([]Sub, 0, n)
	for name, js := range all {
		out = append(out, Sub{Name: name, m: js})
	}
	return out
}
