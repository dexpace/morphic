package oas_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/oas"
	"github.com/dexpace/morphic/ir"
)

// TestCheck_RequiredFieldsFollowTheMinor pins which minors require a field.
func TestCheck_RequiredFieldsFollowTheMinor(t *testing.T) {
	verify(t, []scenario{
		{"info lacks title and version", "3.1.0", oas.Info, `{}`, []finding{at(required, ""), at(required, "")}},
		{"info complete", "3.1.0", oas.Info, `{title: t, version: "1"}`, nil},
		{"info lacks only version", "3.0.3", oas.Info, `{title: t}`, []finding{at(required, "")}},
		{"response description required in 3.0", "3.0.3", oas.Response, `{}`, []finding{at(required, "")}},
		{"response description required in 3.1", "3.1.0", oas.Response, `{}`, []finding{at(required, "")}},
		{"response description optional in 3.2", "3.2.0", oas.Response, `{}`, nil},
		{"operation responses required in 3.0", "3.0.3", oas.Operation, `{}`, []finding{at(required, "")}},
		{"operation responses optional in 3.1", "3.1.0", oas.Operation, `{}`, nil},
		{"request body content required", "3.2.0", oas.RequestBody, `{}`, []finding{at(required, "")}},
		{"server url required", "3.0.3", oas.Server, `{}`, []finding{at(required, "")}},
		{"a field that a later minor added is not required earlier", "3.0.3", oas.OAuthFlow,
			`{scopes: {}, deviceAuthorizationUrl: "u"}`, []finding{at(later, "/deviceAuthorizationUrl")}},
		{"the security scheme's type is the auth package's to report", "3.1.0", oas.SecurityScheme, `{}`, nil},
	})
}

// TestCheck_ShapesFollowTheField pins what each shape admits, and that a
// number or a boolean where a string belongs is a warning: the text it writes
// says what it means.
func TestCheck_ShapesFollowTheField(t *testing.T) {
	verify(t, []scenario{
		{"string field given a list", "3.1.0", oas.Info, `{title: [a], version: v}`, []finding{at(mismatch, "/title")}},
		{"string field given null", "3.1.0", oas.Info, `{title: ~, version: v}`, []finding{at(mismatch, "/title")}},
		{"string field given a number", "3.1.0", oas.Info, `{title: 12, version: 1.0}`,
			[]finding{at(mismatch, "/title"), at(mismatch, "/version")}},
		{"string field given a date", "3.1.0", oas.Info, `{title: t, version: 2024-01-01}`, nil},
		{"string list given a boolean", "3.1.0", oas.Schema, `{required: [a, true]}`, []finding{at(mismatch, "/required/1")}},
		{"string map given a number", "3.1.0", oas.OAuthFlow, `{tokenUrl: 'https://t', scopes: {a: 1, b: x}}`,
			[]finding{at(mismatch, "/scopes/a")}},
		{"object field given a scalar", "3.1.0", oas.Info, `{title: t, version: v, contact: x}`, []finding{at(mismatch, "/contact")}},
		{"the object itself is a list", "3.1.0", oas.Info, `[a]`, []finding{at(mismatch, "")}},
		{"boolean field given a string", "3.1.0", oas.Operation, `{deprecated: "true"}`, []finding{at(mismatch, "/deprecated")}},
		{"boolean field given a boolean", "3.1.0", oas.Operation, `{deprecated: true}`, nil},
		{"list field given a mapping", "3.1.0", oas.Operation, `{parameters: {}}`, []finding{at(mismatch, "/parameters")}},
		{"map field given a list", "3.1.0", oas.Components, `{schemas: []}`, []finding{at(mismatch, "/schemas")}},
		{"string list given an object item", "3.1.0", oas.Operation, `{tags: [a, {b: c}]}`, []finding{at(mismatch, "/tags")}},
		{"string list given strings", "3.1.0", oas.Operation, `{tags: [a, b]}`, nil},
		{"string map given a list entry", "3.1.0", oas.OAuthFlow, `{scopes: {a: [b]}}`, []finding{at(mismatch, "/scopes")}},
		{"string map given strings", "3.1.0", oas.OAuthFlow, `{scopes: {a: b}}`, nil},
		{"raw list given a scalar", "3.1.0", oas.Schema, `{enum: x}`, []finding{at(mismatch, "/enum")}},
		{"raw map given a list", "3.1.0", oas.Schema, `{$vocabulary: []}`, []finding{at(mismatch, "/$vocabulary")}},
		{"any field takes anything", "3.1.0", oas.Schema, `{default: [1, {a: b}], const: ~, example: x}`, nil},
	})
	for doc, want := range map[string]string{
		`{title: 12, version: v}`:   `Info Object field "title" must be a string in OpenAPI 3.1 (Info Object); it is the number 12`,
		`{title: t, version: true}`: `Info Object field "version" must be a string in OpenAPI 3.1 (Info Object); it is the boolean true`,
	} {
		diags := run(t, "3.1.0", oas.Info, doc)
		require.Len(t, diags, 1, doc)
		assert.Equal(t, ir.SeverityWarning, diags[0].Severity, "a string written unquoted means what it says")
		assert.Equal(t, want, diags[0].Message)
	}
	diags := run(t, "3.1.0", oas.Schema, `{required: [a, 1]}`)
	require.Len(t, diags, 1)
	assert.Contains(t, diags[0].Message, `Schema Object field "required" item 1 must be a string`)
}

