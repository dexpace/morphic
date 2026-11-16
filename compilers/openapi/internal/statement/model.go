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
	if st, ok := t.statements[js]; ok {
		return st
	}
	st := &Statement{Schema: t.read(js)}
	t.flatten(st)
	t.statements[js] = st
	return st
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
	readKeys(s, t.rules(s.At), m.GetCore().GetUnknownProperties())
	if kw := t.src.Bindings.RefKeyword(js); kw != "" {
		e, bound := t.src.Bindings.Entry(js)
		s.Ref = &Reference{Keyword: kw, Entry: e, Bound: bound}
	}
	fill(s, m)
	return s
}

// truthOf returns what js states, by the readings the lowering makes of it:
// annotation.IsNoSchema's of a value written as no schema, and that of a
// mapping the parser read as a boolean.
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

// fill reads each slot's values from m, a key at a time, each keyword into
// its own slot (oas.SlotOf) whatever the rows say of it, as the walk reads the
// model: a policy reads a 3.0 `nullable` in a 3.1 schema
// (docs/compilers/openapi.md#nullable), and the key's Slot carries the rows'
// verdict. A key whose value is not read opens its slot and fills nothing.
func fill(s *Schema, m *oas3.Schema) {
	for _, k := range s.Keys {
		slot := oas.SlotOf(k.Name)
		open(s, slot)
		if !k.read() {
			continue
		}
		switch slot {
		case oas.SlotConjunction:
			s.AllOf = listed(m.GetAllOf())
		case oas.SlotKinds:
			fillKinds(s.Kinds, k, m)
		case oas.SlotValues:
			fillValues(s.Values, k, m)
		case oas.SlotFormat:
			s.Format.Name = m.GetFormat()
		case oas.SlotNumber:
			fillNumber(s.Number, k)
		case oas.SlotText:
			fillText(s.Text, k, m)
		case oas.SlotArray:
			fillArray(s.Array, k, m)
		case oas.SlotObject:
			fillObject(s.Object, k, m, s.At)
		case oas.SlotChoice:
			fillChoice(s.Choice, k, m)
		case oas.SlotLogic:
			fillLogic(s.Logic, k, m)
		case oas.SlotNotes:
			fillNotes(s.Notes, k, m)
		default:
			// A reference is the bindings' reading, and the resource and an
			// unknown key are their keys alone.
		}
	}
}

// fillKinds reads k, a key of the kinds slot, into kinds.
func fillKinds(kinds *Kinds, k Key, m *oas3.Schema) {
	switch k.Name {
	case "type":
		for _, t := range m.GetType() {
			kinds.Types = append(kinds.Types, Kind(t))
		}
	case "nullable":
		kinds.Nullable = m.Nullable
	}
}

// fillValues reads k, a key of the values slot, into v.
func fillValues(v *Values, k Key, m *oas3.Schema) {
	switch k.Name {
	case "enum":
		v.Enum = m.GetEnum()
	case "const":
		v.Const = m.GetConst()
	}
}

// fillNumber reads k, a key of the number slot, into n, as the node it was
// written as, which the parser's model holds as a float.
func fillNumber(n *Number, k Key) {
	switch k.Name {
	case "minimum":
		n.Minimum = k.Node
	case "maximum":
		n.Maximum = k.Node
	case "exclusiveMinimum":
		n.ExclusiveMinimum = k.Node
	case "exclusiveMaximum":
		n.ExclusiveMaximum = k.Node
	case "multipleOf":
		n.MultipleOf = k.Node
	}
}

// fillText reads k, a key of the text slot, into text.
func fillText(text *Text, k Key, m *oas3.Schema) {
	switch k.Name {
	case "minLength":
		text.MinLength = m.GetMinLength()
	case "maxLength":
		text.MaxLength = m.GetMaxLength()
	case "pattern":
		text.Pattern = m.GetPattern()
	case "contentEncoding":
		text.ContentEncoding = m.GetContentEncoding()
	case "contentMediaType":
		text.ContentMediaType = m.GetContentMediaType()
	case "contentSchema":
		text.ContentSchema = Sub{m: m.GetContentSchema()}
	}
}

