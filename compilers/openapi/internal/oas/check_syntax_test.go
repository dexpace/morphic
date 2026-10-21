package oas_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/oas"
	"github.com/dexpace/morphic/ir"
)

// TestCheck_NamesSentInAHeaderOrACookieAreTokens pins the grammar the
// specifications point to for a name sent as an HTTP header or a cookie.
func TestCheck_NamesSentInAHeaderOrACookieAreTokens(t *testing.T) {
	param := func(name, in string) string { return "{name: " + name + ", in: " + in + ", schema: {}}" }
	verify(t, []scenario{
		{"a header token", "3.1.0", oas.Parameter, param("X-Request-Id", "header"), nil},
		{"a header with a space", "3.1.0", oas.Parameter, param(`"X Id"`, "header"), []finding{at(badFormat, "/name")}},
		{"a header with a colon", "3.0.3", oas.Parameter, param(`"X:Id"`, "header"), []finding{at(badFormat, "/name")}},
		{"an empty header name", "3.2.0", oas.Parameter, param(`""`, "header"), []finding{at(badFormat, "/name")}},
		{"a cookie token", "3.1.0", oas.Parameter, param("session_id", "cookie"), nil},
		{"a cookie with a semicolon", "3.1.0", oas.Parameter, param(`"a;b"`, "cookie"), []finding{at(badFormat, "/name")}},
		{"a cookie with a non-ASCII letter", "3.2.0", oas.Parameter, param(`"café"`, "cookie"), []finding{at(badFormat, "/name")}},
		{"a query name is any string", "3.1.0", oas.Parameter, param(`"a b;c"`, "query"), nil},
		{"a path name is the template's to judge", "3.1.0", oas.Parameter, `{name: "a b", in: path, required: true, schema: {}}`, nil},
		{"no name, no token to judge", "3.1.0", oas.Parameter, `{in: header, schema: {}}`, []finding{at(required, "")}},
		{"a name that is no scalar", "3.1.0", oas.Parameter, `{name: [a], in: header, schema: {}}`, []finding{at(mismatch, "/name")}},
	})
}

// TestCheck_AnAPIKeyIsSentUnderAToken pins the same grammar for the name of an
// API key sent in a header or a cookie.
func TestCheck_AnAPIKeyIsSentUnderAToken(t *testing.T) {
	key := func(name, in string) string { return "{type: apiKey, name: " + name + ", in: " + in + "}" }
	verify(t, []scenario{
		{"a header token", "3.1.0", oas.SecurityScheme, key("X-API-Key", "header"), nil},
		{"a header with a space", "3.0.3", oas.SecurityScheme, key(`"X API"`, "header"), []finding{at(badFormat, "/name")}},
		{"an empty header name", "3.1.0", oas.SecurityScheme, key(`""`, "header"), []finding{at(badFormat, "/name")}},
		{"a cookie with a comma", "3.2.0", oas.SecurityScheme, key(`"a,b"`, "cookie"), []finding{at(badFormat, "/name")}},
		{"a cookie token", "3.2.0", oas.SecurityScheme, key("sid", "cookie"), nil},
		{"a query name is any string", "3.1.0", oas.SecurityScheme, key(`"a b"`, "query"), nil},
		{"a name beside another type is not judged", "3.1.0", oas.SecurityScheme,
			`{type: http, scheme: basic, name: "a b", in: header}`, nil},
	})
}

// TestCheck_AnHTTPSchemeIsAToken pins the auth-scheme grammar, and that an
// empty scheme is left to the lowering, which reports it as an incomplete
// scheme.
func TestCheck_AnHTTPSchemeIsAToken(t *testing.T) {
	verify(t, []scenario{
		{"bearer", "3.0.3", oas.SecurityScheme, `{type: http, scheme: bearer}`, nil},
		{"a scheme with a space", "3.0.3", oas.SecurityScheme, `{type: http, scheme: "my scheme"}`, []finding{at(badFormat, "/scheme")}},
		{"a scheme with a slash", "3.1.0", oas.SecurityScheme, `{type: http, scheme: "a/b"}`, []finding{at(badFormat, "/scheme")}},
		{"a scheme with a slash in 3.2", "3.2.0", oas.SecurityScheme, `{type: http, scheme: "a/b"}`, []finding{at(badFormat, "/scheme")}},
		{"an empty scheme is the lowering's", "3.1.0", oas.SecurityScheme, `{type: http, scheme: ""}`, nil},
		{"a scheme beside another type is not judged", "3.1.0", oas.SecurityScheme,
			`{type: apiKey, name: n, in: query, scheme: "a b"}`, nil},
	})
}

