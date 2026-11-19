// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package specrules_test

import (
	"encoding/json/jsontext"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
	"github.com/dexpace/morphic/compilers/openapi/internal/specrules"
	"github.com/dexpace/morphic/ir"
)

// TestCheck_RequiredFieldsFollowTheMinor pins which minors require a field.
func TestCheck_RequiredFieldsFollowTheMinor(t *testing.T) {
	verify(t, []scenario{
		{"info lacks title and version", "3.1.0", specrules.Info, `{}`, []finding{findingAtPointer(required, ""), findingAtPointer(required, "")}},
		{"info complete", "3.1.0", specrules.Info, `{title: t, version: "1"}`, nil},
		{"info lacks only version", "3.0.3", specrules.Info, `{title: t}`, []finding{findingAtPointer(required, "")}},
		{"response description required in 3.0", "3.0.3", specrules.Response, `{}`, []finding{findingAtPointer(required, "")}},
		{"response description required in 3.1", "3.1.0", specrules.Response, `{}`, []finding{findingAtPointer(required, "")}},
		{"response description optional in 3.2", "3.2.0", specrules.Response, `{}`, nil},
		{"operation responses required in 3.0", "3.0.3", specrules.Operation, `{}`, []finding{findingAtPointer(required, "")}},
		{"operation responses optional in 3.1", "3.1.0", specrules.Operation, `{}`, nil},
		{"request body content required", "3.2.0", specrules.RequestBody, `{}`, []finding{findingAtPointer(required, "")}},
		{"server url required", "3.0.3", specrules.Server, `{}`, []finding{findingAtPointer(required, "")}},
		{"a field that a later minor added is not required earlier", "3.0.3", specrules.OAuthFlow,
			`{scopes: {}, deviceAuthorizationUrl: "u"}`, []finding{findingAtPointer(later, "/deviceAuthorizationUrl")}},
		{"the security scheme's type is the auth package's to report", "3.1.0", specrules.SecurityScheme, `{}`, nil},
	})
}

// TestCheck_ShapesFollowTheField pins what each shape admits, and that a
// number or a boolean where a string belongs is a warning: the text it writes
// says what it means.
func TestCheck_ShapesFollowTheField(t *testing.T) {
	verify(t, []scenario{
		{"string field given a list", "3.1.0", specrules.Info, `{title: [a], version: v}`, []finding{findingAtPointer(mismatch, "/title")}},
		{"string field given null", "3.1.0", specrules.Info, `{title: ~, version: v}`, []finding{findingAtPointer(mismatch, "/title")}},
		{"string field given a number", "3.1.0", specrules.Info, `{title: 12, version: 1.0}`,
			[]finding{findingAtPointer(mismatch, "/title"), findingAtPointer(mismatch, "/version")}},
		{"string field given a date", "3.1.0", specrules.Info, `{title: t, version: 2024-01-01}`, nil},
		{"string list given a boolean", "3.1.0", specrules.Schema, `{required: [a, true]}`, []finding{findingAtPointer(mismatch, "/required/1")}},
		{"string map given a number", "3.1.0", specrules.OAuthFlow, `{tokenUrl: 'https://t', scopes: {a: 1, b: x}}`,
			[]finding{findingAtPointer(mismatch, "/scopes/a")}},
		{"object field given a scalar", "3.1.0", specrules.Info, `{title: t, version: v, contact: x}`, []finding{findingAtPointer(mismatch, "/contact")}},
		{"the object itself is a list", "3.1.0", specrules.Info, `[a]`, []finding{findingAtPointer(mismatch, "")}},
		{"boolean field given a string", "3.1.0", specrules.Operation, `{deprecated: "true"}`, []finding{findingAtPointer(mismatch, "/deprecated")}},
		{"boolean field given a boolean", "3.1.0", specrules.Operation, `{deprecated: true}`, nil},
		{"list field given a mapping", "3.1.0", specrules.Operation, `{parameters: {}}`, []finding{findingAtPointer(mismatch, "/parameters")}},
		{"map field given a list", "3.1.0", specrules.Components, `{schemas: []}`, []finding{findingAtPointer(mismatch, "/schemas")}},
		{"string list given an object item", "3.1.0", specrules.Operation, `{tags: [a, {b: c}]}`, []finding{findingAtPointer(mismatch, "/tags")}},
		{"string list given strings", "3.1.0", specrules.Operation, `{tags: [a, b]}`, nil},
		{"string map given a list entry", "3.1.0", specrules.OAuthFlow, `{scopes: {a: [b]}}`, []finding{findingAtPointer(mismatch, "/scopes")}},
		{"string map given strings", "3.1.0", specrules.OAuthFlow, `{scopes: {a: b}}`, nil},
		{"raw list given a scalar", "3.1.0", specrules.Schema, `{enum: x}`, []finding{findingAtPointer(mismatch, "/enum")}},
		{"raw map given a list", "3.1.0", specrules.Schema, `{$vocabulary: []}`, []finding{findingAtPointer(mismatch, "/$vocabulary")}},
		{"any field takes anything", "3.1.0", specrules.Schema, `{default: [1, {a: b}], const: ~, example: x}`, nil},
	})
	for doc, want := range map[string]string{
		`{title: 12, version: v}`:   `Info Object field "title" must be a string in OpenAPI 3.1 (Info Object); it is the number 12`,
		`{title: t, version: true}`: `Info Object field "version" must be a string in OpenAPI 3.1 (Info Object); it is the boolean true`,
	} {
		diags := run(t, "3.1.0", specrules.Info, doc)
		require.Len(t, diags, 1, doc)
		assert.Equal(t, ir.SeverityWarning, diags[0].Severity, "a string written unquoted means what it says")
		assert.Equal(t, want, diags[0].Message)
	}
	diags := run(t, "3.1.0", specrules.Schema, `{required: [a, 1]}`)
	require.Len(t, diags, 1)
	assert.Contains(t, diags[0].Message, `Schema Object field "required" item 1 must be a string`)
}