// fillArray reads k, a key of the array slot, into a.
func fillArray(a *Array, k Key, m *oas3.Schema) {
	switch k.Name {
	case "items":
		a.Items = Sub{m: m.GetItems()}
	case "prefixItems":
		a.PrefixItems = listed(m.GetPrefixItems())
	case "additionalItems":
		a.AdditionalItems = k.Node
	case "contains":
		a.Contains = Sub{m: m.GetContains()}
	case "minContains":
		a.MinContains = m.GetMinContains()
	case "maxContains":
		a.MaxContains = m.GetMaxContains()
	case "minItems":
		a.MinItems = m.MinItems
	case "maxItems":
		a.MaxItems = m.GetMaxItems()
	case "uniqueItems":
		a.UniqueItems = m.UniqueItems
	case "unevaluatedItems":
		a.UnevaluatedItems = Sub{m: m.GetUnevaluatedItems()}
	}
}

// fillObject reads k, a key of the object slot of the schema read at at, into
// o.
func fillObject(o *Object, k Key, m *oas3.Schema, at bind.Position) {
	switch k.Name {
	case "properties":
		o.Properties = named(m.GetProperties().Len(), m.GetProperties().All())
	case "patternProperties":
		o.PatternProperties = named(m.GetPatternProperties().Len(), m.GetPatternProperties().All())
	case "additionalProperties":
		o.AdditionalProperties = Sub{m: m.GetAdditionalProperties()}
	case "propertyNames":
		o.PropertyNames = Sub{m: m.GetPropertyNames()}
	case "unevaluatedProperties":
		o.UnevaluatedProperties = Sub{m: m.GetUnevaluatedProperties()}
	case "required":
		o.Required = m.GetRequired()
	case "dependentRequired":
		o.DependentRequired = k.Node
	case "minProperties":
		o.MinProperties = m.GetMinProperties()
	case "maxProperties":
		o.MaxProperties = m.GetMaxProperties()
	case "discriminator":
		o.Discriminator = discriminatorOf(m.GetDiscriminator(), at)
	}
}

// discriminatorOf returns what d says, read where the walk reads it: under
// the schema read at at, a position the bindings know only where at is not
// the zero one.
func discriminatorOf(d *oas3.Discriminator, at bind.Position) *Discriminator {
	if d == nil {
		return nil
	}
	out := &Discriminator{PropertyName: d.GetPropertyName(), DefaultMapping: d.GetDefaultMapping(),
		Node: nodeview.Deref(d.GetRootNode())}
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

// fillChoice reads k, a key of the choice slot, into c.
func fillChoice(c *Choice, k Key, m *oas3.Schema) {
	switch k.Name {
	case "oneOf":
		c.OneOf = listed(m.GetOneOf())
	case "anyOf":
		c.AnyOf = listed(m.GetAnyOf())
	}
}

// fillLogic reads k, a key of the logic slot, into l.
func fillLogic(l *Logic, k Key, m *oas3.Schema) {
	switch k.Name {
	case "not":
		l.Not = Sub{m: m.GetNot()}
	case "if":
		l.If = Sub{m: m.GetIf()}
	case "then":
		l.Then = Sub{m: m.GetThen()}
	case "else":
		l.Else = Sub{m: m.GetElse()}
	case "dependentSchemas":
		l.DependentSchemas = named(m.GetDependentSchemas().Len(), m.GetDependentSchemas().All())
	case "dependencies":
		l.Dependencies = k.Node
	}
}

// fillNotes reads k, a key of the notes slot, into n.
func fillNotes(n *Notes, k Key, m *oas3.Schema) {
	switch k.Name {
	case "title":
		n.Title = m.GetTitle()
	case "description":
		n.Description = m.GetDescription()
	case "default":
		n.Default = m.GetDefault()
	case "example":
		n.Example = m.GetExample()
	case "examples":
		n.Examples = m.GetExamples()
	case "deprecated":
		n.Deprecated = m.Deprecated
	case "readOnly":
		n.ReadOnly = m.ReadOnly
	case "writeOnly":
		n.WriteOnly = m.WriteOnly
	case "xml":
		n.XML = xmlOf(m.GetXML())
	case "externalDocs":
		n.ExternalDocs = externalDocsOf(m.GetExternalDocs())
	case "$comment":
		n.Comment = k.Node
	}
}

// xmlOf returns what x says.
func xmlOf(x *oas3.XML) *XML {
	if x == nil {
		return nil
	}
	return &XML{Name: x.Name, Namespace: x.Namespace, Prefix: x.Prefix, Attribute: x.Attribute, Wrapped: x.Wrapped,
		Node: nodeview.Deref(x.GetRootNode())}
}

// externalDocsOf returns what d says.
func externalDocsOf(d *oas3.ExternalDocumentation) *ExternalDocs {
	if d == nil {
		return nil
	}
	return &ExternalDocs{Description: d.GetDescription(), URL: d.GetURL(), Node: nodeview.Deref(d.GetRootNode())}
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
