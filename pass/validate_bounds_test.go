// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package pass_test

import (
	"encoding/json/jsontext"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/ir"
)

var (
	intT  = ir.TypeRef{Target: ir.PrimitiveTypeID(ir.PrimitiveInteger)}
	strT  = ir.TypeRef{Target: ir.PrimitiveTypeID(ir.PrimitiveString)}
	boolT = ir.TypeRef{Target: ir.PrimitiveTypeID(ir.PrimitiveBool)}
	anyT  = ir.TypeRef{Target: ir.PrimitiveTypeID(ir.PrimitiveAny)}
)

// finding is a diagnostic as a row states it: where it sits and what it says.
type finding struct{ node, message string }

// provAt is the provenance the entity with the ID node is given in the
// fixtures of the checks that read what a source says, so a finding is held
// to carry its entity's own, with Node set.
func provAt(node string) ir.Provenance {
	return ir.Provenance{Pointer: jsontext.Pointer("/" + node)}
}

// valueDoc is validDoc plus the primitives the rows of these checks read.
func valueDoc() *ir.Document {
	doc := validDoc()
	for _, kind := range []ir.PrimitiveKind{ir.PrimitiveInteger, ir.PrimitiveBool, ir.PrimitiveDecimal, ir.PrimitiveAny} {
		id := ir.PrimitiveTypeID(kind)
		doc.Types[id] = &ir.Primitive{ID: id, PrimitiveKind: kind}
	}
	return doc
}

// prop is a property of the ID id and the type typ, carrying its own
// provenance and a wire name no other property has, shaped by set.
func prop(id string, typ ir.TypeRef, set func(*ir.Property)) ir.Property {
	p := ir.Property{ID: ir.PropertyID(id), WireName: id, Type: typ, Provenance: provAt(id)}
	if set != nil {
		set(&p)
	}
	return p
}

// modelOf is valueDoc holding the model t/a over props.
func modelOf(props ...ir.Property) *ir.Document {
	doc := valueDoc()
	doc.Types["type/a"] = &ir.Model{ID: "type/a", Properties: props, Provenance: provAt("type/a")}
	return doc
}

// big is the decimal s as written.
func big(s string) *ir.BigVal { return new(ir.BigVal(s)) }

// numeric is the Constraints of the numeric bounds that are not empty.
func numeric(minimum, exclusiveMin, maximum, exclusiveMax string) *ir.Constraints {
	c := &ir.Constraints{}
	for _, b := range []struct {
		to *(*ir.BigVal)
		s  string
	}{{&c.Min, minimum}, {&c.ExclusiveMin, exclusiveMin}, {&c.Max, maximum}, {&c.ExclusiveMax, exclusiveMax}} {
		if b.s != "" {
			*b.to = big(b.s)
		}
	}
	return c
}

// withMultiple sets the multiple c is a multiple of.
func withMultiple(c *ir.Constraints, s string) *ir.Constraints {
	c.MultipleOf = big(s)
	return c
}

// lengths is the Constraints bounding a string or bytes value.
func lengths(minimum, maximum int64) *ir.Constraints {
	return &ir.Constraints{MinLength: new(minimum), MaxLength: new(maximum)}
}

// bounded sets a property's constraints.
func bounded(c *ir.Constraints) func(*ir.Property) {
	return func(p *ir.Property) { p.Constraints = c }
}

// nullable makes a property's type admit null as well.
func nullable(p *ir.Property) { p.Type.Nullable = true }

// found returns what validated reports of doc under code, each finding held to
// be a warning carrying the provenance its node's entity does, with the node
// set; nil when there is none.
func found(t *testing.T, doc *ir.Document, code string) []finding {
	t.Helper()
	diags := withCode(validated(t, doc), code)
	if len(diags) == 0 {
		return nil
	}
	out := make([]finding, 0, len(diags))
	for _, d := range diags {
		assert.Equal(t, ir.SeverityWarning, d.Severity, d.Message)
		assert.Equal(t, ir.Provenance{Pointer: jsontext.Pointer("/" + d.Provenance.Node), Node: d.Provenance.Node},
			d.Provenance, "a finding carries its entity's provenance, with the node set: %s", d.Message)
		out = append(out, finding{d.Provenance.Node, d.Message})
	}
	return out
}