// TestCheck_NumbersFollowTheirShape pins the numeric shapes, and that the numeric
// bounds the schema lowering reads off the raw node are left to it: it reports a
// value that is no decimal a BigVal holds or is in the other dialect's form, and
// takes a quoted number as the decimal it spells. A number outside a bound's
// range, a multipleOf no greater than 0, quoted or not, is the table's, and so
// is a null, which the lowering reads as the bound left out.
func TestCheck_NumbersFollowTheirShape(t *testing.T) {
	verify(t, []scenario{
		{"natural: zero", "3.1.0", specrules.Schema, `{maxLength: 0}`, nil},
		{"natural: negative", "3.1.0", specrules.Schema, `{maxLength: -1}`, []finding{findingAtPointer(mismatch, "/maxLength")}},
		{"natural: whole float", "3.1.0", specrules.Schema, `{maxLength: 2.0}`, nil},
		{"natural: fraction", "3.1.0", specrules.Schema, `{maxLength: 1.5}`, []finding{findingAtPointer(mismatch, "/maxLength")}},
		{"natural: hex", "3.1.0", specrules.Schema, `{maxLength: 0x10}`, nil},
		{"natural: underscored is text", "3.1.0", specrules.Schema, `{maxLength: 1_000}`, []finding{findingAtPointer(mismatch, "/maxLength")}},
		{"natural: an underscored whole float is text", "3.1.0", specrules.Schema, `{maxLength: 1_0.0}`,
			[]finding{findingAtPointer(mismatch, "/maxLength")}},
		{"natural: a fraction is text", "3.1.0", specrules.Schema, `{maxLength: 4/2}`, []finding{findingAtPointer(mismatch, "/maxLength")}},
		{"natural: a binary exponent is text", "3.1.0", specrules.Schema, `{maxLength: 0x1p4}`,
			[]finding{findingAtPointer(mismatch, "/maxLength")}},
		{"natural: quoted", "3.1.0", specrules.Schema, `{maxLength: "5"}`, []finding{findingAtPointer(mismatch, "/maxLength")}},
		{"natural: infinity", "3.1.0", specrules.Schema, `{maxLength: .inf}`, []finding{findingAtPointer(mismatch, "/maxLength")}},
		{"natural: past a rational's range", "3.1.0", specrules.Schema, `{maxLength: 1e1000001}`, nil},
		{"natural: whole with a point, past it", "3.1.0", specrules.Schema, `{maxLength: 1.5e1000001}`, nil},
		{"natural: negative past it", "3.1.0", specrules.Schema, `{maxLength: -1e1000001}`,
			[]finding{findingAtPointer(mismatch, "/maxLength")}},
		{"natural: a fraction past it", "3.1.0", specrules.Schema, `{maxLength: 1e-1000001}`,
			[]finding{findingAtPointer(mismatch, "/maxLength")}},
		{"natural: past the range a BigVal holds", "3.1.0", specrules.Schema, `{maxLength: 1e1000000000000000000}`, nil},
		{"natural: negative past that range", "3.1.0", specrules.Schema, `{maxLength: -1e1000000000000000000}`,
			[]finding{findingAtPointer(mismatch, "/maxLength")}},
		{"natural: a fraction past that range", "3.1.0", specrules.Schema, `{maxLength: 1e-1000000000000000000}`,
			[]finding{findingAtPointer(mismatch, "/maxLength")}},
		{"bound: exponent", "3.1.0", specrules.Schema, `{maximum: 1e3, minimum: -2.5, multipleOf: 0.5}`, nil},
		{"bound: past float64's range", "3.1.0", specrules.Schema, `{maximum: 1e999, minLength: 1e30}`, nil},
		{"bound: no decimal is judged as read", "3.1.0", specrules.Schema,
			`{maximum: high, minimum: true, multipleOf: [1], exclusiveMinimum: .inf, exclusiveMaximum: "5"}`,
			[]finding{findingAtPointer(literal, "/maximum"), findingAtPointer(literal, "/minimum"), findingAtPointer(literal, "/multipleOf"),
				findingAtPointer(literal, "/exclusiveMinimum")}},
		{"bound: the other dialect's form", "3.0.3", specrules.Schema,
			`{exclusiveMinimum: 5, exclusiveMaximum: 6}`, []finding{findingAtPointer(form, "/exclusiveMinimum"), findingAtPointer(form, "/exclusiveMaximum")}},
		{"bound: a multipleOf of zero is the table's", "3.1.0", specrules.Schema, `{multipleOf: 0}`,
			[]finding{findingAtPointer(mismatch, "/multipleOf")}},
		{"bound: a negative multipleOf in 3.0", "3.0.3", specrules.Schema, `{multipleOf: -2}`, []finding{findingAtPointer(mismatch, "/multipleOf")}},
		{"bound: a quoted multipleOf of zero", "3.1.0", specrules.Schema, `{multipleOf: "0"}`, []finding{findingAtPointer(mismatch, "/multipleOf")}},
		{"bound: a quoted multipleOf above zero", "3.1.0", specrules.Schema, `{multipleOf: "0.5"}`, nil},
		{"bound: a null is the table's", "3.1.0", specrules.Schema, `{maximum: ~}`, []finding{findingAtPointer(mismatch, "/maximum")}},
		{"bound: a negative multipleOf past a rational's range", "3.1.0", specrules.Schema, `{multipleOf: -1e1000001}`,
			[]finding{findingAtPointer(mismatch, "/multipleOf")}},
		{"bound: a negative multipleOf below one", "3.1.0", specrules.Schema, `{multipleOf: -1e-1000001}`,
			[]finding{findingAtPointer(mismatch, "/multipleOf")}},
		{"bound: a multipleOf above zero below one", "3.1.0", specrules.Schema, `{multipleOf: 1e-1000001}`, nil},
		{"bound: past the range a BigVal holds", "3.1.0", specrules.Schema,
			`{multipleOf: 1e1000000000000000000}`, []finding{findingAtPointer(literal, "/multipleOf")}},
		{"bound: a negative multipleOf past that range", "3.1.0", specrules.Schema,
			`{multipleOf: -1e1000000000000000000}`, []finding{findingAtPointer(mismatch, "/multipleOf")}},
	})
}

