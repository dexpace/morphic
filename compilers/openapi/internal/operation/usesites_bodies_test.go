// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package operation_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/ir"
)

// byCode returns the findings of diags coded code, spelled as findings spells
// them.
func byCode(diags []ir.Diagnostic, code string) []string {
	var matching []ir.Diagnostic
	for _, diagnostic := range diags {
		if diagnostic.Code == code {
			matching = append(matching, diagnostic)
		}
	}
	return findings(matching)
}

// requestContent returns the first content of the request of the operation
// named source.
func requestContent(t *testing.T, doc *ir.Document, source string) any {
	t.Helper()
	c := valueAtPath(operationTree(t, doc, source), "request", "contents", "0")
	require.NotNil(t, c, "operation %q lowers a request body", source)
	return c
}

const (
	// octetStream is the part encoding a binary part without a declared
	// content type reads.
	octetStream = `"contentTypes":["application/octet-stream"]`
	// formDataSchema is the end of the pointer a form body's schema sits at.
	formDataSchema = "/requestBody/content/multipart~1form-data/schema"
)

// TestPartKeys_KeysACatchAll holds a form body's catch-all to the encoding the
// source states for it, keyed by the map or model holding the catch-all: a
// model's value in front of a declared part, a map with no declared part at
// all, and a union body keyed variant by variant, each model with its own
// patterns and value.
func TestPartKeys_KeysACatchAll(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, specFrom(t, conformanceSpecsDir, "catch-all-encoding"))
	assert.Empty(t, diags)

	put := requestContent(t, doc, "putSnippet")
	catchAll := func(c any) map[string]any {
		m, _ := valueAtPath(c, "catchAllEncoding").(map[string]any)
		return m
	}
	assert.JSONEq(t, `{"type/anonymous/paths/~1snippet/put`+formDataSchema+`":{"value":{`+octetStream+
		`,"repeatsPerArrayItem":true,"hasFilename":true,"allowReserved":false}}}`, toJSON(t, catchAll(put)))
	encoding, _ := valueAtPath(put, "encoding").(map[string]any)
	assert.Len(t, encoding, 1, "the declared part keeps its encoding beside the catch-all's")
	assert.Contains(t, encoding, "property/openapi/paths/~1snippet/put/requestBody/content/multipart~1form-data/schema/properties/metadata")

	assets := requestContent(t, doc, "uploadAssets")
	assert.JSONEq(t, `{"type/anonymous/paths/~1assets/post`+formDataSchema+`":{"value":{`+octetStream+
		`,"repeatsPerArrayItem":false,"hasFilename":true,"allowReserved":false}}}`, toJSON(t, catchAll(assets)),
		"a body that is a map declares no part, and its values are still parts")

	mixed := requestContent(t, doc, "mixed")
	assert.JSONEq(t, `{`+
		`"type/openapi/components/schemas/Files":{"patterns":{"^f_":{`+octetStream+`,"repeatsPerArrayItem":false,"hasFilename":true,"allowReserved":false}}},`+
		`"type/openapi/components/schemas/Notes":{"value":{"contentTypes":["text/plain"],"repeatsPerArrayItem":false,"hasFilename":false,"allowReserved":false}}}`,
		toJSON(t, catchAll(mixed)), "each variant of the union is keyed, not the union")
}

// TestPartless_FormBodyWithNoObject holds a form body whose schema stands for
// no model and no map to one warning at its schema: an array of strings. An
// untyped body, a file body and a body the compiler can name parts of draw none.
func TestPartless_FormBodyWithNoObject(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, specFrom(t, specsDir, "media_key_and_form_bodies"))
	assert.Equal(t, []string{
		"openapi/partless-form-body warning /paths/~1mp/post/requestBody/content/multipart~1form-data/schema",
	}, byCode(diags, "openapi/partless-form-body"))
	assert.NotNil(t, valueAtPath(operationTree(t, doc, "mp"), "request"), "the body still lowers as declared")

	_, diags = parseFull(t, specFrom(t, conformanceSpecsDir, "catch-all-encoding"))
	assert.Empty(t, byCode(diags, "openapi/partless-form-body"), "a map and a model with a catch-all are objects")
}

