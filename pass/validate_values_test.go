// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package pass_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/ir"
)

const (
	defaultCode = "pass/default-outside-type"
	exampleCode = "pass/example-outside-type"
)

// num is the number s, as written.
func num(s string) *ir.Value { return &ir.Value{Kind: ir.ValueNumber, Number: ir.BigVal(s)} }

// text is the string s.
func text(s string) *ir.Value { return &ir.Value{Kind: ir.ValueString, String: s} }

// null is the value null.
func null() *ir.Value { return &ir.Value{Kind: ir.ValueNull} }

// defaulted gives a property a default.
func defaulted(v *ir.Value) func(*ir.Property) {
	return func(p *ir.Property) { p.Default = v }
}

// exampled gives a property an example of each of vs.
func exampled(vs ...*ir.Value) func(*ir.Property) {
	return func(p *ir.Property) {
		for _, v := range vs {
			p.Examples = append(p.Examples, ir.Example{Value: v})
		}
	}
}

// TestValidate_ADefaultItsTypeDoesNotAdmitIsReported holds a default to the
// type of the position it sits at: its kind, an enum's members, a literal's
// value, null where the position admits none, the whole numbers of an integer
// and the bounds declared at the position. Each row is followed by the
// nearest default that is admitted, and so are the positions the check leaves
// alone: one encoded by its type or by the property, a union, and a reference
// to a constant.
func TestValidate_ADefaultItsTypeDoesNotAdmitIsReported(t *testing.T) {
	t.Parallel()
	enum := &ir.Enum{ID: "type/e", ValueType: ir.PrimitiveString, Members: []ir.EnumMember{
		{ID: "enum-member/red", Value: *text("red")}, {ID: "enum-member/green", Value: *text("green")},
	}}
	literal := &ir.Literal{ID: "type/four", Value: *num("4")}
	list := &ir.List{ID: "type/l", ElementType: strT}
	decimal := &ir.Scalar{ID: "type/dec", Base: &ir.TypeRef{Target: ir.PrimitiveTypeID(ir.PrimitiveDecimal)}, Encoding: &ir.Encoding{Name: "string"}}
	union := &ir.Union{ID: "type/u", Variants: []ir.Variant{{Type: intT}, {Type: strT}}}
	at := func(set func(*ir.Property), typ ir.TypeRef) *ir.Document {
		doc := modelOf(prop("property/a/c", typ, set))
		for _, td := range []ir.TypeDef{enum, literal, list, decimal, union} {
			doc.Types[td.Common().ID] = td
		}
		return doc
	}
	two := &ir.Value{Kind: ir.ValueList, List: []ir.Value{*text("a"), *text("b")}}
	one := &ir.Value{Kind: ir.ValueList, List: []ir.Value{*text("a")}}
	want := func(msg string) []finding { return []finding{{"property/a/c", "default " + msg}} }
	for _, tc := range []struct {
		name string
		doc  *ir.Document
		want []finding
	}{
		{"a string for an integer", at(defaulted(text("5")), intT), want("is a string, and type/primitive/integer holds an integer")},
		{"a number for an integer", at(defaulted(num("5")), intT), nil},
		{"a whole number written with a fraction", at(defaulted(num("1.0")), intT), nil},
		{"a fraction for an integer", at(defaulted(num("1.5")), intT), want("is no integer, and type/primitive/integer holds one")},
		{"a bool for a string", at(defaulted(&ir.Value{Kind: ir.ValueBool, Bool: true}), strT), want("is a bool, and type/primitive/string holds a string")},
		{"null for a type that admits none", at(defaulted(null()), boolT), want("is null, and type/primitive/bool is not nullable here")},
		{"null for a nullable form of it", at(func(p *ir.Property) { p.Default = null(); nullable(p) }, boolT), nil},
		{"a value outside an enum", at(defaulted(text("blue")), ir.TypeRef{Target: "type/e"}), want("is not a member of enum type/e")},
		{"a member of an enum", at(defaulted(text("green")), ir.TypeRef{Target: "type/e"}), nil},
		{"a number for an enum of strings", at(defaulted(num("1")), ir.TypeRef{Target: "type/e"}), want("is not a member of enum type/e")},
		{"a value other than a literal's", at(defaulted(num("5")), ir.TypeRef{Target: "type/four"}), want("is not the value of literal type/four")},
		{"a literal's value written another way", at(defaulted(num("4.0")), ir.TypeRef{Target: "type/four"}), nil},
		{"below the minimum", at(func(p *ir.Property) { p.Default = num("1"); p.Constraints = numeric("3", "", "", "") }, intT),
			want("lies outside the bounds declared for it")},
		{"at the minimum", at(func(p *ir.Property) { p.Default = num("3"); p.Constraints = numeric("3", "", "", "") }, intT), nil},
		{"at an exclusive minimum", at(func(p *ir.Property) { p.Default = num("3"); p.Constraints = numeric("", "3", "", "") }, intT),
			want("lies outside the bounds declared for it")},
		{"a string over its maximum length", at(func(p *ir.Property) { p.Default = text("ABC"); p.Constraints = lengths(0, 1) }, strT),
			want("lies outside the bounds declared for it")},
		{"a string of its maximum length", at(func(p *ir.Property) { p.Default = text("A"); p.Constraints = lengths(0, 1) }, strT), nil},
		{"a string of one character in two bytes", at(func(p *ir.Property) { p.Default = text("é"); p.Constraints = lengths(0, 1) }, strT), nil},
		{"a list over its maximum", at(func(p *ir.Property) { p.Default = two; p.Constraints = &ir.Constraints{MaxItems: new(int64(1))} },
			ir.TypeRef{Target: "type/l"}), want("lies outside the bounds declared for it")},
		{"a list of its maximum", at(func(p *ir.Property) { p.Default = one; p.Constraints = &ir.Constraints{MaxItems: new(int64(1))} },
			ir.TypeRef{Target: "type/l"}), nil},
		{"a string for an encoded decimal", at(defaulted(text("1.5")), ir.TypeRef{Target: "type/dec"}), nil},
		{"a string for an integer the property encodes", at(func(p *ir.Property) {
			p.Default = text("5")
			p.Encoding = &ir.Encoding{Name: "string"}
		}, intT), nil},
		{"a string for a union, one of whose variants admits it", at(defaulted(text("x")), ir.TypeRef{Target: "type/u"}), nil},
		{"a string for a type holding anything", at(defaulted(text("x")), anyT), nil},
		{"a reference to a constant", at(defaulted(&ir.Value{Kind: ir.ValueRefKind, Ref: &ir.ValueRef{}}), intT), nil},
		{"a string for a type nothing declares", at(defaulted(text("x")), ir.TypeRef{Target: "type/ghost"}), nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, found(t, tc.doc, defaultCode))
			assert.Empty(t, found(t, tc.doc, exampleCode), "a default is no example")
		})
	}
}