// TestCheck_ValuesAreEnumerated pins the fields that admit a fixed set.
func TestCheck_ValuesAreEnumerated(t *testing.T) {
	verify(t, []scenario{
		{"unknown location", "3.1.0", specrules.Parameter, `{name: a, in: sideways, schema: {}}`, []finding{findingAtPointer(allowed, "/in")}},
		{"known location", "3.1.0", specrules.Parameter, `{name: a, in: query, schema: {}}`, nil},
		{"security scheme type", "3.1.0", specrules.SecurityScheme, `{type: bogus}`, []finding{findingAtPointer(allowed, "/type")}},
		{"api key location", "3.1.0", specrules.SecurityScheme, `{type: apiKey, name: n, in: body}`, []finding{findingAtPointer(allowed, "/in")}},
		{"header style may only be simple", "3.1.0", specrules.Header, `{style: form, schema: {}}`, []finding{findingAtPointer(allowed, "/style")}},
		{"header style simple", "3.1.0", specrules.Header, `{style: simple, schema: {}}`, nil},
		{"encoding style", "3.1.0", specrules.Encoding, `{style: matrix}`, []finding{findingAtPointer(allowed, "/style")}},
		{"style with its location", "3.1.0", specrules.Parameter, `{name: a, in: query, style: pipeDelimited, schema: {}}`, nil},
		{"style barred by its location", "3.1.0", specrules.Parameter, `{name: a, in: query, style: matrix, schema: {}}`,
			[]finding{findingAtPointer(allowed, "/style")}},
		{"style with no location to judge by", "3.1.0", specrules.Parameter, `{name: a, style: matrix, schema: {}}`,
			[]finding{findingAtPointer(required, "")}},
		{"style with an unreadable location", "3.1.0", specrules.Parameter, `{name: a, in: [x], style: matrix, schema: {}}`,
			[]finding{findingAtPointer(mismatch, "/in")}},
		{"style with a location no style fits", "3.1.0", specrules.Parameter, `{name: a, in: sideways, style: matrix, schema: {}}`,
			[]finding{findingAtPointer(allowed, "/in")}},
		{"schema type", "3.0.3", specrules.Schema, `{type: bogus}`, []finding{findingAtPointer(allowed, "/type")}},
		{"schema type list element", "3.1.0", specrules.Schema, `{type: [string, bogus]}`, []finding{findingAtPointer(allowed, "/type/1")}},
		{"schema type list", "3.1.0", specrules.Schema, `{type: [string, "null"]}`, nil},
		{"schema type that is neither a string nor a list", "3.1.0", specrules.Schema, `{type: {}}`, []finding{findingAtPointer(mismatch, "/type")}},
		{"schema type list needs 3.1", "3.0.3", specrules.Schema, `{type: [string]}`, []finding{findingAtPointer(mismatch, "/type")}},
		{"null type needs 3.1", "3.0.3", specrules.Schema, `{type: "null"}`, []finding{findingAtPointer(allowed, "/type")}},
	})
}