// TestCheck_TheOpenAPIFieldIsAVersionNumber pins the major.minor.patch scheme
// every minor states under "Versions": a version cut short, or one with a
// suffix, is no version number.
func TestCheck_TheOpenAPIFieldIsAVersionNumber(t *testing.T) {
	doc := func(v string) string { return "{openapi: '" + v + "', info: {title: t, version: v}, paths: {}}" }
	verify(t, []scenario{
		{"a version number", "3.1.0", oas.Document, doc("3.1.0"), nil},
		{"many digits", "3.1.0", oas.Document, doc("3.10.207"), nil},
		{"two parts", "3.1.0", oas.Document, doc("3.1"), []finding{at(badFormat, "/openapi")}},
		{"four parts", "3.1.0", oas.Document, doc("3.1.0.1"), []finding{at(badFormat, "/openapi")}},
		{"a suffix", "3.1.0", oas.Document, doc("3.1.0-rc1"), []finding{at(badFormat, "/openapi")}},
		{"a letter", "3.1.0", oas.Document, doc("3.1.x"), []finding{at(badFormat, "/openapi")}},
		{"an empty part", "3.1.0", oas.Document, doc("3..1"), []finding{at(badFormat, "/openapi")}},
	})
}

// TestCheck_AServerURLWritesOnlyWhatAURLHolds pins the check every minor's
// server URL takes: the characters a URL may hold, a variable's braces aside,
// and a percent sign only as an escape.
func TestCheck_AServerURLWritesOnlyWhatAURLHolds(t *testing.T) {
	url := func(u string) string { return `{url: "` + u + `"}` }
	verify(t, []scenario{
		{"variables in host and port", "3.0.3", oas.Server,
			`{url: "https://{host}:{port}/v1", variables: {host: {default: h}, port: {default: "1"}}}`, nil},
		{"a relative URL", "3.1.0", oas.Server, url("/v1"), nil},
		{"an escape", "3.1.0", oas.Server, url("https://x/%41%e9"), nil},
		{"text beyond ASCII", "3.1.0", oas.Server, url("https://exämple.org"), nil},
		{"a malformed escape", "3.1.0", oas.Server, url("https://x/%zz"), []finding{at(badFormat, "/url")}},
		{"an escape cut short", "3.1.0", oas.Server, url("https://x/%4"), []finding{at(badFormat, "/url")}},
		{"a space", "3.0.3", oas.Server, url("https://a b"), []finding{at(badFormat, "/url")}},
		{"a pipe", "3.1.0", oas.Server, url("https://a|b"), []finding{at(badFormat, "/url")}},
	})
}

// TestCheck_AnEncodingContentTypeListsMediaTypes pins the list an Encoding
// Object's contentType writes: media types and ranges, separated by commas.
func TestCheck_AnEncodingContentTypeListsMediaTypes(t *testing.T) {
	enc := func(v string) string { return `{contentType: "` + v + `"}` }
	verify(t, []scenario{
		{"one media type", "3.1.0", oas.Encoding, enc("image/png"), nil},
		{"a range", "3.1.0", oas.Encoding, enc("image/*"), nil},
		{"any", "3.1.0", oas.Encoding, enc("*/*"), nil},
		{"a list with parameters", "3.1.0", oas.Encoding, enc("text/plain; charset=utf-8, application/json"), nil},
		{"an empty element", "3.1.0", oas.Encoding, enc("text/plain,"), nil},
		{"no subtype", "3.1.0", oas.Encoding, enc("text"), []finding{at(badFormat, "/contentType")}},
		{"prose", "3.1.0", oas.Encoding, enc("not a media type"), []finding{at(badFormat, "/contentType")}},
		{"one bad element", "3.1.0", oas.Encoding, enc("image/png, png"), []finding{at(badFormat, "/contentType")}},
	})
}

// TestCheck_CallbackKeysAreRuntimeExpressions pins the key grammar of a
// Callback Object: a runtime expression, or text embedding some in braces, or a
// constant URL, which embeds none (GitHub #621).
func TestCheck_CallbackKeysAreRuntimeExpressions(t *testing.T) {
	cb := func(key string) string { return "{" + key + ": {$ref: '#/components/pathItems/p'}}" }
	good := []string{
		`'$request.body#/url'`, `'{$request.query.queryUrl}'`, `'$url'`, `'$method'`, `'$statusCode'`,
		`'http://notify.example/?id={$request.body#/id}&email={$request.body#/email}'`,
		`'{$response.header.Location}'`, `'{$request.path.id}'`, `'{$request.query.}'`, `'{$request.body}'`,
		`'{$request.body#}'`, `'{$request.body#/a~0b/c~1d}'`, `'https://example.com/callback'`, `notAnExpression`,
		`'{$request.header.X-Id}'`,
	}
	for _, key := range good {
		verify(t, []scenario{{"valid " + key, "3.1.0", oas.Callback, cb(key), nil}})
	}
	bad := []string{
		`'$request.bogus'`, `'{$request.bogus}'`, `'{$request.body#url}'`, `'{$request.body#/a~2}'`, `'{$request.body#/a~}'`,
		`'{$request.header.}'`, `'{$request.header.a b}'`, `'{$request}'`, `'{}'`, `'{$url'`, `'$url}'`, `'a}{$url}'`,
		`'{{$url}}'`, `'{$urls}'`, `'$response.'`, `'a{b'`,
	}
	for _, key := range bad {
		verify(t, []scenario{{"invalid " + key, "3.1.0", oas.Callback, cb(key), []finding{at(badFormat, "/"+path(key))}}})
	}
	verify(t, []scenario{
		{"extensions are not keys", "3.1.0", oas.Callback, `{x-e: 1}`, nil},
		{"an invalid key in 3.0", "3.0.3", oas.Callback, `{"$request.bogus": {}}`, []finding{at(badFormat, "/$request.bogus")}},
	})
}

