// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package pass_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/pass"
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
// style that writes no such shape or a binding of a member, as null of a
// required parameter, bytes, a symbol or the other structured shape, for a
// type of neither shape, as an example the type gives and as a scenario's
// input.
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
		{"null of a required parameter under form", parameterOperation(func(parameter *ir.Parameter) {
			parameter.Required, parameter.Examples = true, []ir.Example{{Value: null()}}
		}, bindsQ(styled("form", nil))),
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
		{"a string of an object under label", parameterOperation(ofType(objectT, text(".R,100")), bindsQ(styled("label", nil))), nil},
		{"a string of an object under matrix", parameterOperation(ofType(objectT, text(";R=100")), bindsQ(styled("matrix", on))), nil},
		{"a string of an object under cookie", parameterOperation(ofType(objectT, text("R,100")), bindsQ(styled("cookie", off))), nil},
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

// boundedItems is itemOperation whose list declares the bounds list and whose
// element is the alias type/s of base, declaring the bounds element.
func boundedItems(base ir.TypeRef, element, list *ir.Constraints, value *ir.Value) *ir.Document {
	doc := itemOperation(ir.TypeRef{Target: "type/s"}, value)
	doc.Types["type/i"] = &ir.List{ID: "type/i", ElementType: ir.TypeRef{Target: "type/s"}, Constraints: list, Provenance: provAt("type/i")}
	doc.Types["type/s"] = &ir.Scalar{ID: "type/s", Base: &base, Constraints: element, Provenance: provAt("type/s")}
	return doc
}

// TestValidate_ANumberOrABoolIsOneItemOfAListsText holds a number or a bool
// example of a list a style writes to be its text only as its one item, since
// it holds no delimiter: judged against the list's item counts and the element
// type, whole numbers and bounds included, as text where the element is text,
// not bytes, and accepted where it holds anything. It is judged where the
// element is a type the check reads no further, and for a tuple, whose items
// have no one type. A string is accepted for a list of leaves, as text a style
// may split.
func TestValidate_ANumberOrABoolIsOneItemOfAListsText(t *testing.T) {
	t.Parallel()
	asNumber := []finding{{"parameter/x/q", "example 0 is a number, and type/i holds a list"}}
	asBool := []finding{{"parameter/x/q", "example 0 is a bool, and type/i holds a list"}}
	on := &ir.Value{Kind: ir.ValueBool, Bool: true}
	header := func(value *ir.Value) *ir.Document {
		doc := headerOperation(func(header *ir.Header) {
			header.Type, header.Examples = ir.TypeRef{Target: "type/i"}, []ir.Example{{Value: value}}
		})
		doc.Types["type/i"] = &ir.List{ID: "type/i", ElementType: intT, Provenance: provAt("type/i")}
		return doc
	}
	five, two := ir.BigVal("5"), int64(2)
	decimalT := ir.TypeRef{Target: ir.PrimitiveTypeID(ir.PrimitiveDecimal)}
	enumT := ir.TypeRef{Target: "type/e"}
	bytesItems := func(value *ir.Value) *ir.Document {
		bytesID := ir.PrimitiveTypeID(ir.PrimitiveBytes)
		return holding(itemOperation(ir.TypeRef{Target: bytesID}, value), &ir.Primitive{ID: bytesID, PrimitiveKind: ir.PrimitiveBytes})
	}
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
		{"a number of a list of bytes", bytesItems(number("5")), asNumber},
		{"a bool of a list of bytes", bytesItems(on), asBool},
		{"a bool of a list of bools", itemOperation(boolT, on), nil},
		{"a bool of a list of strings", itemOperation(stringT, on), nil},
		{"a bool of a list of numbers", itemOperation(decimalT, on), asBool},
		{"a fraction of a list of integers", itemOperation(intT, number("8.5")), asNumber},
		{"a fraction of a list of numbers", itemOperation(decimalT, number("8.5")), nil},
		{"a number within its item's maximum", boundedItems(intT, &ir.Constraints{Max: &five}, nil, number("5")), nil},
		{"a number over its item's maximum", boundedItems(intT, &ir.Constraints{Max: &five}, nil, number("8")), asNumber},
		{"a number under the list's minimum count", boundedItems(intT, nil, &ir.Constraints{MinItems: &two}, number("8")), asNumber},
		{"a number over the list's maximum count", boundedItems(intT, nil, &ir.Constraints{MaxItems: new(int64(0))}, number("8")), asNumber},
		{"a number within the list's counts", boundedItems(intT, nil, &ir.Constraints{MinItems: new(int64(1)), MaxItems: new(int64(1))}, number("8")), nil},
		{"a bool longer than its text item's maximum", boundedItems(stringT, &ir.Constraints{MaxLength: &two}, nil, on), asBool},
		{"a number within its text item's maximum", boundedItems(stringT, &ir.Constraints{MaxLength: &two}, nil, number("12")), nil},
		{"a number shorter than its text item's minimum", boundedItems(stringT, &ir.Constraints{MinLength: &two}, nil, number("1")), asNumber},
		{"a number of a list of anything under the list's minimum count", boundedItems(anyT, nil, &ir.Constraints{MinItems: &two}, number("8")), asNumber},
		{"a fraction of a header's list of integers", header(number("1.5")), []finding{{"property/x/h", "example 0 is a number, and type/i holds a list"}}},
		{"a number of a header's list of integers", header(number("1")), nil},
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

// TestValidate_AParameterWrittenInAnEncodingHasNoKindJudged holds a parameter
// a media type alone serializes, through its own Encoding or a binding writing
// it whole under a content type, to have the kind of its examples alone
// unjudged, since its default is a value of the type. One whose own Encoding
// names anything else has both judged until #2178 decides, as has one a
// content type writes only a member of. A content type's binding ahead of one
// writing no list still serializes the parameter.
func TestValidate_AParameterWrittenInAnEncodingHasNoKindJudged(t *testing.T) {
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
			parameter.Encoding = &ir.Encoding{Name: "http-date"}
		}), true, true},
		{"under its own encoding naming a media type alone", parameterOperation(func(parameter *ir.Parameter) {
			valued(parameter)
			parameter.Encoding = &ir.Encoding{MediaType: "application/json"}
		}), false, true},
		{"under its own encoding and a content type", parameterOperation(func(parameter *ir.Parameter) {
			valued(parameter)
			parameter.Encoding = &ir.Encoding{Name: "http-date"}
		}, bindsQ(content)), false, true},
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