// TestCheck_ValuesAddedLaterAreWarnedOf pins the warning for a value a later
// minor added to a field that was already there.
func TestCheck_ValuesAddedLaterAreWarnedOf(t *testing.T) {
	verify(t, []scenario{
		{"querystring in 3.1", "3.1.0", specrules.Parameter, `{name: a, in: querystring, content: {a/b: {}}}`, []finding{findingAtPointer(later, "/in")}},
		{"querystring in 3.2", "3.2.0", specrules.Parameter, `{name: a, in: querystring, content: {a/b: {}}}`, nil},
		{"cookie style in 3.1", "3.1.0", specrules.Parameter, `{name: a, in: cookie, style: cookie, schema: {}}`, []finding{findingAtPointer(later, "/style")}},
		{"cookie style in 3.2", "3.2.0", specrules.Parameter, `{name: a, in: cookie, style: cookie, schema: {}}`, nil},
		{"mutualTLS in 3.0", "3.0.3", specrules.SecurityScheme, `{type: mutualTLS}`, []finding{findingAtPointer(later, "/type")}},
		{"mutualTLS in 3.1", "3.1.0", specrules.SecurityScheme, `{type: mutualTLS}`, nil},
		{"node type in 3.1", "3.1.0", specrules.XML, `{nodeType: element}`, []finding{findingAtPointer(later, "/nodeType")}},
		{"node type unknown in 3.2", "3.2.0", specrules.XML, `{nodeType: other}`, []finding{findingAtPointer(allowed, "/nodeType")}},
	})
}

// TestCheck_EmptyStringsAreRefusedWhereTheGrammarSaysSo pins NonEmpty.
func TestCheck_EmptyStringsAreRefusedWhereTheGrammarSaysSo(t *testing.T) {
	verify(t, []scenario{
		{"server url empty in 3.1", "3.1.0", specrules.Server, `{url: ""}`, nil},
		{"server url empty in 3.2", "3.2.0", specrules.Server, `{url: ""}`, []finding{findingAtPointer(empty, "/url")}},
		{"empty title is a title", "3.2.0", specrules.Info, `{title: "", version: ""}`, nil},
	})
}

// TestCheck_FormatsAreChecked pins the syntaxes a string field must have.
func TestCheck_FormatsAreChecked(t *testing.T) {
	verify(t, []scenario{
		{"relative reference", "3.1.0", specrules.Info, `{title: t, version: v, termsOfService: tos/index.html}`, nil},
		{"absolute uri", "3.1.0", specrules.Info, `{title: t, version: v, termsOfService: "https://e.com/{x}?a=b#f"}`, nil},
		{"empty reference", "3.1.0", specrules.Info, `{title: t, version: v, termsOfService: ""}`, nil},
		{"space in uri", "3.1.0", specrules.Info, `{title: t, version: v, termsOfService: "a b"}`, []finding{findingAtPointer(badFormat, "/termsOfService")}},
		{"control in uri", "3.1.0", specrules.Info, `{title: t, version: v, termsOfService: "a\u0001b"}`, []finding{findingAtPointer(badFormat, "/termsOfService")}},
		{"unparsable uri", "3.1.0", specrules.Info, `{title: t, version: v, termsOfService: "%zz"}`, []finding{findingAtPointer(badFormat, "/termsOfService")}},
		{"email", "3.1.0", specrules.Contact, `{email: a@b.c}`, nil},
		{"email without at", "3.1.0", specrules.Contact, `{email: abc}`, []finding{findingAtPointer(badFormat, "/email")}},
		{"email without name", "3.1.0", specrules.Contact, `{email: "@b.c"}`, []finding{findingAtPointer(badFormat, "/email")}},
		{"email without domain", "3.1.0", specrules.Contact, `{email: "a@"}`, []finding{findingAtPointer(badFormat, "/email")}},
		{"email with two ats", "3.1.0", specrules.Contact, `{email: "a@b@c"}`, []finding{findingAtPointer(badFormat, "/email")}},
		{"email with a space", "3.1.0", specrules.Contact, `{email: "a b@c"}`, []finding{findingAtPointer(badFormat, "/email")}},
		{"namespace with a scheme", "3.1.0", specrules.XML, `{namespace: "http://a/b"}`, nil},
		{"namespace without a scheme", "3.1.0", specrules.XML, `{namespace: "a/b"}`, []finding{findingAtPointer(badFormat, "/namespace")}},
		{"dialect needs a scheme", "3.1.0", specrules.Document, `{openapi: 3.1.0, info: {title: t, version: v}, components: {}, jsonSchemaDialect: "x"}`,
			[]finding{findingAtPointer(badFormat, "/jsonSchemaDialect")}},
	})
}