// TestCheck_NumbersFollowTheirShape pins the numeric shapes, and that the numeric
// bounds the schema lowering reads off the raw node are left to it: it reports a
// value that is no decimal a BigVal holds, a null included, or is in the other
// dialect's form, and takes a quoted number as the decimal it spells. A number
// outside a bound's range, a multipleOf no greater than 0, quoted or not, is
// the table's.
func TestCheck_NumbersFollowTheirShape(t *testing.T) {
	verify(t, []scenario{
		{"natural: zero", "3.1.0", oas.Schema, `{maxLength: 0}`, nil},
		{"natural: negative", "3.1.0", oas.Schema, `{maxLength: -1}`, []finding{at(mismatch, "/maxLength")}},
		{"natural: whole float", "3.1.0", oas.Schema, `{maxLength: 2.0}`, nil},
		{"natural: fraction", "3.1.0", oas.Schema, `{maxLength: 1.5}`, []finding{at(mismatch, "/maxLength")}},
		{"natural: hex", "3.1.0", oas.Schema, `{maxLength: 0x10}`, nil},
		{"natural: underscored", "3.1.0", oas.Schema, `{maxLength: 1_000}`, nil},
		{"natural: an underscored whole float", "3.1.0", oas.Schema, `{maxLength: 1_0.0}`, nil},
		{"natural: a fraction is text", "3.1.0", oas.Schema, `{maxLength: 4/2}`, []finding{at(mismatch, "/maxLength")}},
		{"natural: a binary exponent is text", "3.1.0", oas.Schema, `{maxLength: 0x1p4}`,
			[]finding{at(mismatch, "/maxLength")}},
		{"natural: quoted", "3.1.0", oas.Schema, `{maxLength: "5"}`, []finding{at(mismatch, "/maxLength")}},
		{"natural: infinity", "3.1.0", oas.Schema, `{maxLength: .inf}`, []finding{at(mismatch, "/maxLength")}},
		{"natural: past a rational's range", "3.1.0", oas.Schema, `{maxLength: 1e1000001}`, nil},
		{"natural: whole with a point, past it", "3.1.0", oas.Schema, `{maxLength: 1.5e1000001}`, nil},
		{"natural: negative past it", "3.1.0", oas.Schema, `{maxLength: -1e1000001}`,
			[]finding{at(mismatch, "/maxLength")}},
		{"natural: a fraction past it", "3.1.0", oas.Schema, `{maxLength: 1e-1000001}`,
			[]finding{at(mismatch, "/maxLength")}},
		{"natural: past the range a BigVal holds", "3.1.0", oas.Schema, `{maxLength: 1e1000000000000000000}`, nil},
		{"natural: negative past that range", "3.1.0", oas.Schema, `{maxLength: -1e1000000000000000000}`,
			[]finding{at(mismatch, "/maxLength")}},
		{"natural: a fraction past that range", "3.1.0", oas.Schema, `{maxLength: 1e-1000000000000000000}`,
			[]finding{at(mismatch, "/maxLength")}},
		{"bound: exponent", "3.1.0", oas.Schema, `{maximum: 1e3, minimum: -2.5, multipleOf: 0.5}`, nil},
		{"bound: past float64's range", "3.1.0", oas.Schema, `{maximum: 1e999, minLength: 1e30}`, nil},
		{"bound: no number is the lowering's", "3.1.0", oas.Schema,
			`{maximum: high, minimum: true, multipleOf: [1], exclusiveMinimum: .inf, exclusiveMaximum: "5"}`, nil},
		{"bound: the other dialect's form is the lowering's", "3.0.3", oas.Schema,
			`{exclusiveMinimum: 5, exclusiveMaximum: 6}`, nil},
		{"bound: a multipleOf of zero is the table's", "3.1.0", oas.Schema, `{multipleOf: 0}`,
			[]finding{at(mismatch, "/multipleOf")}},
		{"bound: a negative multipleOf in 3.0", "3.0.3", oas.Schema, `{multipleOf: -2}`, []finding{at(mismatch, "/multipleOf")}},
		{"bound: a quoted multipleOf of zero", "3.1.0", oas.Schema, `{multipleOf: "0"}`, []finding{at(mismatch, "/multipleOf")}},
		{"bound: a quoted multipleOf above zero is the lowering's", "3.1.0", oas.Schema, `{multipleOf: "0.5"}`, nil},
		{"bound: a null is the lowering's", "3.1.0", oas.Schema, `{maximum: ~}`, nil},
		{"bound: a negative multipleOf past a rational's range", "3.1.0", oas.Schema, `{multipleOf: -1e1000001}`,
			[]finding{at(mismatch, "/multipleOf")}},
		{"bound: a negative multipleOf below one", "3.1.0", oas.Schema, `{multipleOf: -1e-1000001}`,
			[]finding{at(mismatch, "/multipleOf")}},
		{"bound: a multipleOf above zero below one", "3.1.0", oas.Schema, `{multipleOf: 1e-1000001}`, nil},
		{"bound: past the range a BigVal holds is the lowering's", "3.1.0", oas.Schema,
			`{multipleOf: 1e1000000000000000000}`, nil},
		{"bound: a negative multipleOf past that range", "3.1.0", oas.Schema,
			`{multipleOf: -1e1000000000000000000}`, []finding{at(mismatch, "/multipleOf")}},
	})
}