// path returns the pointer token of a YAML-quoted test key.
func path(quoted string) string {
	var s string
	if err := yaml.Unmarshal([]byte(quoted), &s); err != nil {
		panic(err)
	}
	out := ""
	for _, r := range s {
		switch r {
		case '/':
			out += "~1"
		case '~':
			out += "~0"
		default:
			out += string(r)
		}
	}
	return out
}

// TestCheck_NullIsReadAsAbsentWhereTheFieldIsOptional pins that a null written
// for an optional field is a warning, the field being read as left out, and
// one written for a required field an error, whether every object of its kind
// requires it or a rule requires it of this one, as oauth2 does flows. A null
// where a schema belongs, or for a schema keyword, is an error too: the schema
// lowering does not read it as left out. One for a field whose requirement
// another package reports is that package's missing field, and a rule reading
// the null as a sibling's value reads nothing.
func TestCheck_NullIsReadAsAbsentWhereTheFieldIsOptional(t *testing.T) {
	severity := func(kind oas.Kind, doc string) ir.Severity {
		diags := run(t, "3.1.0", kind, doc)
		require.Len(t, diags, 1, doc)
		assert.Equal(t, mismatch, diags[0].Code)
		return diags[0].Severity
	}
	assert.Equal(t, ir.SeverityWarning, severity(oas.Info, `{title: t, version: v, description: ~}`))
	assert.Equal(t, ir.SeverityWarning, severity(oas.Info, `{title: t, version: v, contact: null}`))
	assert.Equal(t, ir.SeverityWarning, severity(oas.Operation, `{tags: ~}`))
	assert.Equal(t, ir.SeverityError, severity(oas.Info, `{title: ~, version: v}`))
	assert.Equal(t, ir.SeverityError, severity(oas.SecurityScheme, `{type: oauth2, flows: ~}`))
	assert.Equal(t, ir.SeverityWarning, severity(oas.SecurityScheme, `{type: http, scheme: basic, flows: ~}`))
	assert.Equal(t, ir.SeverityError, severity(oas.Schema, `{not: ~}`))
	assert.Equal(t, ir.SeverityError, severity(oas.MediaType, `{schema: ~}`))
	assert.Equal(t, ir.SeverityError, severity(oas.Schema, `{description: ~}`))
	assert.Equal(t, ir.SeverityError, severity(oas.Info, `{title: t, version: v, contact: x}`))
	assert.Equal(t, ir.SeverityError, severity(oas.Info, `{title: t, version: v, contact: []}`))
	for _, d := range run(t, "3.1.0", oas.Info, `{title: t, version: v, description: ~}`) {
		assert.Contains(t, d.Message, "read as the field left out")
	}
	assert.Empty(t, run(t, "3.1.0", oas.SecurityScheme, `{type: http, scheme: ~}`), "auth reports the missing scheme")
	assert.Equal(t, ir.SeverityError, severity(oas.Parameter, `{name: p, in: path, required: ~, schema: {}}`),
		"one finding, not a second that required is not true")
	// 3.2 requires a device flow's tokenUrl; a 3.1 document has no such rule.
	for _, d := range run(t, "3.1.0", oas.OAuthFlows, `{deviceAuthorization: {tokenUrl: ~, scopes: {}}}`) {
		assert.Equal(t, ir.SeverityWarning, d.Severity, "%+v", d)
	}
}

// TestCheck_ComponentNameAndKeyFindingsAreWarnings pins the severity of a key
// outside the pattern the specification gives, whose entry lowers as written.
func TestCheck_ComponentNameAndKeyFindingsAreWarnings(t *testing.T) {
	for _, d := range run(t, "3.1.0", oas.Components, `{schemas: {"a b": {}}}`) {
		assert.Equal(t, ir.SeverityWarning, d.Severity)
	}
	for _, d := range run(t, "3.1.0", oas.Callback, `{"{$bogus}": {}}`) {
		assert.Equal(t, ir.SeverityError, d.Severity, "a malformed expression leaves the key's meaning in doubt")
	}
}

// TestCheck_SeenReportsTheObjectsReadAsTheirKind pins the hook that tells a
// caller which mappings were checked as which kind: an object read, and not a
// Reference Object standing in its place.
func TestCheck_SeenReportsTheObjectsReadAsTheirKind(t *testing.T) {
	var n yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte(
		"parameters: {a: {name: n, in: query, schema: {}}, b: {$ref: '#/parameters/a'}}\nschemas: {S: {type: string}}\n"), &n))
	seen := map[oas.Kind]int{}
	oas.NewChecker().Check(&n, "3.0.3", oas.Options{Start: oas.Components, Seen: func(k oas.Kind, node *yaml.Node) {
		require.NotNil(t, node)
		seen[k]++
	}})
	assert.Equal(t, map[oas.Kind]int{oas.Components: 1, oas.Parameter: 1, oas.Schema: 2}, seen)
}