const boundsCode = "pass/bounds-admit-no-value"

// TestValidate_BoundsThatAdmitNoValueAreReported holds each shape of value to
// the bounds it is judged by, a union's kind by kind, with the nearest bounds
// that admit one value beside each that admit none: an exclusive bound only the
// other side meets exactly, a minimum compared as a number and not as text, a
// gap between two integers the check does not judge, and a multiple it does
// not look for.
func TestValidate_BoundsThatAdmitNoValueAreReported(t *testing.T) {
	t.Parallel()
	typed := func(id string, td func(c *ir.Constraints) ir.TypeDef, c *ir.Constraints) *ir.Document {
		doc := valueDoc()
		doc.Types[ir.TypeID(id)] = td(c)
		return doc
	}
	list := func(c *ir.Constraints) ir.TypeDef {
		return &ir.List{ID: "type/l", ElementType: strT, Constraints: c, Provenance: provAt("type/l")}
	}
	object := func(c *ir.Constraints) ir.TypeDef {
		return &ir.Model{ID: "type/o", Constraints: c, Provenance: provAt("type/o")}
	}
	union := func(c *ir.Constraints) ir.TypeDef {
		return &ir.Union{ID: "type/u", Variants: []ir.Variant{{Type: strT}, {Type: intT}}, Constraints: c, Provenance: provAt("type/u")}
	}
	// eachKind is bounds of every kind that admit one value apiece, which set
	// spoils for one kind.
	eachKind := func(set func(*ir.Constraints)) *ir.Constraints {
		c := numeric("5", "", "5", "")
		c.MinLength, c.MaxLength = new(int64(6)), new(int64(6))
		c.MinItems, c.MaxItems = new(int64(2)), new(int64(2))
		c.MinProperties, c.MaxProperties = new(int64(1)), new(int64(1))
		if set != nil {
			set(c)
		}
		return c
	}
	number := func(c *ir.Constraints) *ir.Document { return modelOf(prop("property/a/n", intT, bounded(c))) }
	atMost := func(msg string) []finding {
		return []finding{{"property/a/n", "bounds admit no number: none is " + msg}}
	}
	for _, tc := range []struct {
		name string
		doc  *ir.Document
		want []finding
	}{
		{"a minimum above the maximum", number(numeric("10", "", "5", "")), atMost("at least 10 and at most 5")},
		{"the same, spelled as written", number(numeric("2.0", "", "1", "")), atMost("at least 2.0 and at most 1")},
		{"an exclusive minimum at the maximum", number(numeric("", "5", "5", "")), atMost("above 5 and at most 5")},
		{"a minimum at an exclusive maximum", number(numeric("5", "", "", "5")), atMost("at least 5 and below 5")},
		{"two exclusive bounds that meet", number(numeric("", "5", "", "5")), atMost("above 5 and below 5")},
		{"a minimum that is the maximum", number(numeric("5", "", "5", "")), nil},
		{"a minimum below the maximum, which text puts above", number(numeric("5", "", "10", "")), nil},
		{"a minimum the maximum meets", number(numeric("10", "", "10", "")), nil},
		{"two exclusive bounds with no integer between", number(numeric("", "5", "", "6")), nil},
		{"a multiple of 5 in no range of 1 to 2", number(withMultiple(numeric("1", "", "2", ""), "5")), nil},
		{"a string with a minimum above its maximum", modelOf(prop("property/a/s", strT, bounded(lengths(6, 5)))),
			[]finding{{"property/a/s", "bounds admit no string: none has at least 6 characters and at most 5"}}},
		{"a string with a maximum that meets its minimum", modelOf(prop("property/a/s", strT, bounded(lengths(6, 6)))), nil},
		{"a string that is nullable, which still admits no string", modelOf(
			prop("property/a/s", strT, bounded(lengths(6, 5))), prop("property/a/t", strT, func(p *ir.Property) {
				bounded(lengths(6, 5))(p)
				nullable(p)
			})), []finding{
			{"property/a/s", "bounds admit no string: none has at least 6 characters and at most 5"},
			{"property/a/t", "bounds admit no string: none has at least 6 characters and at most 5"},
		}},
		{"bytes with a minimum above the maximum", func() *ir.Document {
			doc := modelOf(prop("property/a/b", ir.TypeRef{Target: "type/bytes"}, bounded(lengths(2, 1))))
			doc.Types["type/bytes"] = &ir.Primitive{ID: "type/bytes", PrimitiveKind: ir.PrimitiveBytes}
			return doc
		}(), []finding{{"property/a/b", "bounds admit no bytes: none has at least 2 bytes and at most 1"}}},
		{"a list with a minimum above its maximum", typed("type/l", list, &ir.Constraints{MinItems: new(int64(6)), MaxItems: new(int64(5))}),
			[]finding{{"type/l", "bounds admit no list: none has at least 6 items and at most 5"}}},
		{"a list with a maximum that meets its minimum", typed("type/l", list, &ir.Constraints{MinItems: new(int64(6)), MaxItems: new(int64(6))}), nil},
		{"an object with a minimum above its maximum", typed("type/o", object, &ir.Constraints{MinProperties: new(int64(2)), MaxProperties: new(int64(1))}),
			[]finding{{"type/o", "bounds admit no object: none has at least 2 members and at most 1"}}},
		{"an object with a maximum that meets its minimum", typed("type/o", object, &ir.Constraints{MinProperties: new(int64(2)), MaxProperties: new(int64(2))}), nil},
		{"a union whose bounds of each kind admit a value", typed("type/u", union, eachKind(nil)), nil},
		{"a union whose numbers admit none", typed("type/u", union, eachKind(func(c *ir.Constraints) { c.Min = big("6") })),
			[]finding{{"type/u", "bounds admit no number: none is at least 6 and at most 5"}}},
		{"a union whose strings admit none", typed("type/u", union, eachKind(func(c *ir.Constraints) { c.MinLength = new(int64(7)) })),
			[]finding{{"type/u", "bounds admit no string: none has at least 7 characters and at most 6"}}},
		{"a union whose lists admit none", typed("type/u", union, eachKind(func(c *ir.Constraints) { c.MinItems = new(int64(3)) })),
			[]finding{{"type/u", "bounds admit no list: none has at least 3 items and at most 2"}}},
		{"a union whose objects admit none", typed("type/u", union, eachKind(func(c *ir.Constraints) { c.MinProperties = new(int64(2)) })),
			[]finding{{"type/u", "bounds admit no object: none has at least 2 members and at most 1"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, found(t, tc.doc, boundsCode))
		})
	}
}

