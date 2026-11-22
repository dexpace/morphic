// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package pass_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/ir"
)

// listT is the type of the list of strings the rows of the serialized-example
// tests declare.
var listT = ir.TypeRef{Target: "type/l"}

// listOperation is valueDoc holding the list type/l and one operation whose
// parameter parameter/x/q has that type, shaped by set, and whose HTTP
// bindings are bindings.
func listOperation(set func(*ir.Parameter), bindings ...ir.HTTPBinding) *ir.Document {
	doc := valueDoc()
	doc.Types["type/l"] = &ir.List{ID: "type/l", ElementType: stringT, Provenance: provAt("type/l")}
	parameter := ir.Parameter{ID: "parameter/x/q", Type: listT, Provenance: provAt("parameter/x/q")}
	set(&parameter)
	doc.Services = []ir.Service{{ID: "service/x", Groups: []ir.OperationGroup{{ID: "operation-group/x", Operations: []ir.Operation{{
		ID: "operation/x", Params: []ir.Parameter{parameter}, Bindings: ir.OperationBindings{HTTP: bindings},
	}}}}}}
	return doc
}

// bindsQ is an HTTP binding of parameter/x/q in the query, shaped by set.
func bindsQ(set func(*ir.HTTPParamBinding)) ir.HTTPBinding {
	pb := ir.HTTPParamBinding{Param: "parameter/x/q", Location: ir.HTTPLocationQuery, WireName: "q", Style: "form"}
	set(&pb)
	return ir.HTTPBinding{Method: "GET", URITemplate: "/x{?q}", ParamBindings: []ir.HTTPParamBinding{pb}}
}

// styled sets a parameter binding's style and explode.
func styled(style string, explode *bool) func(*ir.HTTPParamBinding) {
	return func(pb *ir.HTTPParamBinding) { pb.Style, pb.Explode = style, explode }
}

// withExample gives a parameter the one example value.
func withExample(value *ir.Value) func(*ir.Parameter) {
	return func(parameter *ir.Parameter) { parameter.Examples = []ir.Example{{Value: value}} }
}

// TestValidate_AParametersSerializedExampleOfAListIsAccepted holds a scalar
// example of a list-typed parameter to be the list written out where an HTTP
// binding writes the whole parameter in a style that can write a list, as
// OpenAPI reads a parameter's example, and to be judged everywhere else: under
// no binding, a content type, deepObject, an exploded delimited style or a
// binding of a member, as null or an object, for a type no list, and as an
// example the list type gives, which is its value.
func TestValidate_AParametersSerializedExampleOfAListIsAccepted(t *testing.T) {
	t.Parallel()
	asString := "example 0 is a string, and type/l holds a list"
	judged := []finding{{"parameter/x/q", asString}}
	on, off := new(true), new(false)
	list := &ir.Value{Kind: ir.ValueList, List: []ir.Value{*text("a"), *text("b")}}
	for _, tc := range []struct {
		name string
		doc  *ir.Document
		want []finding
	}{
		{"a string under form", listOperation(withExample(text("a,b")), bindsQ(styled("form", off))), nil},
		{"a string under exploded form", listOperation(withExample(text("a")), bindsQ(styled("form", nil))), nil},
		{"a number under simple", listOperation(withExample(number("1")), bindsQ(styled("simple", nil))), nil},
		{"a bool under label", listOperation(withExample(&ir.Value{Kind: ir.ValueBool, Bool: true}), bindsQ(styled("label", nil))), nil},
		{"a string under matrix", listOperation(withExample(text(";q=a,b")), bindsQ(styled("matrix", nil))), nil},
		{"a string under cookie", listOperation(withExample(text("a")), bindsQ(styled("cookie", nil))), nil},
		{"a string under spaceDelimited", listOperation(withExample(text("a b")), bindsQ(styled("spaceDelimited", nil))), nil},
		{"a string under pipeDelimited, not exploded", listOperation(withExample(text("a|b")), bindsQ(styled("pipeDelimited", off))), nil},
		{"a string under pipeDelimited, exploded", listOperation(withExample(text("a")), bindsQ(styled("pipeDelimited", on))), judged},
		{"a string under spaceDelimited, exploded", listOperation(withExample(text("a")), bindsQ(styled("spaceDelimited", on))), judged},
		{"a string under deepObject", listOperation(withExample(text("a")), bindsQ(styled("deepObject", nil))), judged},
		{"a string under a style nothing defines", listOperation(withExample(text("a")), bindsQ(styled("csv", nil))), judged},
		{"a string under a content type", listOperation(withExample(text("a")), bindsQ(func(pb *ir.HTTPParamBinding) {
			pb.Style, pb.ContentType = "", "application/json"
		})), judged},
		{"a string under no binding", listOperation(withExample(text("a"))), judged},
		{"a string where a binding writes a member", listOperation(withExample(text("a")), bindsQ(func(pb *ir.HTTPParamBinding) {
			pb.ParamPath = []ir.PropertyID{"property/m/a"}
		})), judged},
		{"a string where a binding writes another parameter", listOperation(withExample(text("a")), bindsQ(func(pb *ir.HTTPParamBinding) {
			pb.Param = "parameter/x/other"
		})), judged},
		{"a string where a second binding writes it in a style", listOperation(withExample(text("a")),
			bindsQ(func(pb *ir.HTTPParamBinding) { pb.Style, pb.ContentType = "", "application/json" }), bindsQ(styled("form", nil))), nil},
		{"a list under form", listOperation(withExample(list), bindsQ(styled("form", nil))), nil},
		{"a list over its maximum under form", listOperation(func(parameter *ir.Parameter) {
			parameter.Examples = []ir.Example{{Value: list}}
			parameter.Constraints = &ir.Constraints{MaxItems: new(int64(1))}
		}, bindsQ(styled("form", nil))), []finding{{"parameter/x/q", "example 0 lies outside the bounds declared for it"}}},
		{"null under form", listOperation(withExample(null()), bindsQ(styled("form", nil))),
			[]finding{{"parameter/x/q", "example 0 is null, and type/l is not nullable here"}}},
		{"a string the parameter's list type gives under form", func() *ir.Document {
			doc := listOperation(func(*ir.Parameter) {}, bindsQ(styled("form", nil)))
			doc.Types["type/l"].Common().Examples = []ir.Example{{Value: text("a")}}
			return doc
		}(), []finding{{"type/l", asString}}},
		{"a string for an integer under form", listOperation(func(parameter *ir.Parameter) {
			parameter.Type, parameter.Examples = intT, []ir.Example{{Value: text("a")}}
		}, bindsQ(styled("form", nil))), []finding{{"parameter/x/q", "example 0 is a string, and type/primitive/integer holds an integer"}}},
		{"an object under form", listOperation(withExample(&ir.Value{Kind: ir.ValueObject}), bindsQ(styled("form", nil))),
			[]finding{{"parameter/x/q", "example 0 is an object, and type/l holds a list"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, found(t, tc.doc, exampleCode))
		})
	}
}