// TestValidate_AHeaderWrittenInAnEncodingHasNoKindJudged holds a header whose
// Encoding names a media type alone, as one declared by content has, to have
// the kind of its examples alone unjudged, as a parameter a content type writes
// has, and one whose Encoding names anything else, or nothing, the kind of
// neither. Its example is an object, which no style writes for a list.
func TestValidate_AHeaderWrittenInAnEncodingHasNoKindJudged(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name          string
		encoding      *ir.Encoding
		defaultJudged bool
	}{
		{"a media type alone", &ir.Encoding{MediaType: "application/json"}, true},
		{"a media type and a name", &ir.Encoding{MediaType: "application/json", Name: "base64"}, false},
		{"a media type and a schema", &ir.Encoding{MediaType: "application/json", Schema: &stringT}, false},
		{"a media type and a wire type", &ir.Encoding{MediaType: "application/json", WireType: &stringT}, false},
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

// TestValidate_AnEncodingExcusesNoMemberAndNoNull holds a value written in an
// encoding, by a header's own Encoding, a media type alone or a content type
// writing the parameter whole, to be judged for an enum's members, a literal's
// value and null all the same, its default and its examples alike: an encoding
// excuses only the kind, whole numbers and bounds, until #2178 decides. Each
// position is required, since a null example of one left out stands for its
// absence.
func TestValidate_AnEncodingExcusesNoMemberAndNoNull(t *testing.T) {
	t.Parallel()
	content := bindsQ(func(pb *ir.HTTPParamBinding) { pb.Style, pb.ContentType = "", "application/json" })
	type written func(typ ir.TypeRef, value *ir.Value) *ir.Document
	parameter := func(encoding *ir.Encoding, bindings ...ir.HTTPBinding) written {
		return func(typ ir.TypeRef, value *ir.Value) *ir.Document {
			return parameterOperation(func(parameter *ir.Parameter) {
				parameter.Type, parameter.Encoding, parameter.Default = typ, encoding, value
				parameter.Required, parameter.Examples = true, []ir.Example{{Value: value}}
			}, bindings...)
		}
	}
	header := func(encoding *ir.Encoding) written {
		return func(typ ir.TypeRef, value *ir.Value) *ir.Document {
			return headerOperation(func(header *ir.Header) {
				header.Type, header.Encoding, header.Default = typ, encoding, value
				header.Required, header.Examples = true, []ir.Example{{Value: value}}
			})
		}
	}
	for _, position := range []struct {
		name string
		node string
		doc  written
	}{
		{"a parameter under a media type alone", "parameter/x/q", parameter(&ir.Encoding{MediaType: "application/json"})},
		{"a parameter under a content type", "parameter/x/q", parameter(nil, content)},
		{"a header under its own encoding", "property/x/h", header(&ir.Encoding{Name: "http-date"})},
		{"a header under a media type alone", "property/x/h", header(&ir.Encoding{MediaType: "text/plain"})},
	} {
		for _, value := range []struct {
			name  string
			typ   ir.TypeRef
			value *ir.Value
			why   string
		}{
			{"no member of an enum", ir.TypeRef{Target: "type/e"}, text("blue"), "is not a member of enum type/e"},
			{"another value than a literal's", ir.TypeRef{Target: "type/four"}, number("5"), "is not the value of literal type/four"},
			{"null where none is admitted", listT, null(), "is null, and type/l is not nullable here"},
		} {
			t.Run(position.name+", "+value.name, func(t *testing.T) {
				t.Parallel()
				doc := position.doc(value.typ, value.value)
				doc.Types["type/e"] = &ir.Enum{ID: "type/e", ValueType: ir.PrimitiveString, Members: []ir.EnumMember{
					{ID: "enum-member/red", Value: *text("red")},
				}, Provenance: provAt("type/e")}
				doc.Types["type/four"] = &ir.Literal{ID: "type/four", Value: *number("4"), Provenance: provAt("type/four")}
				assert.Equal(t, []finding{{position.node, "example 0 " + value.why}}, found(t, doc, exampleCode))
				assert.Equal(t, []finding{{position.node, "default " + value.why}}, found(t, doc, defaultCode))
			})
		}
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

// holding is doc holding each of types under its own ID as well.
func holding(doc *ir.Document, types ...ir.TypeDef) *ir.Document {
	for _, td := range types {
		doc.Types[td.Common().ID] = td
	}
	return doc
}

// unionOf is the union id whose variants are of the types variants.
func unionOf(id ir.TypeID, variants ...ir.TypeRef) *ir.Union {
	union := &ir.Union{ID: id, Provenance: provAt(string(id))}
	for i := range variants {
		union.Variants = append(union.Variants, ir.Variant{Type: variants[i]})
	}
	return union
}

// repeated is n copies of typ.
func repeated(typ ir.TypeRef, n int) []ir.TypeRef {
	out := make([]ir.TypeRef, n)
	for i := range out {
		out[i] = typ
	}
	return out
}

// TestValidate_AStringIsTheTextOfLeavesAlone holds a string example to be a
// list's or an object's text only where its items or members are leaves, as
// a style arranges nothing deeper (docs/emitters/refine/text.md): a list or a
// tuple of leaves, a map of leaves, and a model whose flattened members and
// catch-alls are leaves, a union of leaves being one, however deep, up to the
// types leaf reads. A list or an object holding a list or an object has
// every such string judged.
func TestValidate_AStringIsTheTextOfLeavesAlone(t *testing.T) {
	t.Parallel()
	listJudged := []finding{{"parameter/x/q", "example 0 is a string, and type/i holds a list"}}
	objectJudged := []finding{{"parameter/x/q", "example 0 is a string, and type/n holds an object"}}
	unionT, innerT, wideT := ir.TypeRef{Target: "type/u"}, ir.TypeRef{Target: "type/v"}, ir.TypeRef{Target: "type/w"}
	itemsOf := func(element ir.TypeRef, types ...ir.TypeDef) *ir.Document {
		return holding(itemOperation(element, text("a,b")), types...)
	}
	tupleOf := func(rest *ir.TypeRef, elements ...ir.TypeRef) *ir.Document {
		return itemsOf(stringT, &ir.Tuple{ID: "type/i", ElementTypes: elements, Rest: rest, Provenance: provAt("type/i")})
	}
	modelOf := func(set func(*ir.Model), types ...ir.TypeDef) *ir.Document {
		model := &ir.Model{ID: "type/n", Provenance: provAt("type/n")}
		set(model)
		doc := parameterOperation(ofType(ir.TypeRef{Target: "type/n"}, text("a,1")), bindsQ(styled("form", new(false))))
		return holding(doc, append(types, model)...)
	}
	member := func(typ ir.TypeRef) func(*ir.Model) {
		return func(model *ir.Model) { model.Properties = []ir.Property{property("property/n/a", typ, nil)} }
	}
	catchAll := func(additional ir.AdditionalProperties) func(*ir.Model) {
		return func(model *ir.Model) { model.AdditionalProperties = &additional }
	}
	base := &ir.Model{ID: "type/b", Properties: []ir.Property{property("property/b/a", objectT, nil)}, Provenance: provAt("type/b")}
	enum := &ir.Enum{ID: "type/e", ValueType: ir.PrimitiveString, Members: []ir.EnumMember{{ID: "enum-member/e/a", Value: *text("a")}},
		Provenance: provAt("type/e")}
	external := &ir.External{ID: "type/x", Identity: "x", Provenance: provAt("type/x")}
	for _, tc := range []struct {
		name string
		doc  *ir.Document
		want []finding
	}{
		{"a list of models", itemsOf(objectT), listJudged},
		{"a list of lists", itemsOf(listT), listJudged},
		{"a list of maps", itemsOf(mapT), listJudged},
		{"a list of an enum", itemsOf(ir.TypeRef{Target: "type/e"}, enum), nil},
		{"a list of anything", itemsOf(anyT), nil},
		{"a list of an external type", itemsOf(ir.TypeRef{Target: "type/x"}, external), nil},
		{"a list of a union of leaves", itemsOf(unionT, unionOf("type/u", stringT, intT)), nil},
		{"a list of a union holding a list", itemsOf(unionT, unionOf("type/u", stringT, listT)), listJudged},
		{"a list of a union of a union of leaves", itemsOf(unionT, unionOf("type/u", innerT), unionOf("type/v", stringT)), nil},
		{"a list of a union of a union holding a model", itemsOf(unionT, unionOf("type/u", stringT, innerT), unionOf("type/v", objectT)),
			listJudged},
		{"a list of a union that is its own variant", itemsOf(unionT, unionOf("type/u", unionT)), listJudged},
		{"a list of a union of as many leaves as are read", itemsOf(unionT, unionOf("type/u", repeated(stringT, pass.MaxLeafReads-1)...)), nil},
		{"a list of a union of more leaves than are read", itemsOf(unionT, unionOf("type/u", repeated(stringT, pass.MaxLeafReads)...)),
			listJudged},
		{"a list of a union of two unions of more leaves than are read", itemsOf(unionT, unionOf("type/u", innerT, wideT),
			unionOf("type/v", repeated(stringT, pass.MaxLeafReads/2-1)...), unionOf("type/w", repeated(stringT, pass.MaxLeafReads/2-1)...)),
			listJudged},
		{"a tuple of leaves", tupleOf(nil, stringT, intT), nil},
		{"a tuple holding a model", tupleOf(nil, stringT, objectT), listJudged},
		{"a tuple of leaves and a rest of leaves", tupleOf(&intT, stringT), nil},
		{"a tuple of leaves and a rest of lists", tupleOf(&listT, stringT), listJudged},
		{"a map of lists", holding(parameterOperation(ofType(ir.TypeRef{Target: "type/n"}, text("a,b")), bindsQ(styled("form", nil))),
			&ir.MapT{ID: "type/n", Value: listT, Provenance: provAt("type/n")}), objectJudged},
		{"a model of leaves", modelOf(member(intT)), nil},
		{"a model holding a list", modelOf(member(listT)), objectJudged},
		{"a model holding a model through its base", modelOf(func(model *ir.Model) {
			model.Base = &ir.TypeRef{Target: "type/b"}
		}, base), objectJudged},
		{"a model composing a union", modelOf(func(model *ir.Model) {
			model.Mixins = []ir.TypeRef{unionT}
		}, unionOf("type/u", stringT)), objectJudged},
		{"a model whose catch-all holds leaves", modelOf(catchAll(ir.AdditionalProperties{Value: &stringT})), nil},
		{"a model whose catch-all holds lists", modelOf(catchAll(ir.AdditionalProperties{Value: &listT})), objectJudged},
		{"a model whose pattern holds leaves", modelOf(catchAll(ir.AdditionalProperties{Patterns: []ir.PatternProperties{
			{Pattern: "^a", Value: intT},
		}})), nil},
		{"a model whose pattern holds models", modelOf(catchAll(ir.AdditionalProperties{Value: &stringT, Patterns: []ir.PatternProperties{
			{Pattern: "^a", Value: intT}, {Pattern: "^b", Value: objectT},
		}})), objectJudged},
		{"a model whose base's catch-all holds maps", modelOf(func(model *ir.Model) {
			model.Base = &ir.TypeRef{Target: "type/b"}
		}, &ir.Model{ID: "type/b", AdditionalProperties: &ir.AdditionalProperties{Value: &mapT}, Provenance: provAt("type/b")}), objectJudged},
		{"a model holding a model under deepObject", holding(parameterOperation(ofType(ir.TypeRef{Target: "type/n"}, text("q[a]=1")),
			bindsQ(styled("deepObject", nil))), &ir.Model{
			ID: "type/n", Properties: []ir.Property{property("property/n/a", objectT, nil)}, Provenance: provAt("type/n"),
		}), objectJudged},
		{"a number of a list of lists", itemOperation(listT, number("1")), []finding{
			{"parameter/x/q", "example 0 is a number, and type/i holds a list"},
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, found(t, tc.doc, exampleCode))
		})
	}
}