// TestCheck_TheEmptyStringIsJudgedByItsFormat pins the empty string against
// each format: one it breaks, an email's or an absolute URI's, is reported as
// any other text is, and one admitting it, a URI reference's or a server
// URL's, draws nothing.
func TestCheck_TheEmptyStringIsJudgedByItsFormat(t *testing.T) {
	verify(t, []scenario{
		{"empty email", "3.1.0", specrules.Contact, `{email: ""}`, []finding{findingAtPointer(badFormat, "/email")}},
		{"empty namespace", "3.1.0", specrules.XML, `{namespace: ""}`, []finding{findingAtPointer(badFormat, "/namespace")}},
		{"empty dialect", "3.1.0", specrules.Document, `{openapi: 3.1.0, info: {title: t, version: v}, components: {}, jsonSchemaDialect: ""}`,
			[]finding{findingAtPointer(badFormat, "/jsonSchemaDialect")}},
		{"empty $schema", "3.1.0", specrules.Schema, `{$schema: ""}`, []finding{findingAtPointer(badFormat, "/$schema")}},
		{"empty reference", "3.1.0", specrules.Info, `{title: t, version: v, termsOfService: ""}`, nil},
		{"empty server url", "3.1.0", specrules.Server, `{url: ""}`, nil},
	})
}

// TestCheck_ComponentNamesAreChecked pins the key grammar of the components.
func TestCheck_ComponentNamesAreChecked(t *testing.T) {
	verify(t, []scenario{
		{"good names", "3.1.0", specrules.Components, `{schemas: {a.B-c_1: {}}}`, nil},
		{"space in a name", "3.1.0", specrules.Components, `{schemas: {"a b": {}}}`, []finding{findingAtPointer(badSyntax, "/schemas/a b")}},
		{"empty name", "3.1.0", specrules.Components, `{headers: {"": {}}}`, []finding{findingAtPointer(badSyntax, "/headers/")}},
		{"link names follow the components'", "3.1.0", specrules.Response, `{description: d, links: {"a b": {operationId: x}}}`,
			[]finding{findingAtPointer(badSyntax, "/links/a b")}},
	})
}

// TestCheck_HeaderNamesAreFieldNames pins the key grammar of a headers map, a
// Response Object's and an Encoding Object's: each key is an HTTP field name,
// so a token, an error where it is not, as a header parameter's name is, and
// a key naming the header a key spelled lower in byte order names in another
// case draws a warning at itself, whichever order the map writes the two in.
func TestCheck_HeaderNamesAreFieldNames(t *testing.T) {
	const h = "{schema: {}}"
	verify(t, []scenario{
		{"tokens", "3.1.0", specrules.Response, `{description: d, headers: {X-Rate: ` + h + `, "!#$%&'*+-.^_|~09": ` + h + `}}`, nil},
		{"the empty key", "3.1.0", specrules.Response, `{description: d, headers: {"": ` + h + `}}`,
			[]finding{findingAtPointer(badFormat, "/headers/")}},
		{"a key holding spaces", "3.0.3", specrules.Response, `{description: d, headers: {"not a token": ` + h + `}}`,
			[]finding{findingAtPointer(badFormat, "/headers/not a token")}},
		{"a key past ASCII", "3.2.0", specrules.Response, `{headers: {"X-Rät": ` + h + `}}`, []finding{findingAtPointer(badFormat, "/headers/X-Rät")}},
		{"two keys one header", "3.1.0", specrules.Response, `{description: d, headers: {X-Rate: ` + h + `, x-rate: ` + h + `}}`,
			[]finding{findingAtPointer(duplicate, "/headers/x-rate")}},
		{"the two the other way round", "3.1.0", specrules.Response,
			`{description: d, headers: {x-rate: ` + h + `, X-Rate: ` + h + `}}`, []finding{findingAtPointer(duplicate, "/headers/x-rate")}},
		{"three keys one header", "3.1.0", specrules.Response,
			`{description: d, headers: {x-rate: ` + h + `, X-RATE: ` + h + `, X-Rate: ` + h + `}}`,
			[]finding{findingAtPointer(duplicate, "/headers/x-rate"), findingAtPointer(duplicate, "/headers/X-Rate")}},
		{"an encoding's", "3.1.0", specrules.Encoding, `{headers: {"": ` + h + `, X-Part: ` + h + `, x-part: ` + h + `}}`,
			[]finding{findingAtPointer(badFormat, "/headers/"), findingAtPointer(duplicate, "/headers/x-part")}},
		{"a component's key is a component name", "3.1.0", specrules.Components, `{headers: {X-Rate: ` + h + `, x-rate: ` + h + `}}`, nil},
	})
	diags := run(t, "3.1.0", specrules.Response, `{description: d, headers: {x-rate: `+h+`, X-Rate: `+h+`}}`)
	require.Len(t, diags, 1)
	assert.Equal(t, ir.SeverityWarning, diags[0].Severity)
	assert.Equal(t, `Response Object field "headers" has the keys "X-Rate" and "x-rate", which name one header, `+
		`as HTTP compares field names in any case, and both are read in OpenAPI 3.1 (Response Object)`, diags[0].Message)
	diags = run(t, "3.1.0", specrules.Response, `{description: d, headers: {"": `+h+`}}`)
	require.Len(t, diags, 1)
	assert.Equal(t, ir.SeverityError, diags[0].Severity)
	assert.Equal(t, `Response Object field "headers" has the key "", which must be a token of letters, digits and `+
		"!#$%&'*+-.^_`|~ in OpenAPI 3.1 (Response Object)", diags[0].Message)
}

