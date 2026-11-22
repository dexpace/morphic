// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package pass_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/ir"
)

// listT, objectT and mapT are the list of strings, the model and the map of
// integers the rows of the serialized-example tests declare.
var (
	listT   = ir.TypeRef{Target: "type/l"}
	objectT = ir.TypeRef{Target: "type/o"}
	mapT    = ir.TypeRef{Target: "type/m"}
)

// structuredDoc is valueDoc holding listT, objectT and mapT.
func structuredDoc() *ir.Document {
	doc := valueDoc()
	doc.Types["type/l"] = &ir.List{ID: "type/l", ElementType: stringT, Provenance: provAt("type/l")}
	doc.Types["type/o"] = &ir.Model{ID: "type/o", Provenance: provAt("type/o")}
	doc.Types["type/m"] = &ir.MapT{ID: "type/m", Value: intT, Provenance: provAt("type/m")}
	return doc
}

// parameterOperation is structuredDoc holding one operation whose parameter
// parameter/x/q is of type listT, shaped by set, and whose HTTP bindings are
// bindings.
func parameterOperation(set func(*ir.Parameter), bindings ...ir.HTTPBinding) *ir.Document {
	doc := structuredDoc()
	parameter := ir.Parameter{ID: "parameter/x/q", Type: listT, Provenance: provAt("parameter/x/q")}
	set(&parameter)
	doc.Services = []ir.Service{{ID: "service/x", Groups: []ir.OperationGroup{{ID: "operation-group/x", Operations: []ir.Operation{{
		ID: "operation/x", Params: []ir.Parameter{parameter}, Bindings: ir.OperationBindings{HTTP: bindings},
		Provenance: provAt("operation/x"),
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

// ofType gives a parameter the type typ and the one example value.
func ofType(typ ir.TypeRef, value *ir.Value) func(*ir.Parameter) {
	return func(parameter *ir.Parameter) { parameter.Type, parameter.Examples = typ, []ir.Example{{Value: value}} }
}

// TestValidate_AParametersSerializedExampleOfAListOrAnObjectIsAccepted holds a
// scalar example of a list-typed or object-typed parameter to be the value
// written out where an HTTP binding writes the whole parameter in a style that
// can write that shape, as OpenAPI reads a parameter's example, or a content
// type serializes it, and to be judged everywhere else: under no binding, a
// style that writes no such shape or a binding of a member, as null, bytes, a
// symbol or the other structured shape, for a type of neither shape, as an
// example the type gives and as a scenario's input.
func TestValidate_AParametersSerializedExampleOfAListOrAnObjectIsAccepted(t *testing.T) {
	t.Parallel()
	asString := "example 0 is a string, and type/l holds a list"
	judged := []finding{{"parameter/x/q", asString}}
	objectJudged := []finding{{"parameter/x/q", "example 0 is a string, and type/o holds an object"}}
	on, off := new(true), new(false)
	list := &ir.Value{Kind: ir.ValueList, List: []ir.Value{*text("a"), *text("b")}}
	for _, tc := range []struct {
		name string
		doc  *ir.Document
		want []finding
	}{
		{"a string under form", parameterOperation(withExample(text("a,b")), bindsQ(styled("form", off))), nil},
		{"a string under exploded form", parameterOperation(withExample(text("a")), bindsQ(styled("form", nil))), nil},
		{"a number under simple", parameterOperation(withExample(number("1")), bindsQ(styled("simple", nil))), nil},
		{"a bool under label", parameterOperation(withExample(&ir.Value{Kind: ir.ValueBool, Bool: true}), bindsQ(styled("label", nil))), nil},
		{"a string under matrix", parameterOperation(withExample(text(";q=a,b")), bindsQ(styled("matrix", nil))), nil},
		{"a string under cookie", parameterOperation(withExample(text("a")), bindsQ(styled("cookie", nil))), nil},
		{"a string under spaceDelimited", parameterOperation(withExample(text("a b")), bindsQ(styled("spaceDelimited", nil))), nil},
		{"a string under pipeDelimited, not exploded", parameterOperation(withExample(text("a|b")), bindsQ(styled("pipeDelimited", off))), nil},
		{"a string under pipeDelimited, exploded", parameterOperation(withExample(text("a")), bindsQ(styled("pipeDelimited", on))), judged},
		{"a string under spaceDelimited, exploded", parameterOperation(withExample(text("a")), bindsQ(styled("spaceDelimited", on))), judged},
		{"a string under deepObject", parameterOperation(withExample(text("a")), bindsQ(styled("deepObject", nil))), judged},
		{"a string under a style nothing defines", parameterOperation(withExample(text("a")), bindsQ(styled("csv", nil))), judged},
		{"a string under a content type", parameterOperation(withExample(text("a")), bindsQ(func(pb *ir.HTTPParamBinding) {
			pb.Style, pb.ContentType = "", "application/json"
		})), nil},
		{"a string under no binding", parameterOperation(withExample(text("a"))), judged},
		{"a string where a binding writes a member", parameterOperation(withExample(text("a")), bindsQ(func(pb *ir.HTTPParamBinding) {
			pb.ParamPath = []ir.PropertyID{"property/m/a"}
		})), judged},
		{"a string where a binding writes another parameter", parameterOperation(withExample(text("a")), bindsQ(func(pb *ir.HTTPParamBinding) {
			pb.Param = "parameter/x/other"
		})), judged},
		{"a string where a second binding writes it in a style", parameterOperation(withExample(text("a")),
			bindsQ(func(pb *ir.HTTPParamBinding) { pb.Style, pb.ContentType = "", "application/json" }), bindsQ(styled("form", nil))), nil},
		{"a string where a second binding writes it in a style that writes no list", parameterOperation(withExample(text("a")),
			bindsQ(styled("form", nil)), bindsQ(styled("deepObject", nil))), nil},
		{"a list under form", parameterOperation(withExample(list), bindsQ(styled("form", nil))), nil},
		{"a list over its maximum under form", parameterOperation(func(parameter *ir.Parameter) {
			parameter.Examples = []ir.Example{{Value: list}}
			parameter.Constraints = &ir.Constraints{MaxItems: new(int64(1))}
		}, bindsQ(styled("form", nil))), []finding{{"parameter/x/q", "example 0 lies outside the bounds declared for it"}}},
		{"null under form", parameterOperation(withExample(null()), bindsQ(styled("form", nil))),
			[]finding{{"parameter/x/q", "example 0 is null, and type/l is not nullable here"}}},
		{"a string the parameter's list type gives under form", func() *ir.Document {
			doc := parameterOperation(func(*ir.Parameter) {}, bindsQ(styled("form", nil)))
			doc.Types["type/l"].Common().Examples = []ir.Example{{Value: text("a")}}
			return doc
		}(), []finding{{"type/l", asString}}},
		{"a string for an integer under form", parameterOperation(func(parameter *ir.Parameter) {
			parameter.Type, parameter.Examples = intT, []ir.Example{{Value: text("a")}}
		}, bindsQ(styled("form", nil))), []finding{{"parameter/x/q", "example 0 is a string, and type/primitive/integer holds an integer"}}},
		{"an object under form", parameterOperation(withExample(&ir.Value{Kind: ir.ValueObject}), bindsQ(styled("form", nil))),
			[]finding{{"parameter/x/q", "example 0 is an object, and type/l holds a list"}}},
		{"bytes under form", parameterOperation(withExample(&ir.Value{Kind: ir.ValueBytes, Bytes: []byte("a,b")}), bindsQ(styled("form", nil))),
			[]finding{{"parameter/x/q", "example 0 is bytes, and type/l holds a list"}}},
		{"a symbol under form", parameterOperation(withExample(&ir.Value{Kind: ir.ValueSymbol, String: "a"}), bindsQ(styled("form", nil))),
			[]finding{{"parameter/x/q", "example 0 is a symbol, and type/l holds a list"}}},
		{"a scenario's string input under form", func() *ir.Document {
			doc := parameterOperation(func(*ir.Parameter) {}, bindsQ(styled("form", off)))
			doc.Services[0].Groups[0].Operations[0].Examples = []ir.Example{{
				Name: "s", Input: &ir.ScenarioInput{Params: map[ir.ParameterID]ir.Value{"parameter/x/q": *text("a,b")}},
			}}
			return doc
		}(), []finding{{"operation/x", `scenario "s" parameter "parameter/x/q" is a string, and type/l holds a list`}}},
		{"a string of an object under form", parameterOperation(ofType(objectT, text("R,100,G,200")), bindsQ(styled("form", off))), nil},
		{"a string of an object under exploded form", parameterOperation(ofType(objectT, text("R=100&G=200")), bindsQ(styled("form", nil))), nil},
		{"a string of an object under deepObject", parameterOperation(ofType(objectT, text("q[R]=100")), bindsQ(styled("deepObject", on))), nil},
		{"a string of an object under deepObject, not exploded", parameterOperation(ofType(objectT, text("q[R]=100")),
			bindsQ(styled("deepObject", off))), nil},
		{"a string of an object under pipeDelimited, not exploded", parameterOperation(ofType(objectT, text("R|100")),
			bindsQ(styled("pipeDelimited", nil))), nil},
		{"a string of an object under spaceDelimited, exploded", parameterOperation(ofType(objectT, text("R 100")),
			bindsQ(styled("spaceDelimited", on))), objectJudged},
		{"a string of an object under a content type", parameterOperation(ofType(objectT, text("R,100")), bindsQ(func(pb *ir.HTTPParamBinding) {
			pb.Style, pb.ContentType = "", "application/json"
		})), nil},
		{"a string of an object under no binding", parameterOperation(ofType(objectT, text("R,100"))), objectJudged},
		{"a string of a map under form", parameterOperation(ofType(mapT, text("R,100")), bindsQ(styled("form", nil))), nil},
		{"a number of an object under form", parameterOperation(ofType(objectT, number("5")), bindsQ(styled("form", nil))),
			[]finding{{"parameter/x/q", "example 0 is a number, and type/o holds an object"}}},
		{"a bool of a map under deepObject", parameterOperation(ofType(mapT, &ir.Value{Kind: ir.ValueBool, Bool: true}),
			bindsQ(styled("deepObject", on))), []finding{{"parameter/x/q", "example 0 is a bool, and type/m holds an object"}}},
		{"a number of an object under simple", parameterOperation(ofType(objectT, number("42")), bindsQ(styled("simple", nil))),
			[]finding{{"parameter/x/q", "example 0 is a number, and type/o holds an object"}}},
		{"a bool of a list of integers under form", itemOperation(intT, &ir.Value{Kind: ir.ValueBool, Bool: true}),
			[]finding{{"parameter/x/q", "example 0 is a bool, and type/i holds a list"}}},
		{"a number of a list of bools under form", itemOperation(boolT, number("7")),
			[]finding{{"parameter/x/q", "example 0 is a number, and type/i holds a list"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, found(t, tc.doc, exampleCode))
		})
	}
}

// itemOperation is parameterOperation under form whose parameter is of the
// list type/i, of element type element, with the one example value.
func itemOperation(element ir.TypeRef, value *ir.Value) *ir.Document {
	doc := parameterOperation(ofType(ir.TypeRef{Target: "type/i"}, value), bindsQ(styled("form", nil)))
	doc.Types["type/i"] = &ir.List{ID: "type/i", ElementType: element, Provenance: provAt("type/i")}
	return doc
}

// TestValidate_ANumberOrABoolIsOneItemOfAListsText holds a number or a bool
// example of a list a style writes to be its text only as the one item it
// is, since it holds no delimiter: accepted where the element type settles to
// text, to anything or to a shape admitting the value's kind, and judged where
// it settles to another shape or to a type the check reads no further, and for
// a tuple, whose items have no one type. A string is accepted whatever the
// element type, being text a style may split.
func TestValidate_ANumberOrABoolIsOneItemOfAListsText(t *testing.T) {
	t.Parallel()
	asNumber := []finding{{"parameter/x/q", "example 0 is a number, and type/i holds a list"}}
	on := &ir.Value{Kind: ir.ValueBool, Bool: true}
	decimalT := ir.TypeRef{Target: ir.PrimitiveTypeID(ir.PrimitiveDecimal)}
	enumT := ir.TypeRef{Target: "type/e"}
	tuple := itemOperation(intT, number("7"))
	tuple.Types["type/i"] = &ir.Tuple{ID: "type/i", ElementTypes: []ir.TypeRef{intT}, Provenance: provAt("type/i")}
	enum := itemOperation(enumT, number("8"))
	enum.Types["type/e"] = &ir.Enum{ID: "type/e", ValueType: ir.PrimitiveInteger, Provenance: provAt("type/e"), Members: []ir.EnumMember{
		{ID: "enum-member/e/7", Value: *number("7")},
	}}
	for _, tc := range []struct {
		name string
		doc  *ir.Document
		want []finding
	}{
		{"a number of a list of integers", itemOperation(intT, number("7")), nil},
		{"a number of a list of strings", itemOperation(stringT, number("7")), nil},
		{"a number of a list of anything", itemOperation(anyT, number("7")), nil},
		{"a bool of a list of bools", itemOperation(boolT, on), nil},
		{"a bool of a list of strings", itemOperation(stringT, on), nil},
		{"a bool of a list of numbers", itemOperation(decimalT, on), []finding{{"parameter/x/q", "example 0 is a bool, and type/i holds a list"}}},
		{"a number of a list of an enum it is no member of", enum, asNumber},
		{"a number of a tuple", tuple, asNumber},
		{"a string of a list of integers", itemOperation(intT, text("a,b")), nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, found(t, tc.doc, exampleCode))
		})
	}
}

// TestValidate_ASerializedDefaultOfAListOrAnObjectIsReported holds a
// parameter's and a header's default to its type where its examples may be
// written out: a default is a value an emitter writes into code, not a wire
// form.
func TestValidate_ASerializedDefaultOfAListOrAnObjectIsReported(t *testing.T) {
	t.Parallel()
	parameter := parameterOperation(func(parameter *ir.Parameter) { parameter.Default = text("a,b") }, bindsQ(styled("form", nil)))
	object := parameterOperation(func(parameter *ir.Parameter) { parameter.Type, parameter.Default = objectT, text("R,100") },
		bindsQ(styled("form", nil)))
	header := headerOperation(func(header *ir.Header) { header.Default = text("a,b") })

	assert.Equal(t, []finding{{"parameter/x/q", "default is a string, and type/l holds a list"}}, found(t, parameter, defaultCode))
	assert.Equal(t, []finding{{"parameter/x/q", "default is a string, and type/o holds an object"}}, found(t, object, defaultCode))
	assert.Equal(t, []finding{{"property/x/h", "default is a string, and type/l holds a list"}}, found(t, header, defaultCode))
}

// TestValidate_AParameterWrittenInAnEncodingHasNoValueJudged holds a parameter
// whose own Encoding writes it to have neither its examples nor its default
// judged, and one a media type alone serializes, through its own Encoding or a
// binding writing it whole under a content type, to have its examples alone
// unjudged, since its default is a value of the type. One a content type writes only a member of
// has both judged. A content type's binding ahead of one writing no list still
// serializes the parameter.
func TestValidate_AParameterWrittenInAnEncodingHasNoValueJudged(t *testing.T) {
	t.Parallel()
	content := func(pb *ir.HTTPParamBinding) { pb.Style, pb.ContentType = "", "application/json" }
	valued := func(parameter *ir.Parameter) {
		parameter.Examples, parameter.Default = []ir.Example{{Value: text("a,b")}}, text("a,b")
	}
	for _, tc := range []struct {
		name                         string
		doc                          *ir.Document
		exampleJudged, defaultJudged bool
	}{
		{"under a content type", parameterOperation(valued, bindsQ(content)), false, true},
		{"under a content type, then a style writing no list", parameterOperation(valued, bindsQ(content), bindsQ(styled("deepObject", nil))),
			false, true},
		{"under its own encoding", parameterOperation(func(parameter *ir.Parameter) {
			valued(parameter)
			parameter.Encoding = &ir.Encoding{Name: "string"}
		}), false, false},
		{"under its own encoding naming a media type alone", parameterOperation(func(parameter *ir.Parameter) {
			valued(parameter)
			parameter.Encoding = &ir.Encoding{MediaType: "application/json"}
		}), false, true},
		{"under its own encoding and a content type", parameterOperation(func(parameter *ir.Parameter) {
			valued(parameter)
			parameter.Encoding = &ir.Encoding{Name: "http-date"}
		}, bindsQ(content)), false, false},
		{"where a content type writes a member", parameterOperation(valued, bindsQ(func(pb *ir.HTTPParamBinding) {
			content(pb)
			pb.ParamPath = []ir.PropertyID{"property/m/a"}
		})), true, true},
		{"where a content type writes another parameter", parameterOperation(valued, bindsQ(func(pb *ir.HTTPParamBinding) {
			content(pb)
			pb.Param = "parameter/x/other"
		})), true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var wantExample, wantDefault []finding
			if tc.exampleJudged {
				wantExample = []finding{{"parameter/x/q", "example 0 is a string, and type/l holds a list"}}
			}
			if tc.defaultJudged {
				wantDefault = []finding{{"parameter/x/q", "default is a string, and type/l holds a list"}}
			}
			assert.Equal(t, wantExample, found(t, tc.doc, exampleCode))
			assert.Equal(t, wantDefault, found(t, tc.doc, defaultCode))
		})
	}
}

// TestValidate_AHeaderWrittenInAnEncodingHasNoValueJudged holds a header whose
// Encoding names a media type alone, as one declared by content has, to have
// its examples alone unjudged, as a parameter a content type writes has, and
// one whose Encoding names anything else, or nothing, to have neither judged.
// Its example is an object, which no style writes for a list.
func TestValidate_AHeaderWrittenInAnEncodingHasNoValueJudged(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name          string
		encoding      *ir.Encoding
		defaultJudged bool
	}{
		{"a media type alone", &ir.Encoding{MediaType: "application/json"}, true},
		{"a media type and a name", &ir.Encoding{MediaType: "application/json", Name: "base64"}, false},
		{"a name alone", &ir.Encoding{Name: "http-date"}, false},
		{"nothing", &ir.Encoding{}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc := headerOperation(func(header *ir.Header) {
				header.Encoding, header.Default = tc.encoding, text("a,b")
				header.Examples = []ir.Example{{Value: &ir.Value{Kind: ir.ValueObject}}}
			})
			var wantDefault []finding
			if tc.defaultJudged {
				wantDefault = []finding{{"property/x/h", "default is a string, and type/l holds a list"}}
			}
			assert.Empty(t, found(t, doc, exampleCode))
			assert.Equal(t, wantDefault, found(t, doc, defaultCode))
		})
	}
}

// headerOperation is structuredDoc holding one operation whose response
// declares the header property/x/h, of type listT, shaped by set.
func headerOperation(set func(*ir.Header)) *ir.Document {
	doc := structuredDoc()
	header := ir.Header{Property: property("property/x/h", listT, nil)}
	set(&header)
	doc.Services = []ir.Service{{ID: "service/x", Groups: []ir.OperationGroup{{ID: "operation-group/x", Operations: []ir.Operation{{
		ID: "operation/x", Responses: []ir.Response{{Headers: []ir.Header{header}}},
	}}}}}}
	return doc
}

// TestValidate_AHeadersSerializedExampleOfAListOrAnObjectIsAccepted holds a
// header's scalar example of a list or an object to be the value written out
// where its style can write that shape, simple where it states none, as a
// parameter's is, and a model property's, which has no style, to be judged.
func TestValidate_AHeadersSerializedExampleOfAListOrAnObjectIsAccepted(t *testing.T) {
	t.Parallel()
	judged := []finding{{"property/x/h", "example 0 is a string, and type/l holds a list"}}
	objectJudged := []finding{{"property/x/h", "example 0 is a string, and type/o holds an object"}}
	object := func(style string, explode *bool) func(*ir.Header) {
		return func(header *ir.Header) {
			header.Type, header.Examples, header.Style, header.Explode = objectT, []ir.Example{{Value: text("R,100")}}, style, explode
		}
	}
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
	objectModel := modelOf(property("property/x/h", objectT, exampled(text("R,100"))))
	objectModel.Types["type/o"] = &ir.Model{ID: "type/o", Provenance: provAt("type/o")}
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
		{"a response's header of an object stating no style", headerOperation(object("", nil)), nil},
		{"a response's header of an object under spaceDelimited, exploded", headerOperation(object("spaceDelimited", new(true))), objectJudged},
		{"a model's property of an object", objectModel, objectJudged},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, found(t, tc.doc, exampleCode))
		})
	}
}
