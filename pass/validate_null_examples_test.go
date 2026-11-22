// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package pass_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/ir"
)

// nullDoc returns doc, one structuredDoc built, holding also the enum type/e
// of "open" and "closed" and the literal type/four, the types a null example
// is judged against by membership rather than by kind.
func nullDoc(doc *ir.Document) *ir.Document {
	doc.Types["type/e"] = &ir.Enum{ID: "type/e", ValueType: ir.PrimitiveString, Provenance: provAt("type/e"), Members: []ir.EnumMember{
		{ID: "enum-member/open", Value: *text("open")}, {ID: "enum-member/closed", Value: *text("closed")},
	}}
	doc.Types["type/four"] = &ir.Literal{ID: "type/four", Value: *number("4"), Provenance: provAt("type/four")}
	return doc
}

// nullParameter is nullDoc holding one operation whose parameter parameter/x/q
// is of type typ, required as required says, with a null example named
// "Optional" and the example set, a value of typ, named "Set".
func nullParameter(typ ir.TypeRef, required bool, set *ir.Value) *ir.Document {
	return nullDoc(parameterOperation(func(parameter *ir.Parameter) {
		parameter.Type, parameter.Required = typ, required
		parameter.Examples = []ir.Example{{Name: "Optional", Value: null()}, {Name: "Set", Value: set}}
	}))
}

// nullHeader is headerOperation's document with its header of type stringT,
// required as required says, written in encoding, and with a null example.
func nullHeader(required bool, encoding *ir.Encoding) *ir.Document {
	return headerOperation(func(header *ir.Header) {
		header.Type, header.Required, header.Encoding, header.Examples = stringT, required, encoding, []ir.Example{{Value: null()}}
	})
}

// movedHeader is nullHeader's document with its header moved off the response
// by place, which gives the operation the headers somewhere else to be read.
func movedHeader(required bool, place func(*ir.Operation, []ir.Header)) *ir.Document {
	doc := nullHeader(required, nil)
	operation := &doc.Services[0].Groups[0].Operations[0]
	headers := operation.Responses[0].Headers
	operation.Responses = nil
	place(operation, headers)
	return doc
}

// nullExample gives a parameter of listT a null example, the requirement
// required and the encoding encoding.
func nullExample(required bool, encoding *ir.Encoding) func(*ir.Parameter) {
	return func(parameter *ir.Parameter) {
		parameter.Required, parameter.Encoding, parameter.Examples = required, encoding, []ir.Example{{Value: null()}}
	}
}