// TestCheck_ObjectsAreReadWhereTheyAreHeld pins the descent into every
// container shape.
func TestCheck_ObjectsAreReadWhereTheyAreHeld(t *testing.T) {
	verify(t, []scenario{
		{"nested object", "3.1.0", specrules.Info, `{title: t, version: v, license: {}}`, []finding{findingAtPointer(required, "/license")}},
		{"map entry", "3.1.0", specrules.Components, `{parameters: {p: {in: query}}}`, []finding{findingAtPointer(required, "/parameters/p"), findingAtPointer(required, "/parameters/p")}},
		{"map entry that is no object", "3.1.0", specrules.Components, `{parameters: {p: 3}}`, []finding{findingAtPointer(mismatch, "/parameters/p")}},
		{"list item", "3.1.0", specrules.Operation, `{servers: [{url: a}, {}]}`, []finding{findingAtPointer(required, "/servers/1")}},
		{"list item that is no object", "3.1.0", specrules.Operation, `{servers: [a]}`, []finding{findingAtPointer(mismatch, "/servers/0")}},
		{"deep", "3.1.0", specrules.Components, `{schemas: {A: {properties: {b: {items: {discriminator: {}}}}}}}`,
			[]finding{findingAtPointer(required, "/schemas/A/properties/b/items/discriminator")}},
	})
}

// TestCheck_BooleanSchemasStartAtThreeOne pins where true and false stand for a
// schema.
func TestCheck_BooleanSchemasStartAtThreeOne(t *testing.T) {
	verify(t, []scenario{
		{"3.1 component", "3.1.0", specrules.Components, `{schemas: {A: true}}`, nil},
		{"3.0 component", "3.0.3", specrules.Components, `{schemas: {A: true}}`, []finding{findingAtPointer(mismatch, "/schemas/A")}},
		{"3.1 keyword", "3.1.0", specrules.Schema, `{items: false, not: true}`, nil},
		{"3.0 additionalProperties", "3.0.3", specrules.Schema, `{additionalProperties: false}`, nil},
		{"3.0 items", "3.0.3", specrules.Schema, `{items: false}`, []finding{findingAtPointer(mismatch, "/items")}},
		{"3.1 a string is no schema", "3.1.0", specrules.Schema, `{items: x}`, []finding{findingAtPointer(mismatch, "/items")}},
		{"boolean for an object that is not a schema", "3.1.0", specrules.Info, `true`, []finding{findingAtPointer(mismatch, "")}},
	})
}