// TestValidate_AParametersDefaultIsHeldToItsType holds a parameter's default to
// its type at the parameter, as a property's is held at the property.
func TestValidate_AParametersDefaultIsHeldToItsType(t *testing.T) {
	t.Parallel()
	at := func(v *ir.Value) *ir.Document {
		doc := valueDoc()
		doc.Services = []ir.Service{{ID: "service/x", Groups: []ir.OperationGroup{{ID: "operation-group/x", Operations: []ir.Operation{{
			ID: "operation/x", Params: []ir.Parameter{{ID: "parameter/x/q", Type: intT, Default: v, Provenance: provAt("parameter/x/q")}},
		}}}}}}
		return doc
	}

	assert.Equal(t, []finding{{"parameter/x/q", "default is a string, and type/primitive/integer holds an integer"}},
		found(t, at(text("x")), defaultCode))
	assert.Empty(t, found(t, at(num("1")), defaultCode))
}

// TestValidate_AnExampleItsTypeDoesNotAdmitIsReported holds each example of a
// position, of a type and of an operation's scenario to what a default is held
// to, once for each example that fails and labelled by its name where it has
// one and its index where not, and leaves alone what is not a value of the
// type: a null example of a type, which says the example is absent, and the
// examples of a content, which are in a media type's format.
func TestValidate_AnExampleItsTypeDoesNotAdmitIsReported(t *testing.T) {
	t.Parallel()
	typeExamples := func(vs ...*ir.Value) *ir.Document {
		doc := valueDoc()
		l := &ir.List{ID: "type/l", ElementType: strT, Provenance: provAt("type/l")}
		for _, v := range vs {
			l.Examples = append(l.Examples, ir.Example{Value: v})
		}
		doc.Types["type/l"] = l
		return doc
	}
	scenario := func(in ir.Example) *ir.Document {
		doc := valueDoc()
		doc.Services = []ir.Service{{ID: "service/x", Groups: []ir.OperationGroup{{ID: "operation-group/x", Operations: []ir.Operation{{
			ID: "operation/x", Provenance: provAt("operation/x"), Examples: []ir.Example{in},
			Params: []ir.Parameter{{ID: "parameter/x/q", Type: intT}},
		}}}}}}
		return doc
	}
	params := func(v *ir.Value) ir.Example {
		return ir.Example{Name: "s", Input: &ir.ScenarioInput{Params: map[ir.ParameterID]ir.Value{"parameter/x/q": *v}}}
	}
	badNamed := ir.Example{Name: "bad", Value: text("x")}
	contentExample := valueDoc()
	body := &ir.Payload{Contents: []ir.Content{{
		MediaType: "application/json", Type: ir.TypeRef{Target: "type/m"}, Examples: []ir.Example{{Value: text("a string on a model")}},
	}}}
	contentExample.Services = []ir.Service{{ID: "service/x", Groups: []ir.OperationGroup{{ID: "operation-group/x", Operations: []ir.Operation{
		{ID: "operation/x", Responses: []ir.Response{{Payload: body}}},
	}}}}}
	for _, tc := range []struct {
		name string
		doc  *ir.Document
		want []finding
	}{
		{"a property's examples, one of which fails", func() *ir.Document {
			doc := modelOf(prop("property/a/e", intT, func(p *ir.Property) {
				p.Examples = []ir.Example{{Value: num("7")}, badNamed}
			}))
			return doc
		}(), []finding{{"property/a/e", `example "bad" is a string, and type/primitive/integer holds an integer`}}},
		{"a property's examples, labelled by index", modelOf(prop("property/a/e", intT, exampled(num("7"), text("x")))),
			[]finding{{"property/a/e", `example 1 is a string, and type/primitive/integer holds an integer`}}},
		{"a property's examples, all admitted", modelOf(prop("property/a/e", intT, exampled(num("7"), num("8")))), nil},
		{"a type's example, a string for a list", typeExamples(text("a")),
			[]finding{{"type/l", "example 0 is a string, and type/l holds a list"}}},
		{"a type's example, null", typeExamples(null()), nil},
		{"a type's example, a list", typeExamples(&ir.Value{Kind: ir.ValueList, List: []ir.Value{*text("a")}}), nil},
		{"a scenario's parameter, a string for an integer", scenario(params(text("x"))),
			[]finding{{"operation/x", `scenario "s" parameter "parameter/x/q" is a string, and type/primitive/integer holds an integer`}}},
		{"a scenario's parameter, an integer", scenario(params(num("1"))), nil},
		{"a scenario's two parameters, each a string, in ID order", func() *ir.Document {
			doc := scenario(ir.Example{Name: "s", Input: &ir.ScenarioInput{Params: map[ir.ParameterID]ir.Value{
				"parameter/x/q": *text("x"), "parameter/x/a": *text("y"),
			}}})
			op := &doc.Services[0].Groups[0].Operations[0]
			op.Params = append(op.Params, ir.Parameter{ID: "parameter/x/a", Type: intT})
			return doc
		}(), []finding{
			{"operation/x", `scenario "s" parameter "parameter/x/a" is a string, and type/primitive/integer holds an integer`},
			{"operation/x", `scenario "s" parameter "parameter/x/q" is a string, and type/primitive/integer holds an integer`},
		}},
		{"a scenario's parameter naming none the operation declares", scenario(ir.Example{Name: "s", Input: &ir.ScenarioInput{
			Params: map[ir.ParameterID]ir.Value{"parameter/x/none": *text("x")},
		}}), nil},
		{"a scenario's error, a string for an integer", scenario(ir.Example{Input: &ir.ScenarioInput{}, Error: &ir.ErrorExample{
			Type: intT, Content: *text("x"),
		}}), []finding{{"operation/x", "scenario 0 error is a string, and type/primitive/integer holds an integer"}}},
		{"a content's example, a string on a model", contentExample, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, found(t, tc.doc, exampleCode))
			assert.Empty(t, found(t, tc.doc, defaultCode), "an example is no default")
		})
	}
}