// TestRequestBody_OnAMethodWithoutSemantics holds a request body on GET, HEAD
// or DELETE to an info at the body, lowered as declared, and one on OPTIONS,
// PUT or POST to nothing.
func TestRequestBody_OnAMethodWithoutSemantics(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, specFrom(t, specsDir, "body_on_methods"))
	assert.Equal(t, []string{
		"openapi/body-without-semantics info /paths/~1a/delete/requestBody",
		"openapi/body-without-semantics info /paths/~1a/get/requestBody",
		"openapi/body-without-semantics info /paths/~1a/head/requestBody",
	}, byCode(diags, "openapi/body-without-semantics"))
	assert.Empty(t, byCode(diags, "openapi/body-forbidden-by-method"))
	for _, name := range []string{"g", "d", "h", "o", "u"} {
		assert.NotNil(t, valueAtPath(operationTree(t, doc, name), "request"), "operation %s keeps its body", name)
	}

	_, diags = parseFull(t, specFrom(t, specsDir, "media_key_and_form_bodies"))
	assert.Equal(t, []string{"openapi/body-without-semantics info /paths/~1a/get/requestBody"},
		byCode(diags, "openapi/body-without-semantics"), "the post beside it draws none")
}

// TestRequestBody_OnAMethodThatForbidsOne holds a request body on TRACE, and on
// a 3.2 CONNECT written as an additional operation, to a warning under its own
// code at the body, citing each method's section, and not to the info a GET
// draws.
func TestRequestBody_OnAMethodThatForbidsOne(t *testing.T) {
	t.Parallel()
	section := func(diags []ir.Diagnostic) []string {
		var out []string
		for _, diagnostic := range diags {
			if diagnostic.Code == "openapi/body-forbidden-by-method" {
				out = append(out, diagnostic.Message)
			}
		}
		return out
	}
	doc, diags := parseFull(t, specFrom(t, specsDir, "body_on_trace"))
	assert.Equal(t, []string{"openapi/body-forbidden-by-method warning /paths/~1a/trace/requestBody"},
		byCode(diags, "openapi/body-forbidden-by-method"))
	assert.Equal(t, []string{"TRACE request declares a body, which HTTP forbids (RFC 9110 §9.3.8); lowered as declared"},
		section(diags))
	assert.Empty(t, byCode(diags, "openapi/body-without-semantics"))
	assert.NotNil(t, valueAtPath(operationTree(t, doc, "a"), "request"))

	doc, diags = parseFull(t, specFrom(t, specsDir, "body_on_additional_operations"))
	assert.Equal(t, []string{"openapi/body-forbidden-by-method warning /paths/~1a/additionalOperations/CONNECT/requestBody"},
		byCode(diags, "openapi/body-forbidden-by-method"))
	assert.Equal(t, []string{"CONNECT request declares a body, which HTTP forbids (RFC 9110 §9.3.6); lowered as declared"},
		section(diags))
	assert.Empty(t, byCode(diags, "openapi/body-without-semantics"))
	assert.NotNil(t, valueAtPath(operationTree(t, doc, "c"), "request"))
}

// TestExamples_EmptyKeyIsKept holds an examples entry keyed by the empty string
// to an unnamed example that keeps the key, with an info at the entry; the same
// value written as example draws none and keeps nothing, so the two documents
// differ.
func TestExamples_EmptyKeyIsKept(t *testing.T) {
	t.Parallel()
	const entry = "/paths/~1a/get/responses/200/content/application~1json/examples/"
	named, diags := parseFull(t, specFrom(t, specsDir, "example_key_empty"))
	assert.Equal(t, []string{"openapi/degraded-construct info " + entry}, findings(diags))
	got := valueAtPath(tree(t, named.Services[0].Groups[0].Operations[0]), "responses", "0", "payload", "contents", "0", "examples")
	assert.JSONEq(t, `[{"value":{"kind":"number","number":"1"},"unmodeled":{"openapi:examples/key":{"reason":"degraded_lowering",`+
		`"value":"","provenance":{"source":0,"pointer":"`+entry+`"}}}}]`, toJSON(t, got))

	single, diags := parseFull(t, specFrom(t, specsDir, "example_key_single"))
	assert.Empty(t, diags)
	one := valueAtPath(tree(t, single.Services[0].Groups[0].Operations[0]), "responses", "0", "payload", "contents", "0", "examples")
	assert.JSONEq(t, `[{"value":{"kind":"number","number":"1"}}]`, toJSON(t, one))
	assert.NotEqual(t, withoutSources(t, named), withoutSources(t, single))
}