// TestCheck_ReferencesAreReferenceObjects pins how a $ref is read.
func TestCheck_ReferencesAreReferenceObjects(t *testing.T) {
	verify(t, []scenario{
		{"a parameter reference needs nothing else", "3.1.0", specrules.Parameter, `{$ref: "#/components/parameters/p"}`, nil},
		{"siblings are judged as a Reference Object's", "3.0.3", specrules.Parameter, `{$ref: "#/a", name: [x], in: 5, bogus: 1}`,
			[]finding{findingAtPointer(unknownKey, "/name"), findingAtPointer(unknownKey, "/in"), findingAtPointer(unknownKey, "/bogus")}},
		{"summary beside a 3.0 reference", "3.0.3", specrules.Parameter, `{$ref: "#/a", summary: s}`, []finding{findingAtPointer(later, "/summary")}},
		{"description beside a 3.1 reference", "3.1.0", specrules.Parameter, `{$ref: "#/a", description: d}`, nil},
		{"reference that is no string", "3.1.0", specrules.Parameter, `{$ref: [a]}`, []finding{findingAtPointer(mismatch, "/$ref")}},
		{"reference with a bad uri", "3.1.0", specrules.Parameter, `{$ref: "a b"}`, []finding{findingAtPointer(badFormat, "/$ref")}},
		{"media type reference in 3.1", "3.1.0", specrules.MediaType, `{$ref: "#/components/mediaTypes/m"}`, []finding{findingAtPointer(later, "/$ref")}},
		{"media type reference in 3.2", "3.2.0", specrules.MediaType, `{$ref: "#/components/mediaTypes/m"}`, nil},
		{"3.0 schema reference holds its siblings to 3.0's keywords", "3.0.3", specrules.Schema,
			`{$ref: "#/a", type: bogus, maxLength: -1, nullable: true}`,
			[]finding{findingAtPointer(later, "/maxLength"), findingAtPointer(mismatch, "/maxLength"), findingAtPointer(later, "/nullable"),
				findingAtPointer(later, "/type"), findingAtPointer(allowed, "/type")}},
		{"3.0 schema reference with a description draws the schema's later construct", "3.0.3", specrules.Schema,
			`{$ref: "#/a", description: d}`, []finding{findingAtPointer(later, "/description")}},
		{"3.0 schema reference with a bad uri", "3.0.3", specrules.Schema, `{$ref: "a b"}`, []finding{findingAtPointer(badFormat, "/$ref")}},
		{"3.1 schema reference keeps its siblings", "3.1.0", specrules.Schema, `{$ref: "#/a", type: bogus}`, []finding{findingAtPointer(allowed, "/type")}},
		{"path item reference is a field", "3.1.0", specrules.PathItem, `{$ref: [a]}`, []finding{findingAtPointer(mismatch, "/$ref")}},
	})
}

// TestCheck_PatternedKeysAreRead pins the keys an object admits beside its
// fixed fields.
func TestCheck_PatternedKeysAreRead(t *testing.T) {
	verify(t, []scenario{
		{"paths", "3.1.0", specrules.Paths, `{/a: {get: {}}, x-e: 1}`, nil},
		{"a path's operation", "3.1.0", specrules.Paths, `{/a: {get: {deprecated: 3}}}`, []finding{findingAtPointer(mismatch, "/~1a/get/deprecated")}},
		{"extensions on a closed object are entries", "3.1.0", specrules.SecurityRequirement, `{x-a: foo}`, []finding{findingAtPointer(mismatch, "/x-a")}},
		{"security requirement scopes", "3.1.0", specrules.SecurityRequirement, `{a: [x, y], b: []}`, nil},
		{"security requirement given an object", "3.1.0", specrules.SecurityRequirement, `{a: {}}`, []finding{findingAtPointer(mismatch, "/a")}},
		{"responses", "3.1.0", specrules.Responses, `{"200": {description: d}, default: {description: d}, x-e: 1}`, nil},
		{"a response", "3.1.0", specrules.Responses, `{"200": {}}`, []finding{findingAtPointer(required, "/200")}},
		{"callback entries", "3.1.0", specrules.Callback, `{"{$request.body#/u}": {get: {deprecated: 3}}, x-e: 1}`,
			[]finding{findingAtPointer(mismatch, "/{$request.body#~1u}/get/deprecated")}},
		{"an object with no patterned keys reports others as unknown", "3.1.0", specrules.Info, `{title: t, version: v, other: [1]}`,
			[]finding{findingAtPointer(unknownKey, "/other")}},
	})
}