// TestValidate_BoundsAreJudgedOnlyForTheValueTheyApply holds a bound the type
// does not apply to, or that no one judges, to no finding: a length pair on an
// integer, on a type holding anything, on a union and on an enum, and a pair
// on a type nothing declares.
func TestValidate_BoundsAreJudgedOnlyForTheValueTheyApply(t *testing.T) {
	t.Parallel()
	pair := bounded(lengths(6, 5))
	doc := modelOf(
		prop("property/a/i", intT, pair),
		prop("property/a/y", anyT, pair),
		prop("property/a/u", ir.TypeRef{Target: "type/u"}, pair),
		prop("property/a/e", ir.TypeRef{Target: "type/e"}, pair),
		prop("property/a/g", ir.TypeRef{Target: "type/ghost"}, pair),
		prop("property/a/z", ir.TypeRef{}, pair),
		prop("property/a/f", boolT, pair),
	)
	doc.Types["type/u"] = &ir.Union{ID: "type/u", Variants: []ir.Variant{{Type: strT}}}
	doc.Types["type/e"] = &ir.Enum{ID: "type/e", ValueType: ir.PrimitiveString, Members: []ir.EnumMember{{ID: "enum-member/a", Value: ir.Value{Kind: ir.ValueString, String: "a"}}}}

	assert.Empty(t, found(t, doc, boundsCode))
}