// TestParameter_ContentStatesNoDefaultStyle holds a parameter serialized
// through its content to the keywords it writes: the three written beside it
// stay lowered as declared and are reported, one writing no style gets none of
// its location's default, and one serialized through its schema keeps it.
func TestParameter_ContentStatesNoDefaultStyle(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, specFrom(t, specsDir, "param_content_serialization"))
	assert.Equal(t, []string{
		"openapi/inapplicable-location-keyword warning /paths/~1a/get/parameters/0/allowReserved",
		"openapi/inapplicable-location-keyword warning /paths/~1a/get/parameters/0/explode",
		"openapi/inapplicable-location-keyword warning /paths/~1a/get/parameters/0/style",
	}, findings(diags))

	a := valueAtPath(operationTree(t, doc, "a"), "bindings", "http", "0", "paramBindings")
	q := namedItem(t, a, "q")
	assert.Equal(t, "form", valueAtPath(q, "style"))
	assert.Equal(t, false, valueAtPath(q, "explode"))
	assert.Equal(t, true, valueAtPath(q, "allowReserved"))
	assert.Equal(t, "application/json", valueAtPath(q, "contentType"))

	c := namedItem(t, a, "c")
	assert.Equal(t, "application/json", valueAtPath(c, "contentType"))
	binding, _ := c.(map[string]any)
	assert.NotContains(t, binding, "style", "the media type serializes the value: no style is stated for it")

	assert.Equal(t, "form", valueAtPath(namedItem(t, a, "s"), "style"), "a schema parameter keeps its location's default")
}

// TestPartless_JudgesWhatTheBodyNames holds the warning to a body naming no
// part: an object const and an enum of objects name theirs, and draw none, as
// does an enum with one object among scalars; a scalar const draws one under
// either form media type; a body union read past its bound is not judged.
func TestPartless_JudgesWhatTheBodyNames(t *testing.T) {
	t.Parallel()
	body := func(path, mt, schema string) string {
		return "  /" + path + ":\n    post:\n      operationId: " + path + "\n      requestBody:\n        content:\n          " +
			mt + ":\n            schema: " + schema + "\n      responses: {\"204\": {description: ok}}\n"
	}
	unions := "components:\n  schemas:\n"
	for i := range 70 {
		unions += fmt.Sprintf("    U%d: {oneOf: [{type: string}, {$ref: '#/components/schemas/U%d'}]}\n", i, i+1)
	}
	unions += "    U70: {oneOf: [{type: string}, {type: integer}]}\n"
	_, diags := parseFull(t, openapitest.PathsSpecVer("3.1.0",
		body("a", "multipart/form-data", "{const: {a: 1, b: x}}")+
			body("b", "application/x-www-form-urlencoded", "{enum: [{a: 1}, {a: 2}]}")+
			body("c", "multipart/form-data", "{const: 5}")+
			body("d", "multipart/form-data", "{enum: [1, {a: 2}]}")+
			body("e", "application/x-www-form-urlencoded", "{const: 5}")+
			body("f", "multipart/form-data", "{$ref: '#/components/schemas/U0'}")+unions))
	assert.Equal(t, []string{
		"openapi/partless-form-body warning /paths/~1c/post/requestBody/content/multipart~1form-data/schema",
		"openapi/partless-form-body warning /paths/~1e/post/requestBody/content/application~1x-www-form-urlencoded/schema",
	}, byCode(diags, "openapi/partless-form-body"))
}