// TestCheck_ValuesAreEnumerated pins the fields that admit a fixed set.
func TestCheck_ValuesAreEnumerated(t *testing.T) {
	verify(t, []scenario{
		{"unknown location", "3.1.0", oas.Parameter, `{name: a, in: sideways, schema: {}}`, []finding{at(allowed, "/in")}},
		{"known location", "3.1.0", oas.Parameter, `{name: a, in: query, schema: {}}`, nil},
		{"security scheme type", "3.1.0", oas.SecurityScheme, `{type: bogus}`, []finding{at(allowed, "/type")}},
		{"api key location", "3.1.0", oas.SecurityScheme, `{type: apiKey, name: n, in: body}`, []finding{at(allowed, "/in")}},
		{"header style may only be simple", "3.1.0", oas.Header, `{style: form, schema: {}}`, []finding{at(allowed, "/style")}},
		{"header style simple", "3.1.0", oas.Header, `{style: simple, schema: {}}`, nil},
		{"encoding style", "3.1.0", oas.Encoding, `{style: matrix}`, []finding{at(allowed, "/style")}},
		{"style with its location", "3.1.0", oas.Parameter, `{name: a, in: query, style: pipeDelimited, schema: {}}`, nil},
		{"style barred by its location", "3.1.0", oas.Parameter, `{name: a, in: query, style: matrix, schema: {}}`,
			[]finding{at(allowed, "/style")}},
		{"style with no location to judge by", "3.1.0", oas.Parameter, `{name: a, style: matrix, schema: {}}`,
			[]finding{at(required, "")}},
		{"style with an unreadable location", "3.1.0", oas.Parameter, `{name: a, in: [x], style: matrix, schema: {}}`,
			[]finding{at(mismatch, "/in")}},
		{"style with a location no style fits", "3.1.0", oas.Parameter, `{name: a, in: sideways, style: matrix, schema: {}}`,
			[]finding{at(allowed, "/in")}},
		{"schema type", "3.0.3", oas.Schema, `{type: bogus}`, []finding{at(allowed, "/type")}},
		{"schema type list element", "3.1.0", oas.Schema, `{type: [string, bogus]}`, []finding{at(allowed, "/type/1")}},
		{"schema type list", "3.1.0", oas.Schema, `{type: [string, "null"]}`, nil},
		{"schema type that is neither a string nor a list", "3.1.0", oas.Schema, `{type: {}}`, []finding{at(mismatch, "/type")}},
		{"schema type list needs 3.1", "3.0.3", oas.Schema, `{type: [string]}`, []finding{at(mismatch, "/type")}},
		{"null type needs 3.1", "3.0.3", oas.Schema, `{type: "null"}`, []finding{at(allowed, "/type")}},
	})
}