// TestValidate_ANullExampleOfAParameterLeftOutIsNotReported holds a null
// example of a parameter or a header that is not required to be the value left
// out, not judged in any wording (of a kind, an enum or a literal) under any
// binding, a content type, an encoding or a media type of its own or RPC among
// them. A required one stays judged, neither excusing a null, as do a null
// default, a property's null example, which an object can hold as a member
// whose value is null, and a scenario's null input, which leaves a parameter
// out by not naming it.
func TestValidate_ANullExampleOfAParameterLeftOutIsNotReported(t *testing.T) {
	t.Parallel()
	enumT, literalT := ir.TypeRef{Target: "type/e"}, ir.TypeRef{Target: "type/four"}
	commonError := func(required bool) *ir.Document {
		doc := nullHeader(required, nil)
		doc.Services[0].CommonErrors = []ir.ErrorCase{{Headers: doc.Services[0].Groups[0].Operations[0].Responses[0].Headers}}
		doc.Services[0].Groups[0].Operations[0].Responses = nil
		return doc
	}
	scenario := nullDoc(parameterOperation(func(parameter *ir.Parameter) { parameter.Type = stringT }))
	scenario.Services[0].Groups[0].Operations[0].Examples = []ir.Example{{
		Name: "s", Input: &ir.ScenarioInput{Params: map[ir.ParameterID]ir.Value{"parameter/x/q": *null()}},
	}}
	content := bindsQ(func(pb *ir.HTTPParamBinding) { pb.Style, pb.ContentType = "", "application/json" })
	encoding, mediaType := &ir.Encoding{Name: "string"}, &ir.Encoding{MediaType: "application/json"}
	rpc := parameterOperation(nullExample(false, nil))
	rpc.Services[0].Groups[0].Operations[0].Bindings.RPC = &ir.RPCBinding{System: "jsonrpc", FullMethod: "x"}
	onDefault := func(operation *ir.Operation, headers []ir.Header) {
		operation.Default = &ir.Response{Headers: headers}
	}
	onError := func(operation *ir.Operation, headers []ir.Header) {
		operation.Errors = []ir.ErrorCase{{Headers: headers}}
	}
	onPart := func(operation *ir.Operation, headers []ir.Header) {
		operation.Request = &ir.Payload{Contents: []ir.Content{{Encoding: map[ir.PropertyID]ir.PartEncoding{"property/p": {Headers: headers}}}}}
	}
	onItem := func(operation *ir.Operation, headers []ir.Header) {
		operation.Request = &ir.Payload{Contents: []ir.Content{{ItemEncoding: &ir.PartEncoding{Headers: headers}}}}
	}
	nullOfList := []finding{{"parameter/x/q", "example 0 is null, and type/l is not nullable here"}}
	nullOfHeader := []finding{{"property/x/h", "example 0 is null, and type/primitive/string is not nullable here"}}
	for _, tc := range []struct {
		name string
		doc  *ir.Document
		want []finding
	}{
		{"a parameter not required, of a string", nullParameter(stringT, false, text("abc")), nil},
		{"a parameter not required, of an enum", nullParameter(enumT, false, text("open")), nil},
		{"a parameter not required, of a literal", nullParameter(literalT, false, number("4")), nil},
		{"a parameter not required, of a list under form", nullDoc(parameterOperation(withExample(null()), bindsQ(styled("form", nil)))), nil},
		{"a parameter not required, under a content type", parameterOperation(nullExample(false, nil), content), nil},
		{"a parameter not required, in an encoding of its own", parameterOperation(nullExample(false, encoding)), nil},
		{"a parameter not required, in a media type of its own", parameterOperation(nullExample(false, mediaType)), nil},
		{"a parameter not required, under an RPC binding", rpc, nil},
		{"a required parameter, under a content type", parameterOperation(nullExample(true, nil), content), nullOfList},
		{"a required parameter, in an encoding of its own", parameterOperation(nullExample(true, encoding)), nullOfList},
		{"a required parameter, in a media type of its own", parameterOperation(nullExample(true, mediaType)), nullOfList},
		{"a required parameter, of a string", nullParameter(stringT, true, text("abc")),
			[]finding{{"parameter/x/q", `example "Optional" is null, and type/primitive/string is not nullable here`}}},
		{"a required parameter, of an enum", nullParameter(enumT, true, text("open")),
			[]finding{{"parameter/x/q", `example "Optional" is not a member of enum type/e`}}},
		{"a required parameter, of a literal", nullParameter(literalT, true, number("4")),
			[]finding{{"parameter/x/q", `example "Optional" is not the value of literal type/four`}}},
		{"a response's header not required", nullHeader(false, nil), nil},
		{"a response's header not required, declared by content", nullHeader(false, mediaType), nil},
		{"a response's header not required, in an encoding of its own", nullHeader(false, encoding), nil},
		{"a common error's header not required", commonError(false), nil},
		{"a default response's header not required", movedHeader(false, onDefault), nil},
		{"an error's header not required", movedHeader(false, onError), nil},
		{"a part's header not required", movedHeader(false, onPart), nil},
		{"an item encoding's header not required", movedHeader(false, onItem), nil},
		{"a required response's header", nullHeader(true, nil), nullOfHeader},
		{"a required response's header, declared by content", nullHeader(true, mediaType), nullOfHeader},
		{"a required response's header, in an encoding of its own", nullHeader(true, encoding), nullOfHeader},
		{"a required common error's header", commonError(true), nullOfHeader},
		{"a required default response's header", movedHeader(true, onDefault), nullOfHeader},
		{"a required error's header", movedHeader(true, onError), nullOfHeader},
		{"a required part's header", movedHeader(true, onPart), nullOfHeader},
		{"a required item encoding's header", movedHeader(true, onItem), nullOfHeader},
		{"a model's property not required", modelOf(property("property/a/e", stringT, exampled(null()))),
			[]finding{{"property/a/e", "example 0 is null, and type/primitive/string is not nullable here"}}},
		{"a scenario's input of a parameter not required", scenario,
			[]finding{{"operation/x", `scenario "s" parameter "parameter/x/q" is null, and type/primitive/string is not nullable here`}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, found(t, tc.doc, exampleCode))
		})
	}
}

// TestValidate_ANullDefaultOfAParameterLeftOutIsReported holds a null default
// of a parameter or a header that is not required to its type: a default is a
// value an emitter writes into code, so null is no reading of it as left out.
func TestValidate_ANullDefaultOfAParameterLeftOutIsReported(t *testing.T) {
	t.Parallel()
	parameter := parameterOperation(func(parameter *ir.Parameter) { parameter.Type, parameter.Default = stringT, null() })
	header := headerOperation(func(header *ir.Header) { header.Type, header.Default = stringT, null() })

	assert.Equal(t, []finding{{"parameter/x/q", "default is null, and type/primitive/string is not nullable here"}}, found(t, parameter, defaultCode))
	assert.Equal(t, []finding{{"property/x/h", "default is null, and type/primitive/string is not nullable here"}}, found(t, header, defaultCode))
	assert.Empty(t, found(t, parameter, exampleCode), "a default is no example")
}