// TestValidate_BoundsAreJoinedWithThoseOfTheAliasesAPositionReaches holds a
// contradiction to be told once, where it completes: at the property whose own
// bounds meet the alias's, and at the alias alone when it contradicts itself,
// whatever a property of it adds.
func TestValidate_BoundsAreJoinedWithThoseOfTheAliasesAPositionReaches(t *testing.T) {
	t.Parallel()
	alias := func(c *ir.Constraints) *ir.Scalar {
		return &ir.Scalar{ID: "type/s", Base: &strT, Constraints: c, Provenance: provAt("type/s")}
	}
	min6, max5 := &ir.Constraints{MinLength: new(int64(6))}, &ir.Constraints{MaxLength: new(int64(5))}
	onAlias := func(c *ir.Constraints, own *ir.Constraints) *ir.Document {
		doc := modelOf(prop("property/a/r", ir.TypeRef{Target: "type/s"}, bounded(own)))
		doc.Types["type/s"] = alias(c)
		return doc
	}
	const message = "bounds admit no string: none has at least 6 characters and at most "
	for _, tc := range []struct {
		name string
		doc  *ir.Document
		want []finding
	}{
		{"an alias's minimum and a property's maximum", onAlias(min6, max5), []finding{{"property/a/r", message + "5"}}},
		{"the same property with no bounds of its own", onAlias(min6, nil), nil},
		{"the same property with a maximum the minimum meets", onAlias(min6, &ir.Constraints{MaxLength: new(int64(6))}), nil},
		{"an alias that contradicts itself, and a property that tightens it",
			onAlias(lengths(6, 5), &ir.Constraints{MaxLength: new(int64(4))}), []finding{{"type/s", message + "5"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, found(t, tc.doc, boundsCode))
		})
	}
}

// hdr is a header of the ID id holding a string that no length admits.
func hdr(id string) ir.Header {
	return ir.Header{Property: prop(id, strT, bounded(lengths(6, 5)))}
}

// partsPayload is a payload whose one content encodes its parts by encoding
// and its items by item, each with the headers named.
func partsPayload(item *ir.Header, parts map[ir.PropertyID]ir.Header) *ir.Payload {
	content := ir.Content{MediaType: "multipart/mixed", Type: ir.TypeRef{Target: "type/m"}}
	if item != nil {
		content.ItemEncoding = &ir.PartEncoding{Headers: []ir.Header{*item}}
	}
	if len(parts) > 0 {
		content.Encoding = map[ir.PropertyID]ir.PartEncoding{}
		for key, h := range parts {
			content.Encoding[key] = ir.PartEncoding{Headers: []ir.Header{h}}
		}
	}
	return &ir.Payload{Contents: []ir.Content{content}}
}