// TestCheck_ValuesAddedLaterAreWarnedOf pins the warning for a value a later
// minor added to a field that was already there.
func TestCheck_ValuesAddedLaterAreWarnedOf(t *testing.T) {
	verify(t, []scenario{
		{"querystring in 3.1", "3.1.0", oas.Parameter, `{name: a, in: querystring, content: {a/b: {}}}`, []finding{at(later, "/in")}},
		{"querystring in 3.2", "3.2.0", oas.Parameter, `{name: a, in: querystring, content: {a/b: {}}}`, nil},
		{"cookie style in 3.1", "3.1.0", oas.Parameter, `{name: a, in: cookie, style: cookie, schema: {}}`, []finding{at(later, "/style")}},
		{"cookie style in 3.2", "3.2.0", oas.Parameter, `{name: a, in: cookie, style: cookie, schema: {}}`, nil},
		{"mutualTLS in 3.0", "3.0.3", oas.SecurityScheme, `{type: mutualTLS}`, []finding{at(later, "/type")}},
		{"mutualTLS in 3.1", "3.1.0", oas.SecurityScheme, `{type: mutualTLS}`, nil},
		{"node type in 3.1", "3.1.0", oas.XML, `{nodeType: element}`, []finding{at(later, "/nodeType")}},
		{"node type unknown in 3.2", "3.2.0", oas.XML, `{nodeType: other}`, []finding{at(allowed, "/nodeType")}},
	})
}

// TestCheck_EmptyStringsAreRefusedWhereTheGrammarSaysSo pins NonEmpty.
func TestCheck_EmptyStringsAreRefusedWhereTheGrammarSaysSo(t *testing.T) {
	verify(t, []scenario{
		{"server url empty in 3.1", "3.1.0", oas.Server, `{url: ""}`, nil},
		{"server url empty in 3.2", "3.2.0", oas.Server, `{url: ""}`, []finding{at(empty, "/url")}},
		{"empty title is a title", "3.2.0", oas.Info, `{title: "", version: ""}`, nil},
	})
}

// TestCheck_FormatsAreChecked pins the syntaxes a string field must have.
func TestCheck_FormatsAreChecked(t *testing.T) {
	verify(t, []scenario{
		{"relative reference", "3.1.0", oas.Info, `{title: t, version: v, termsOfService: tos/index.html}`, nil},
		{"absolute uri", "3.1.0", oas.Info, `{title: t, version: v, termsOfService: "https://e.com/{x}?a=b#f"}`, nil},
		{"empty reference", "3.1.0", oas.Info, `{title: t, version: v, termsOfService: ""}`, nil},
		{"space in uri", "3.1.0", oas.Info, `{title: t, version: v, termsOfService: "a b"}`, []finding{at(badFormat, "/termsOfService")}},
		{"control in uri", "3.1.0", oas.Info, `{title: t, version: v, termsOfService: "a\u0001b"}`, []finding{at(badFormat, "/termsOfService")}},
		{"unparsable uri", "3.1.0", oas.Info, `{title: t, version: v, termsOfService: "%zz"}`, []finding{at(badFormat, "/termsOfService")}},
		{"email", "3.1.0", oas.Contact, `{email: a@b.c}`, nil},
		{"email without at", "3.1.0", oas.Contact, `{email: abc}`, []finding{at(badFormat, "/email")}},
		{"email without name", "3.1.0", oas.Contact, `{email: "@b.c"}`, []finding{at(badFormat, "/email")}},
		{"email without domain", "3.1.0", oas.Contact, `{email: "a@"}`, []finding{at(badFormat, "/email")}},
		{"email with two ats", "3.1.0", oas.Contact, `{email: "a@b@c"}`, []finding{at(badFormat, "/email")}},
		{"email with a space", "3.1.0", oas.Contact, `{email: "a b@c"}`, []finding{at(badFormat, "/email")}},
		{"namespace with a scheme", "3.1.0", oas.XML, `{namespace: "http://a/b"}`, nil},
		{"namespace without a scheme", "3.1.0", oas.XML, `{namespace: "a/b"}`, []finding{at(badFormat, "/namespace")}},
		{"dialect needs a scheme", "3.1.0", oas.Document, `{openapi: 3.1.0, info: {title: t, version: v}, components: {}, jsonSchemaDialect: "x"}`,
			[]finding{at(badFormat, "/jsonSchemaDialect")}},
	})
}

