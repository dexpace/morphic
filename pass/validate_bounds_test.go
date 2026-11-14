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
	intT  = ir.TypeRef{Target: ir.PrimTypeID(ir.PrimInteger)}
	strT  = ir.TypeRef{Target: ir.PrimTypeID(ir.PrimString)}
	boolT = ir.TypeRef{Target: ir.PrimTypeID(ir.PrimBool)}
	anyT  = ir.TypeRef{Target: ir.PrimTypeID(ir.PrimAny)}
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
	for _, kind := range []ir.PrimKind{ir.PrimInteger, ir.PrimBool, ir.PrimDecimal, ir.PrimAny} {
		id := ir.PrimTypeID(kind)
		doc.Types[id] = &ir.Primitive{ID: id, Prim: kind}
	}
	return doc
}

// prop is a property of the ID id and the type typ, carrying its own
// provenance and a wire name no other property has, shaped by set.
func prop(id string, typ ir.TypeRef, set func(*ir.Property)) ir.Property {
	p := ir.Property{ID: ir.PropID(id), WireName: id, Type: typ, Provenance: provAt(id)}
	if set != nil {
		set(&p)
	}
	return p
}

// modelOf is valueDoc holding the model t/a over props.
func modelOf(props ...ir.Property) *ir.Document {
	doc := valueDoc()
	doc.Types["t/a"] = &ir.Model{ID: "t/a", Properties: props, Provenance: provAt("t/a")}
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
// the bounds it is judged by, with the nearest bounds that admit one value
// beside each that admit none: an exclusive bound only the other side meets
// exactly, a minimum compared as a number and not as text, a gap between two
// integers the check does not judge, and a multiple it does not look for.
func TestValidate_BoundsThatAdmitNoValueAreReported(t *testing.T) {
	t.Parallel()
	typed := func(id string, td func(c *ir.Constraints) ir.TypeDef, c *ir.Constraints) *ir.Document {
		doc := valueDoc()
		doc.Types[ir.TypeID(id)] = td(c)
		return doc
	}
	list := func(c *ir.Constraints) ir.TypeDef {
		return &ir.List{ID: "t/l", Elem: strT, Constraints: c, Provenance: provAt("t/l")}
	}
	object := func(c *ir.Constraints) ir.TypeDef {
		return &ir.Model{ID: "t/o", Constraints: c, Provenance: provAt("t/o")}
	}
	number := func(c *ir.Constraints) *ir.Document { return modelOf(prop("p/a/n", intT, bounded(c))) }
	atMost := func(msg string) []finding {
		return []finding{{"p/a/n", "bounds admit no number: none is " + msg}}
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
		{"a string with a minimum above its maximum", modelOf(prop("p/a/s", strT, bounded(lengths(6, 5)))),
			[]finding{{"p/a/s", "bounds admit no string: none has at least 6 characters and at most 5"}}},
		{"a string with a maximum that meets its minimum", modelOf(prop("p/a/s", strT, bounded(lengths(6, 6)))), nil},
		{"a string that is nullable, which still admits no string", modelOf(
			prop("p/a/s", strT, bounded(lengths(6, 5))), prop("p/a/t", strT, func(p *ir.Property) {
				bounded(lengths(6, 5))(p)
				nullable(p)
			})), []finding{
			{"p/a/s", "bounds admit no string: none has at least 6 characters and at most 5"},
			{"p/a/t", "bounds admit no string: none has at least 6 characters and at most 5"},
		}},
		{"bytes with a minimum above the maximum", func() *ir.Document {
			doc := modelOf(prop("p/a/b", ir.TypeRef{Target: "t/bytes"}, bounded(lengths(2, 1))))
			doc.Types["t/bytes"] = &ir.Primitive{ID: "t/bytes", Prim: ir.PrimBytes}
			return doc
		}(), []finding{{"p/a/b", "bounds admit no bytes: none has at least 2 bytes and at most 1"}}},
		{"a list with a minimum above its maximum", typed("t/l", list, &ir.Constraints{MinItems: new(int64(6)), MaxItems: new(int64(5))}),
			[]finding{{"t/l", "bounds admit no list: none has at least 6 items and at most 5"}}},
		{"a list with a maximum that meets its minimum", typed("t/l", list, &ir.Constraints{MinItems: new(int64(6)), MaxItems: new(int64(6))}), nil},
		{"an object with a minimum above its maximum", typed("t/o", object, &ir.Constraints{MinProps: new(int64(2)), MaxProps: new(int64(1))}),
			[]finding{{"t/o", "bounds admit no object: none has at least 2 members and at most 1"}}},
		{"an object with a maximum that meets its minimum", typed("t/o", object, &ir.Constraints{MinProps: new(int64(2)), MaxProps: new(int64(2))}), nil},
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
		prop("p/a/i", intT, pair),
		prop("p/a/y", anyT, pair),
		prop("p/a/u", ir.TypeRef{Target: "t/u"}, pair),
		prop("p/a/e", ir.TypeRef{Target: "t/e"}, pair),
		prop("p/a/g", ir.TypeRef{Target: "t/ghost"}, pair),
		prop("p/a/z", ir.TypeRef{}, pair),
		prop("p/a/f", boolT, pair),
	)
	doc.Types["t/u"] = &ir.Union{ID: "t/u", Variants: []ir.Variant{{Type: strT}}}
	doc.Types["t/e"] = &ir.Enum{ID: "t/e", ValueType: ir.PrimString, Members: []ir.EnumMember{{ID: "e/a", Value: ir.Value{Kind: ir.ValueString, Str: "a"}}}}

	assert.Empty(t, found(t, doc, boundsCode))
}

// TestValidate_BoundsAreJoinedWithThoseOfTheAliasesAPositionReaches holds a
// contradiction to be told once, where it completes: at the property whose own
// bounds meet the alias's, and at the alias alone when it contradicts itself,
// whatever a property of it adds.
func TestValidate_BoundsAreJoinedWithThoseOfTheAliasesAPositionReaches(t *testing.T) {
	t.Parallel()
	alias := func(c *ir.Constraints) *ir.Scalar {
		return &ir.Scalar{ID: "t/s", Base: &strT, Constraints: c, Provenance: provAt("t/s")}
	}
	min6, max5 := &ir.Constraints{MinLength: new(int64(6))}, &ir.Constraints{MaxLength: new(int64(5))}
	onAlias := func(c *ir.Constraints, own *ir.Constraints) *ir.Document {
		doc := modelOf(prop("p/a/r", ir.TypeRef{Target: "t/s"}, bounded(own)))
		doc.Types["t/s"] = alias(c)
		return doc
	}
	const message = "bounds admit no string: none has at least 6 characters and at most "
	for _, tc := range []struct {
		name string
		doc  *ir.Document
		want []finding
	}{
		{"an alias's minimum and a property's maximum", onAlias(min6, max5), []finding{{"p/a/r", message + "5"}}},
		{"the same property with no bounds of its own", onAlias(min6, nil), nil},
		{"the same property with a maximum the minimum meets", onAlias(min6, &ir.Constraints{MaxLength: new(int64(6))}), nil},
		{"an alias that contradicts itself, and a property that tightens it",
			onAlias(lengths(6, 5), &ir.Constraints{MaxLength: new(int64(4))}), []finding{{"t/s", message + "5"}}},
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
func partsPayload(item *ir.Header, parts map[ir.PropID]ir.Header) *ir.Payload {
	content := ir.Content{MediaType: "multipart/mixed", Type: ir.TypeRef{Target: "t/m"}}
	if item != nil {
		content.ItemEncoding = &ir.PartEncoding{Headers: []ir.Header{*item}}
	}
	if len(parts) > 0 {
		content.Encoding = map[ir.PropID]ir.PartEncoding{}
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
	doc := modelOf(prop("p/a/n", strT, bounded(lengths(6, 5))))
	doc.Types["t/o"] = &ir.Model{ID: "t/o", Constraints: &ir.Constraints{MinProps: new(int64(2)), MaxProps: new(int64(1))}, Provenance: provAt("t/o")}
	doc.Types["t/l"] = &ir.List{ID: "t/l", Elem: strT, Constraints: &ir.Constraints{MinItems: new(int64(2)), MaxItems: new(int64(1))}, Provenance: provAt("t/l")}
	op := ir.Operation{
		ID: "op/x", Provenance: provAt("op/x"),
		Params:    []ir.Parameter{{ID: "param/x/q", Type: intT, Constraints: numeric("10", "", "5", ""), Provenance: provAt("param/x/q")}},
		Request:   partsPayload(new(hdr("h/req/item")), map[ir.PropID]ir.Header{"p/m/b": hdr("h/req/b"), "p/m/a": hdr("h/req/a")}),
		Responses: []ir.Response{{Headers: []ir.Header{hdr("h/resp")}, Payload: partsPayload(nil, map[ir.PropID]ir.Header{"p/m/a": hdr("h/resp/c")})}},
		Default:   &ir.Response{Headers: []ir.Header{hdr("h/def")}, Payload: partsPayload(nil, map[ir.PropID]ir.Header{"p/m/a": hdr("h/def/c")})},
		Errors:    []ir.ErrorCase{{Headers: []ir.Header{hdr("h/err")}, Payload: partsPayload(nil, map[ir.PropID]ir.Header{"p/m/a": hdr("h/err/c")})}},
	}
	doc.Services = []ir.Service{{
		ID: "s/x", Provenance: provAt("s/x"),
		CommonErrors: []ir.ErrorCase{{Headers: []ir.Header{hdr("h/ce")}}},
		Groups:       []ir.OperationGroup{{ID: "g/x", Operations: []ir.Operation{op}}},
	}}
	str := finding{message: message}
	num := finding{message: "bounds admit no number: none is at least 10 and at most 5"}
	obj := finding{message: "bounds admit no object: none has at least 2 members and at most 1"}
	list := finding{message: "bounds admit no list: none has at least 2 items and at most 1"}
	at := func(f finding, node string) finding { f.node = node; return f }

	assert.Equal(t, []finding{
		at(list, "t/l"), at(obj, "t/o"),
		at(str, "p/a/n"), at(str, "h/ce"),
		at(num, "param/x/q"), at(str, "h/resp"), at(str, "h/def"), at(str, "h/err"),
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
	doc.Services = []ir.Service{{ID: "s/x", Groups: []ir.OperationGroup{{ID: "g/x", Operations: []ir.Operation{
		{ID: "op/x", Responses: []ir.Response{{Headers: []ir.Header{header}}}},
	}}}}}

	got := withCode(validated(t, doc), boundsCode)

	assert.Equal(t, []string{"t/a", "op/x"}, pointers(got))
	for _, d := range got {
		assert.Equal(t, ir.Provenance{Pointer: where.Pointer, Node: d.Provenance.Node}, d.Provenance)
	}
}