// TestValidate_ASerializedDefaultOfAListIsReported holds a parameter's and a
// header's default to its type where its examples may be written out: a
// default is a value an emitter writes into code, not a wire form.
func TestValidate_ASerializedDefaultOfAListIsReported(t *testing.T) {
	t.Parallel()
	parameter := listOperation(func(parameter *ir.Parameter) { parameter.Default = text("a,b") }, bindsQ(styled("form", nil)))
	header := headerOperation(func(header *ir.Header) { header.Default = text("a,b") })

	assert.Equal(t, []finding{{"parameter/x/q", "default is a string, and type/l holds a list"}}, found(t, parameter, defaultCode))
	assert.Equal(t, []finding{{"property/x/h", "default is a string, and type/l holds a list"}}, found(t, header, defaultCode))
}

// headerOperation is valueDoc holding the list type/l and one operation whose
// response, whose part and whose common error each declare a header of that
// type, the header property/x/h, shaped by set.
func headerOperation(set func(*ir.Header)) *ir.Document {
	doc := valueDoc()
	doc.Types["type/l"] = &ir.List{ID: "type/l", ElementType: stringT, Provenance: provAt("type/l")}
	header := ir.Header{Property: property("property/x/h", listT, nil)}
	set(&header)
	doc.Services = []ir.Service{{ID: "service/x", Groups: []ir.OperationGroup{{ID: "operation-group/x", Operations: []ir.Operation{{
		ID: "operation/x", Responses: []ir.Response{{Headers: []ir.Header{header}}},
	}}}}}}
	return doc
}

// TestValidate_AHeadersSerializedExampleOfAListIsAccepted holds a header's
// scalar example of a list to be the list written out where its style can
// write one, simple where it states none, as a parameter's is, and a model
// property's, which has no style, to be judged.
func TestValidate_AHeadersSerializedExampleOfAListIsAccepted(t *testing.T) {
	t.Parallel()
	judged := []finding{{"property/x/h", "example 0 is a string, and type/l holds a list"}}
	example := func(style string, explode *bool) func(*ir.Header) {
		return func(header *ir.Header) {
			header.Examples, header.Style, header.Explode = []ir.Example{{Value: text("a,b")}}, style, explode
		}
	}
	part := headerOperation(example("", nil))
	operation := &part.Services[0].Groups[0].Operations[0]
	operation.Request = &ir.Payload{Contents: []ir.Content{{
		MediaType: "multipart/form-data", Type: listT,
		Encoding: map[ir.PropertyID]ir.PartEncoding{"property/m/p": {Headers: operation.Responses[0].Headers}},
	}}}
	operation.Responses = nil
	commonError := headerOperation(example("", nil))
	commonError.Services[0].CommonErrors = []ir.ErrorCase{{Headers: commonError.Services[0].Groups[0].Operations[0].Responses[0].Headers}}
	commonError.Services[0].Groups[0].Operations[0].Responses = nil
	model := modelOf(property("property/x/h", listT, exampled(text("a,b"))))
	model.Types["type/l"] = &ir.List{ID: "type/l", ElementType: stringT, Provenance: provAt("type/l")}
	for _, tc := range []struct {
		name string
		doc  *ir.Document
		want []finding
	}{
		{"a response's header stating no style", headerOperation(example("", nil)), nil},
		{"a response's header under simple", headerOperation(example("simple", nil)), nil},
		{"a response's header under deepObject", headerOperation(example("deepObject", nil)), judged},
		{"a response's header under spaceDelimited, exploded", headerOperation(example("spaceDelimited", new(true))), judged},
		{"a part's header", part, nil},
		{"a common error's header", commonError, nil},
		{"a model's property", model, judged},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, found(t, tc.doc, exampleCode))
		})
	}
}