// TestValidate_BoundsAreJudgedAtEverySiteAPositionSits holds each site a
// property or a parameter sits at to be read, in the order its table lists
// them: the types of the registry, in ID order, then the properties of each
// model, a common error's headers, each operation's parameters, response,
// default and error headers, and then each content's item encoding and part
// encodings, the parts by sorted key. The parts are declared in the reverse of
// that order, so a range over the map reddens it.
func TestValidate_BoundsAreJudgedAtEverySiteAPositionSits(t *testing.T) {
	t.Parallel()
	const message = "bounds admit no string: none has at least 6 characters and at most 5"
	doc := modelOf(prop("property/a/n", strT, bounded(lengths(6, 5))))
	doc.Types["type/o"] = &ir.Model{ID: "type/o", Constraints: &ir.Constraints{MinProperties: new(int64(2)), MaxProperties: new(int64(1))}, Provenance: provAt("type/o")}
	doc.Types["type/l"] = &ir.List{ID: "type/l", ElementType: strT, Constraints: &ir.Constraints{MinItems: new(int64(2)), MaxItems: new(int64(1))}, Provenance: provAt("type/l")}
	op := ir.Operation{
		ID: "operation/x", Provenance: provAt("operation/x"),
		Params:    []ir.Parameter{{ID: "parameter/x/q", Type: intT, Constraints: numeric("10", "", "5", ""), Provenance: provAt("parameter/x/q")}},
		Request:   partsPayload(new(hdr("h/req/item")), map[ir.PropertyID]ir.Header{"property/m/b": hdr("h/req/b"), "property/m/a": hdr("h/req/a")}),
		Responses: []ir.Response{{Headers: []ir.Header{hdr("h/resp")}, Payload: partsPayload(nil, map[ir.PropertyID]ir.Header{"property/m/a": hdr("h/resp/c")})}},
		Default:   &ir.Response{Headers: []ir.Header{hdr("h/def")}, Payload: partsPayload(nil, map[ir.PropertyID]ir.Header{"property/m/a": hdr("h/def/c")})},
		Errors:    []ir.ErrorCase{{Headers: []ir.Header{hdr("h/err")}, Payload: partsPayload(nil, map[ir.PropertyID]ir.Header{"property/m/a": hdr("h/err/c")})}},
	}
	doc.Services = []ir.Service{{
		ID: "service/x", Provenance: provAt("service/x"),
		CommonErrors: []ir.ErrorCase{{Headers: []ir.Header{hdr("h/ce")}}},
		Groups:       []ir.OperationGroup{{ID: "operation-group/x", Operations: []ir.Operation{op}}},
	}}
	str := finding{message: message}
	num := finding{message: "bounds admit no number: none is at least 10 and at most 5"}
	obj := finding{message: "bounds admit no object: none has at least 2 members and at most 1"}
	list := finding{message: "bounds admit no list: none has at least 2 items and at most 1"}
	at := func(f finding, node string) finding { f.node = node; return f }

	assert.Equal(t, []finding{
		at(list, "type/l"), at(obj, "type/o"),
		at(str, "property/a/n"), at(str, "h/ce"),
		at(num, "parameter/x/q"), at(str, "h/resp"), at(str, "h/def"), at(str, "h/err"),
		at(str, "h/req/item"), at(str, "h/req/a"), at(str, "h/req/b"),
		at(str, "h/resp/c"), at(str, "h/def/c"), at(str, "h/err/c"),
	}, found(t, doc, boundsCode))
}

// TestValidate_AnEntityWithNoIDIsReportedAtItsOwner holds a property or a header
// declaring no ID to be reported at the model or the operation it sits in,
// with its own provenance.
func TestValidate_AnEntityWithNoIDIsReportedAtItsOwner(t *testing.T) {
	t.Parallel()
	where := ir.Provenance{Pointer: "/where/it/was/written"}
	blank := prop("", strT, bounded(lengths(6, 5)))
	blank.Provenance = where
	header := hdr("")
	header.Provenance = where

	doc := modelOf(blank)
	doc.Services = []ir.Service{{ID: "service/x", Groups: []ir.OperationGroup{{ID: "operation-group/x", Operations: []ir.Operation{
		{ID: "operation/x", Responses: []ir.Response{{Headers: []ir.Header{header}}}},
	}}}}}

	got := withCode(validated(t, doc), boundsCode)

	assert.Equal(t, []string{"type/a", "operation/x"}, pointers(got))
	for _, d := range got {
		assert.Equal(t, ir.Provenance{Pointer: where.Pointer, Node: d.Provenance.Node}, d.Provenance)
	}
}