// TestCheck_ComponentNamesAreChecked pins the key grammar of the components.
func TestCheck_ComponentNamesAreChecked(t *testing.T) {
	verify(t, []scenario{
		{"good names", "3.1.0", oas.Components, `{schemas: {a.B-c_1: {}}}`, nil},
		{"space in a name", "3.1.0", oas.Components, `{schemas: {"a b": {}}}`, []finding{at(badSyntax, "/schemas/a b")}},
		{"empty name", "3.1.0", oas.Components, `{headers: {"": {}}}`, []finding{at(badSyntax, "/headers/")}},
		{"link names follow the components'", "3.1.0", oas.Response, `{description: d, links: {"a b": {operationId: x}}}`,
			[]finding{at(badSyntax, "/links/a b")}},
	})
}

// TestCheck_ObjectsAreReadWhereTheyAreHeld pins the descent into every
// container shape.
func TestCheck_ObjectsAreReadWhereTheyAreHeld(t *testing.T) {
	verify(t, []scenario{
		{"nested object", "3.1.0", oas.Info, `{title: t, version: v, license: {}}`, []finding{at(required, "/license")}},
		{"map entry", "3.1.0", oas.Components, `{parameters: {p: {in: query}}}`, []finding{at(required, "/parameters/p"), at(required, "/parameters/p")}},
		{"map entry that is no object", "3.1.0", oas.Components, `{parameters: {p: 3}}`, []finding{at(mismatch, "/parameters/p")}},
		{"list item", "3.1.0", oas.Operation, `{servers: [{url: a}, {}]}`, []finding{at(required, "/servers/1")}},
		{"list item that is no object", "3.1.0", oas.Operation, `{servers: [a]}`, []finding{at(mismatch, "/servers/0")}},
		{"deep", "3.1.0", oas.Components, `{schemas: {A: {properties: {b: {items: {discriminator: {}}}}}}}`,
			[]finding{at(required, "/schemas/A/properties/b/items/discriminator")}},
	})
}

// TestCheck_BooleanSchemasStartAtThreeOne pins where true and false stand for a
// schema.
func TestCheck_BooleanSchemasStartAtThreeOne(t *testing.T) {
	verify(t, []scenario{
		{"3.1 component", "3.1.0", oas.Components, `{schemas: {A: true}}`, nil},
		{"3.0 component", "3.0.3", oas.Components, `{schemas: {A: true}}`, []finding{at(mismatch, "/schemas/A")}},
		{"3.1 keyword", "3.1.0", oas.Schema, `{items: false, not: true}`, nil},
		{"3.0 additionalProperties", "3.0.3", oas.Schema, `{additionalProperties: false}`, nil},
		{"3.0 items", "3.0.3", oas.Schema, `{items: false}`, []finding{at(mismatch, "/items")}},
		{"3.1 a string is no schema", "3.1.0", oas.Schema, `{items: x}`, []finding{at(mismatch, "/items")}},
		{"boolean for an object that is not a schema", "3.1.0", oas.Info, `true`, []finding{at(mismatch, "")}},
	})
}

// TestCheck_ReferencesAreReferenceObjects pins how a $ref is read.
func TestCheck_ReferencesAreReferenceObjects(t *testing.T) {
	verify(t, []scenario{
		{"a parameter reference needs nothing else", "3.1.0", oas.Parameter, `{$ref: "#/components/parameters/p"}`, nil},
		{"siblings are ignored", "3.0.3", oas.Parameter, `{$ref: "#/a", name: [x], in: 5, bogus: 1}`, nil},
		{"summary beside a 3.0 reference", "3.0.3", oas.Parameter, `{$ref: "#/a", summary: s}`, []finding{at(later, "/summary")}},
		{"description beside a 3.1 reference", "3.1.0", oas.Parameter, `{$ref: "#/a", description: d}`, nil},
		{"reference that is no string", "3.1.0", oas.Parameter, `{$ref: [a]}`, []finding{at(mismatch, "/$ref")}},
		{"reference with a bad uri", "3.1.0", oas.Parameter, `{$ref: "a b"}`, []finding{at(badFormat, "/$ref")}},
		{"media type reference in 3.1", "3.1.0", oas.MediaType, `{$ref: "#/components/mediaTypes/m"}`, []finding{at(later, "/$ref")}},
		{"media type reference in 3.2", "3.2.0", oas.MediaType, `{$ref: "#/components/mediaTypes/m"}`, nil},
		{"3.0 schema reference holds its siblings to 3.0's keywords", "3.0.3", oas.Schema,
			`{$ref: "#/a", type: bogus, maxLength: -1, nullable: true}`,
			[]finding{at(later, "/maxLength"), at(mismatch, "/maxLength"), at(later, "/nullable"),
				at(later, "/type"), at(allowed, "/type")}},
		{"3.0 schema reference with a description draws the schema's later construct", "3.0.3", oas.Schema,
			`{$ref: "#/a", description: d}`, []finding{at(later, "/description")}},
		{"3.0 schema reference with a bad uri", "3.0.3", oas.Schema, `{$ref: "a b"}`, []finding{at(badFormat, "/$ref")}},
		{"3.1 schema reference keeps its siblings", "3.1.0", oas.Schema, `{$ref: "#/a", type: bogus}`, []finding{at(allowed, "/type")}},
		{"path item reference is a field", "3.1.0", oas.PathItem, `{$ref: [a]}`, []finding{at(mismatch, "/$ref")}},
	})
}