// TestCheck_RepeatedKeysInRawValuesAreWarnedOf pins the warning for a key
// written twice inside a value the table reads as no object, at every depth,
// in a list and beside a $ref: the lowering keeps such a value, or reads it as
// an ir.Value, at the copy the model reads, so the table is where the repeat is
// said. A key a later minor's schema defines is read as its field, once.
func TestCheck_RepeatedKeysInRawValuesAreWarnedOf(t *testing.T) {
	const info = "title: a, version: v, "
	verify(t, []scenario{
		{"an extension's value", "3.1.0", specrules.Info, `{` + info + `x-e: {k: 1, k: 2}}`, []finding{findingAtPointer(duplicate, "/x-e/k")}},
		{"a mapping in a list, deeper", "3.1.0", specrules.Info, `{` + info + `x-e: [{}, {o: {k: 1, k: 2}}]}`,
			[]finding{findingAtPointer(duplicate, "/x-e/1/o/k")}},
		{"an undefined key's value", "3.1.0", specrules.Info, `{` + info + `bogus: {k: 1, k: 2}}`,
			[]finding{findingAtPointer(unknownKey, "/bogus"), findingAtPointer(duplicate, "/bogus/k")}},
		{"a field read as any value", "3.1.0", specrules.Example, `{value: {k: 1, k: 2}}`, []finding{findingAtPointer(duplicate, "/value/k")}},
		{"a schema's default", "3.1.0", specrules.Schema, `{default: {k: 1, k: 2}}`, []finding{findingAtPointer(duplicate, "/default/k")}},
		{"a schema's undefined keyword", "3.1.0", specrules.Schema, `{bogus: {k: 1, k: 2}}`, []finding{findingAtPointer(duplicate, "/bogus/k")}},
		{"a key beside a $ref", "3.1.0", specrules.Parameter, `{$ref: '#/a', x-n: {k: 1, k: 2}}`,
			[]finding{findingAtPointer(unknownKey, "/x-n"), findingAtPointer(duplicate, "/x-n/k")}},
		{"a patterned object's extension", "3.1.0", specrules.Responses, `{"200": {description: d}, x-e: {k: 1, k: 2}}`,
			[]finding{findingAtPointer(duplicate, "/x-e/k")}},
		{"a later keyword is read as its field", "3.0.3", specrules.Schema, `{prefixItems: [{type: string, type: integer}]}`,
			[]finding{findingAtPointer(later, "/prefixItems"), findingAtPointer(duplicate, "/prefixItems/0/type")}},
		{"each key once", "3.1.0", specrules.Info, `{` + info + `x-e: {k: 1, l: [{k: 1}, {k: 2}]}}`, nil},
	})
}

// TestCheck_AnAliasedUnknownKeyIsPlacedAtItsKey pins where an undefined key
// whose value is an alias sits: at the key, in the mapping writing it, not where
// the alias's anchor is written, another key's coordinate, so the place does not
// hang on which of two keys the anchor is written under. The locator places a
// pointer as load does, at the node it reaches.
func TestCheck_AnAliasedUnknownKeyIsPlacedAtItsKey(t *testing.T) {
	var doc yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte("openapi: 3.1.0\nx-m: &m {k: 1}\ninfo: {title: t, version: v, bogus: *m}\n"+
		"paths: {}\n"), &doc))
	root, view := nodeview.DocumentRoot(&doc), nodeview.New()
	diags := specrules.NewChecker().Check(t.Context(), &doc, "3.1.0", specrules.Options{
		Locate: func(pointer jsontext.Pointer) ir.Provenance {
			path, _ := view.DocumentPath(root, pointer)
			n := path[len(path)-1]
			return ir.Provenance{Pointer: pointer, Position: ir.Position{Line: n.Line, Column: n.Column}}
		},
	})
	var places []ir.Provenance
	for _, diagnostic := range diags {
		if diagnostic.Code == unknownKey {
			places = append(places, diagnostic.Provenance)
		}
	}
	assert.Equal(t, []ir.Provenance{{Pointer: "/info/bogus", Position: ir.Position{Line: 3, Column: 37}}}, places,
		"at the key's own line, not the anchor's")
}

// TestCheck_RequestsAreHeldToTheirMinor pins that an unsupported version is
// held to nothing and an unknown start to nothing either.
func TestCheck_RequestsAreHeldToTheirMinor(t *testing.T) {
	assert.Empty(t, run(t, "3.5.0", specrules.Info, `{}`))
	assert.Empty(t, run(t, "4", specrules.Info, `{}`))
	assert.Empty(t, run(t, "3.1.0", specrules.Kind(99), `{}`))
}

// TestCheck_RepeatedKeysAreWarnedOf pins the warning for a key a mapping writes
// twice, whose earlier copy the model drops without a word.
func TestCheck_RepeatedKeysAreWarnedOf(t *testing.T) {
	verify(t, []scenario{
		{"a field twice", "3.1.0", specrules.Info, `{title: a, title: b, version: v}`, []finding{findingAtPointer(duplicate, "/title")}},
		{"a key of a map twice", "3.1.0", specrules.Components, `{schemas: {A: {}, B: {}, A: {}}}`, []finding{findingAtPointer(duplicate, "/schemas/A")}},
		{"a path twice", "3.1.0", specrules.Paths, `{/a: {}, /a: {}}`, []finding{findingAtPointer(duplicate, "/~1a")}},
		{"a third copy is no second finding", "3.1.0", specrules.Info, `{title: a, version: v, version: w, version: x}`,
			[]finding{findingAtPointer(duplicate, "/version")}},
		{"a key a merge also supplies", "3.1.0", specrules.Info, `{<<: {title: a}, title: b, version: v}`, nil},
		{"each key once", "3.1.0", specrules.Info, `{title: a, version: v}`, nil},
	})
	got := run(t, "3.1.0", specrules.Info, `{title: a, title: b, version: v}`)
	require.Len(t, got, 1)
	assert.Equal(t, ir.SeverityWarning, got[0].Severity)
}