// TestCheck_PatternedKeysAreRead pins the keys an object admits beside its
// fixed fields.
func TestCheck_PatternedKeysAreRead(t *testing.T) {
	verify(t, []scenario{
		{"paths", "3.1.0", oas.Paths, `{/a: {get: {}}, x-e: 1}`, nil},
		{"a path's operation", "3.1.0", oas.Paths, `{/a: {get: {deprecated: 3}}}`, []finding{at(mismatch, "/~1a/get/deprecated")}},
		{"extensions on a closed object are entries", "3.1.0", oas.SecurityRequirement, `{x-a: foo}`, []finding{at(mismatch, "/x-a")}},
		{"security requirement scopes", "3.1.0", oas.SecurityRequirement, `{a: [x, y], b: []}`, nil},
		{"security requirement given an object", "3.1.0", oas.SecurityRequirement, `{a: {}}`, []finding{at(mismatch, "/a")}},
		{"responses", "3.1.0", oas.Responses, `{"200": {description: d}, default: {description: d}, x-e: 1}`, nil},
		{"a response", "3.1.0", oas.Responses, `{"200": {}}`, []finding{at(required, "/200")}},
		{"callback entries", "3.1.0", oas.Callback, `{"{$request.body#/u}": {get: {deprecated: 3}}, x-e: 1}`,
			[]finding{at(mismatch, "/{$request.body#~1u}/get/deprecated")}},
		{"an object with no patterned keys leaves others alone", "3.1.0", oas.Info, `{title: t, version: v, other: [1]}`, nil},
	})
}

// TestCheck_RequestsAreHeldToTheirMinor pins that an unsupported version is
// held to nothing and an unknown start to nothing either.
func TestCheck_RequestsAreHeldToTheirMinor(t *testing.T) {
	assert.Empty(t, run(t, "3.5.0", oas.Info, `{}`))
	assert.Empty(t, run(t, "4", oas.Info, `{}`))
	assert.Empty(t, run(t, "3.1.0", oas.Kind(99), `{}`))
}

// TestCheck_RepeatedKeysAreWarnedOf pins the warning for a key a mapping writes
// twice, whose earlier copy the model drops without a word.
func TestCheck_RepeatedKeysAreWarnedOf(t *testing.T) {
	verify(t, []scenario{
		{"a field twice", "3.1.0", oas.Info, `{title: a, title: b, version: v}`, []finding{at(duplicate, "/title")}},
		{"a key of a map twice", "3.1.0", oas.Components, `{schemas: {A: {}, B: {}, A: {}}}`, []finding{at(duplicate, "/schemas/A")}},
		{"a path twice", "3.1.0", oas.Paths, `{/a: {}, /a: {}}`, []finding{at(duplicate, "/~1a")}},
		{"a third copy is no second finding", "3.1.0", oas.Info, `{title: a, version: v, version: w, version: x}`,
			[]finding{at(duplicate, "/version")}},
		{"a key a merge also supplies", "3.1.0", oas.Info, `{<<: {title: a}, title: b, version: v}`, nil},
		{"each key once", "3.1.0", oas.Info, `{title: a, version: v}`, nil},
	})
	got := run(t, "3.1.0", oas.Info, `{title: a, title: b, version: v}`)
	require.Len(t, got, 1)
	assert.Equal(t, ir.SeverityWarning, got[0].Severity)
}
