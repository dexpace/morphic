// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package specrules_test

import (
	"encoding/json/jsontext"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/specrules"
	"github.com/dexpace/morphic/ir"
)

// TestCheck_ExclusiveFieldsAreNotWrittenTogether pins the exclusion rules at
// each minor that states them.
func TestCheck_ExclusiveFieldsAreNotWrittenTogether(t *testing.T) {
	verify(t, []scenario{
		{"license url and identifier in 3.1", "3.1.0", specrules.License, `{name: n, identifier: i, url: u}`, []finding{findingAtPointer(exclusive, "")}},
		{"license url alone", "3.1.0", specrules.License, `{name: n, url: u}`, nil},
		{"license in 3.0 knows no identifier", "3.0.3", specrules.License, `{name: n, identifier: i, url: u}`,
			[]finding{findingAtPointer(later, "/identifier")}},
		{"example and examples", "3.1.0", specrules.MediaType, `{example: 1, examples: {}}`, []finding{findingAtPointer(exclusive, "")}},
		{"example alone", "3.1.0", specrules.MediaType, `{example: 1}`, nil},
		{"parameter example and examples", "3.0.3", specrules.Parameter, `{name: a, in: query, schema: {}, example: 1, examples: {}}`,
			[]finding{findingAtPointer(exclusive, "")}},
		{"header example and examples", "3.2.0", specrules.Header, `{schema: {}, example: 1, examples: {}}`, []finding{findingAtPointer(exclusive, "")}},
		{"example value and externalValue", "3.0.3", specrules.Example, `{value: 1, externalValue: u}`, []finding{findingAtPointer(exclusive, "")}},
		{"example dataValue and value in 3.2", "3.2.0", specrules.Example, `{dataValue: 1, value: 1}`, []finding{findingAtPointer(exclusive, "")}},
		{"example serializedValue and value in 3.2", "3.2.0", specrules.Example, `{serializedValue: s, value: 1}`, []finding{findingAtPointer(exclusive, "")}},
		{"example serializedValue and externalValue in 3.2", "3.2.0", specrules.Example, `{serializedValue: s, externalValue: u}`,
			[]finding{findingAtPointer(exclusive, "")}},
		{"example dataValue and serializedValue in 3.2", "3.2.0", specrules.Example, `{dataValue: 1, serializedValue: s}`, nil},
		{"example dataValue and value in 3.1", "3.1.0", specrules.Example, `{dataValue: 1, value: 1}`, []finding{findingAtPointer(later, "/dataValue")}},
		{"media type encodings", "3.2.0", specrules.MediaType, `{encoding: {}, prefixEncoding: [], itemEncoding: {}}`,
			[]finding{findingAtPointer(exclusive, ""), findingAtPointer(exclusive, "")}},
		{"media type encoding alone", "3.2.0", specrules.MediaType, `{encoding: {}}`, nil},
		{"nested encodings", "3.2.0", specrules.Encoding, `{encoding: {}, itemEncoding: {}}`, []finding{findingAtPointer(exclusive, "")}},
		{"xml node type and attribute", "3.2.0", specrules.XML, `{nodeType: element, attribute: true}`, []finding{findingAtPointer(exclusive, "")}},
		{"xml node type and wrapped", "3.2.0", specrules.XML, `{nodeType: element, wrapped: true}`, []finding{findingAtPointer(exclusive, "")}},
		{"xml attribute alone", "3.2.0", specrules.XML, `{attribute: true}`, nil},
		{"header schema and content", "3.1.0", specrules.Header, `{schema: {}, content: {a/b: {}}}`, []finding{findingAtPointer(exclusive, "")}},
		{"header with neither", "3.1.0", specrules.Header, `{description: d}`, nil},
	})
}

// TestCheck_ExactlyOneOfTheFieldsIsWritten pins the rules that demand a choice.
func TestCheck_ExactlyOneOfTheFieldsIsWritten(t *testing.T) {
	verify(t, []scenario{
		{"parameter with neither", "3.1.0", specrules.Parameter, `{name: a, in: query}`, []finding{findingAtPointer(required, "")}},
		{"parameter with both", "3.1.0", specrules.Parameter, `{name: a, in: query, schema: {}, content: {a/b: {}}}`, []finding{findingAtPointer(exclusive, "")}},
		{"parameter with a schema", "3.1.0", specrules.Parameter, `{name: a, in: query, schema: {}}`, nil},
		{"parameter with a content", "3.1.0", specrules.Parameter, `{name: a, in: query, content: {a/b: {}}}`, nil},
		{"link with neither", "3.1.0", specrules.Link, `{}`, []finding{findingAtPointer(required, "")}},
		{"link with both", "3.1.0", specrules.Link, `{operationRef: a, operationId: b}`, []finding{findingAtPointer(exclusive, "")}},
		{"link with an operation id", "3.1.0", specrules.Link, `{operationId: b}`, nil},
	})
}

// TestCheck_AtLeastOneSectionStandsInADocument pins the rule on the root.
func TestCheck_AtLeastOneSectionStandsInADocument(t *testing.T) {
	const head = "openapi: %s\ninfo: {title: t, version: v}\n"
	verify(t, []scenario{
		{"3.0 needs paths", "3.0.3", specrules.Document, "openapi: 3.0.3\ninfo: {title: t, version: v}\ncomponents: {}\n",
			[]finding{findingAtPointer(required, "")}},
		{"3.0 with paths", "3.0.3", specrules.Document, "openapi: 3.0.3\ninfo: {title: t, version: v}\npaths: {}\n", nil},
		{"3.1 with nothing", "3.1.0", specrules.Document, "openapi: 3.1.0\ninfo: {title: t, version: v}\n", []finding{findingAtPointer(required, "")}},
		{"3.1 with components", "3.1.0", specrules.Document, "openapi: 3.1.0\ninfo: {title: t, version: v}\ncomponents: {}\n", nil},
		{"3.1 with webhooks", "3.1.0", specrules.Document, "openapi: 3.1.0\ninfo: {title: t, version: v}\nwebhooks: {}\n", nil},
		{"3.2 with nothing", "3.2.0", specrules.Document, "openapi: 3.2.0\ninfo: {title: t, version: v}\n", []finding{findingAtPointer(required, "")}},
		{"3.2 with paths", "3.2.0", specrules.Document, "openapi: 3.2.0\ninfo: {title: t, version: v}\npaths: {}\n", nil},
		{"the root lacks its basics", "3.1.0", specrules.Document, "components: {}\n", []finding{findingAtPointer(required, ""), findingAtPointer(required, "")}},
	})
	_ = head
}

// TestCheck_CountsBoundTheEntries pins the rules that count a map or list. A
// map's null entry is none, and a list's is one, read as the empty schema.
func TestCheck_CountsBoundTheEntries(t *testing.T) {
	verify(t, []scenario{
		{"no responses", "3.1.0", specrules.Responses, `{}`, []finding{findingAtPointer(entries, "")}},
		{"extensions are not responses", "3.1.0", specrules.Responses, `{x-a: 1}`, []finding{findingAtPointer(entries, "")}},
		{"one response", "3.1.0", specrules.Responses, `{default: {description: d}}`, nil},
		{"a null response is none", "3.1.0", specrules.Responses, `{'200': ~}`, []finding{findingAtPointer(entries, ""), findingAtPointer(mismatch, "/200")}},
		{"a null content is none", "3.1.0", specrules.Parameter, `{name: a, in: query, content: {a/b: ~}}`,
			[]finding{findingAtPointer(entries, "/content"), findingAtPointer(mismatch, "/content/a~1b")}},
		{"two contents", "3.1.0", specrules.Parameter, `{name: a, in: query, content: {a/b: {}, c/d: {}}}`, []finding{findingAtPointer(entries, "/content")}},
		{"one content", "3.1.0", specrules.Parameter, `{name: a, in: query, content: {a/b: {}}}`, nil},
		{"empty content", "3.1.0", specrules.Parameter, `{name: a, in: query, content: {}}`, []finding{findingAtPointer(entries, "/content")}},
		{"header two contents", "3.1.0", specrules.Header, `{content: {a/b: {}, c/d: {}}}`, []finding{findingAtPointer(entries, "/content")}},
		{"empty enum in 3.0 is a warning", "3.0.3", specrules.ServerVariable, `{default: a, enum: []}`, []finding{findingAtPointer(entries, "/enum")}},
		{"empty enum in 3.1", "3.1.0", specrules.ServerVariable, `{default: a, enum: []}`, []finding{findingAtPointer(entries, "/enum")}},
		{"enum that is no list", "3.1.0", specrules.ServerVariable, `{default: a, enum: x}`, []finding{findingAtPointer(mismatch, "/enum")}},
		{"enum with entries", "3.1.0", specrules.ServerVariable, `{default: a, enum: [a]}`, nil},
		{"a null schema entry is one", "3.1.0", specrules.Schema, `{prefixItems: [~]}`, []finding{findingAtPointer(mismatch, "/prefixItems/0")}},
		{"a null branch is one", "3.0.3", specrules.Schema, `{oneOf: [~]}`, []finding{findingAtPointer(mismatch, "/oneOf/0")}},
		{"request body content is the load package's", "3.1.0", specrules.RequestBody, `{content: {}}`, nil},
	})
}

// TestCheck_RulesGradeByLevel pins that a SHOULD is a warning and a MUST an
// error.
func TestCheck_RulesGradeByLevel(t *testing.T) {
	sev := func(version, doc string) string {
		diags := run(t, version, specrules.ServerVariable, doc)
		if len(diags) != 1 {
			return "?"
		}
		return string(diags[0].Severity)
	}
	if got := sev("3.0.3", `{default: a, enum: []}`); got != "warning" {
		t.Errorf("3.0 SHOULD NOT: severity %q", got)
	}
	if got := sev("3.1.0", `{default: a, enum: []}`); got != "error" {
		t.Errorf("3.1 MUST NOT: severity %q", got)
	}
}

// TestCheck_NestedEncodingsAreWarnedOfByAnalogy pins that the Media Type
// Object's exclusion is a MUST NOT and the Encoding Object's, which the
// specification words only by reference to it, a warning.
func TestCheck_NestedEncodingsAreWarnedOfByAnalogy(t *testing.T) {
	severity := func(kind specrules.Kind, doc string) ir.Severity {
		diags := run(t, "3.2.0", kind, doc)
		require.Len(t, diags, 1)
		return diags[0].Severity
	}
	assert.Equal(t, ir.SeverityError, severity(specrules.MediaType, `{encoding: {}, itemEncoding: {}}`))
	assert.Equal(t, ir.SeverityWarning, severity(specrules.Encoding, `{encoding: {}, itemEncoding: {}}`))
}

// TestCheck_DefaultsMustBeAmongTheEnum pins the member rule.
func TestCheck_DefaultsMustBeAmongTheEnum(t *testing.T) {
	verify(t, []scenario{
		{"default in enum", "3.1.0", specrules.ServerVariable, `{default: a, enum: [a, b]}`, nil},
		{"default outside the enum", "3.1.0", specrules.ServerVariable, `{default: c, enum: [a, b]}`, []finding{findingAtPointer(allowed, "/default")}},
		{"default outside the enum in 3.0", "3.0.3", specrules.ServerVariable, `{default: c, enum: [a, b]}`, []finding{findingAtPointer(allowed, "/default")}},
		{"no enum", "3.1.0", specrules.ServerVariable, `{default: c}`, nil},
		{"empty enum is counted, not compared", "3.1.0", specrules.ServerVariable, `{default: c, enum: []}`, []finding{findingAtPointer(entries, "/enum")}},
		{"enum of aliases", "3.1.0", specrules.ServerVariable, "{default: &d a, enum: [*d]}", nil},
		{"default that is no scalar", "3.1.0", specrules.ServerVariable, `{default: [a], enum: [a]}`, []finding{findingAtPointer(mismatch, "/default")}},
	})
}

// TestCheck_PathParametersAreRequired pins the conditional rules.
func TestCheck_PathParametersAreRequired(t *testing.T) {
	verify(t, []scenario{
		{"path without required", "3.1.0", specrules.Parameter, `{name: a, in: path, schema: {}}`, []finding{findingAtPointer(required, "")}},
		{"path with required false", "3.1.0", specrules.Parameter, `{name: a, in: path, required: false, schema: {}}`, []finding{findingAtPointer(allowed, "/required")}},
		{"path with required true", "3.1.0", specrules.Parameter, `{name: a, in: path, required: true, schema: {}}`, nil},
		{"path with required True", "3.1.0", specrules.Parameter, `{name: a, in: path, required: True, schema: {}}`, nil},
		{"query without required", "3.1.0", specrules.Parameter, `{name: a, in: query, schema: {}}`, nil},
		{"query with required false", "3.1.0", specrules.Parameter, `{name: a, in: query, required: false, schema: {}}`, nil},
		{"unknown location asks nothing", "3.1.0", specrules.Parameter, `{name: a, schema: {}}`, []finding{findingAtPointer(required, "")}},
	})
}

// TestCheck_SecuritySchemesNeedWhatTheirTypeNeeds pins the per-type fields.
func TestCheck_SecuritySchemesNeedWhatTheirTypeNeeds(t *testing.T) {
	verify(t, []scenario{
		{"api key", "3.1.0", specrules.SecurityScheme, `{type: apiKey}`, []finding{findingAtPointer(required, ""), findingAtPointer(required, "")}},
		{"api key complete", "3.1.0", specrules.SecurityScheme, `{type: apiKey, name: n, in: header}`, nil},
		{"http scheme is the auth package's", "3.1.0", specrules.SecurityScheme, `{type: http}`, nil},
		{"oauth2 flows", "3.1.0", specrules.SecurityScheme, `{type: oauth2}`, []finding{findingAtPointer(required, "")}},
		{"openid url", "3.1.0", specrules.SecurityScheme, `{type: openIdConnect}`, []finding{findingAtPointer(required, "")}},
		{"openid complete", "3.1.0", specrules.SecurityScheme, `{type: openIdConnect, openIdConnectUrl: u}`, nil},
		{"mutual tls needs nothing", "3.1.0", specrules.SecurityScheme, `{type: mutualTLS}`, nil},
		{"a type not written", "3.1.0", specrules.SecurityScheme, `{name: n}`, nil},
	})
}

// TestCheck_OAuthFlowsNeedTheirUrls pins that what a flow requires depends on
// which flow it is.
func TestCheck_OAuthFlowsNeedTheirUrls(t *testing.T) {
	flows := func(body string) string { return "{" + body + "}" }
	verify(t, []scenario{
		{"implicit", "3.1.0", specrules.OAuthFlows, flows(`implicit: {scopes: {}}`), []finding{findingAtPointer(required, "/implicit")}},
		{"implicit complete", "3.1.0", specrules.OAuthFlows, flows(`implicit: {scopes: {}, authorizationUrl: u}`), nil},
		{"password", "3.1.0", specrules.OAuthFlows, flows(`password: {scopes: {}}`), []finding{findingAtPointer(required, "/password")}},
		{"client credentials", "3.1.0", specrules.OAuthFlows, flows(`clientCredentials: {scopes: {}}`), []finding{findingAtPointer(required, "/clientCredentials")}},
		{"authorization code", "3.1.0", specrules.OAuthFlows, flows(`authorizationCode: {scopes: {}}`),
			[]finding{findingAtPointer(required, "/authorizationCode"), findingAtPointer(required, "/authorizationCode")}},
		{"scopes", "3.1.0", specrules.OAuthFlows, flows(`password: {tokenUrl: u}`), []finding{findingAtPointer(required, "/password")}},
		{"device authorization in 3.1", "3.1.0", specrules.OAuthFlows, flows(`deviceAuthorization: {scopes: {}}`),
			[]finding{findingAtPointer(later, "/deviceAuthorization")}},
		{"device authorization in 3.2", "3.2.0", specrules.OAuthFlows, flows(`deviceAuthorization: {scopes: {}}`),
			[]finding{findingAtPointer(required, "/deviceAuthorization"), findingAtPointer(required, "/deviceAuthorization")}},
		{"device authorization complete", "3.2.0", specrules.OAuthFlows,
			flows(`deviceAuthorization: {scopes: {}, tokenUrl: t, deviceAuthorizationUrl: d}`), nil},
		{"password in 3.2", "3.2.0", specrules.OAuthFlows, flows(`password: {scopes: {}}`), []finding{findingAtPointer(required, "/password")}},
		{"a flow read alone does not know its kind", "3.2.0", specrules.OAuthFlow, `{scopes: {}}`, nil},
	})
}

// TestCheck_HeadersLeaveWhatAParameterNamesToTheUnknownKeys pins that the
// table states the Header Object's bans on what a parameter names and reports
// each as an unknown key, once, since a Header Object defines none of those
// keys; a Parameter Object's allowEmptyValue outside the query, a field it does
// define, is a forbidden field.
func TestCheck_HeadersLeaveWhatAParameterNamesToTheUnknownKeys(t *testing.T) {
	verify(t, []scenario{
		{"name and in", "3.1.0", specrules.Header, `{name: n, in: header, schema: {}}`,
			[]finding{findingAtPointer(unknownKey, "/name"), findingAtPointer(unknownKey, "/in")}},
		{"allowEmptyValue and allowReserved in 3.1", "3.1.0", specrules.Header, `{allowEmptyValue: true, allowReserved: true, schema: {}}`,
			[]finding{findingAtPointer(unknownKey, "/allowEmptyValue"), findingAtPointer(unknownKey, "/allowReserved")}},
		{"allowEmptyValue in 3.2", "3.2.0", specrules.Header, `{allowEmptyValue: true, schema: {}}`,
			[]finding{findingAtPointer(unknownKey, "/allowEmptyValue")}},
		{"allowEmptyValue outside the query", "3.1.0", specrules.Parameter, `{name: a, in: header, allowEmptyValue: true, schema: {}}`,
			[]finding{findingAtPointer(forbidden, "/allowEmptyValue")}},
		{"allowEmptyValue in the query", "3.1.0", specrules.Parameter, `{name: a, in: query, allowEmptyValue: true, schema: {}}`, nil},
		{"allowEmptyValue with no location", "3.1.0", specrules.Parameter, `{name: a, allowEmptyValue: true, schema: {}}`, []finding{findingAtPointer(required, "")}},
	})
}

// TestCheck_QuerystringTakesOnlyContent pins the rules on in: querystring: a
// schema there leaves the parameter no content to bind the query string by, an
// error, while a style, explode or allowReserved has nothing to qualify, and
// lowers as declared, a warning in the words the message gives.
func TestCheck_QuerystringTakesOnlyContent(t *testing.T) {
	verify(t, []scenario{
		{"schema", "3.2.0", specrules.Parameter, `{name: a, in: querystring, schema: {}}`, []finding{findingAtPointer(forbidden, "/schema")}},
		{"content alone", "3.2.0", specrules.Parameter, `{name: a, in: querystring, content: {a/b: {}}}`, nil},
		{"style, explode and allowReserved", "3.2.0", specrules.Parameter,
			`{name: a, in: querystring, content: {a/b: {}}, style: form, explode: true, allowReserved: true}`,
			[]finding{findingAtPointer(misplacedField, "/style"), findingAtPointer(misplacedField, "/explode"), findingAtPointer(misplacedField, "/allowReserved")}},
		{"at another location", "3.2.0", specrules.Parameter, `{name: a, in: query, schema: {}, style: form, explode: true}`, nil},
	})
	diags := run(t, "3.2.0", specrules.Parameter, `{name: a, in: querystring, content: {a/b: {}}, explode: true}`)
	require.Len(t, diags, 1)
	assert.Equal(t, ir.SeverityWarning, diags[0].Severity)
	assert.Equal(t, "parameter field explode is not allowed for in=querystring; lowered as declared", diags[0].Message)
}

// TestCheck_AllowReservedWarnsWhereItHasNoEffect pins the minors apart.
func TestCheck_AllowReservedWarnsWhereItHasNoEffect(t *testing.T) {
	param := func(where string) string {
		return "{name: a, in: " + where + ", allowReserved: true, schema: {}, required: true}"
	}
	verify(t, []scenario{
		{"3.0 path", "3.0.3", specrules.Parameter, param("path"), []finding{findingAtPointer(noEffect, "/allowReserved")}},
		{"3.0 header", "3.0.3", specrules.Parameter, param("header"), []finding{findingAtPointer(noEffect, "/allowReserved")}},
		{"3.0 cookie", "3.0.3", specrules.Parameter, param("cookie"), []finding{findingAtPointer(noEffect, "/allowReserved")}},
		{"3.0 query", "3.0.3", specrules.Parameter, param("query"), nil},
		{"3.1 path", "3.1.0", specrules.Parameter, param("path"), []finding{findingAtPointer(noEffect, "/allowReserved")}},
		{"3.2 path", "3.2.0", specrules.Parameter, param("path"), nil},
		{"3.2 header", "3.2.0", specrules.Parameter, param("header"), []finding{findingAtPointer(noEffect, "/allowReserved")}},
		{"3.2 cookie", "3.2.0", specrules.Parameter, param("cookie"), nil},
		{"3.2 cookie style", "3.2.0", specrules.Parameter, "{name: a, in: cookie, style: cookie, allowReserved: true, schema: {}}",
			[]finding{findingAtPointer(noEffect, "/allowReserved")}},
		{"3.2 query", "3.2.0", specrules.Parameter, param("query"), nil},
	})
}

// TestCheck_AllowEmptyValueWarnsUnderAStyleThatCannotWriteIt pins the styles
// whose empty value OpenAPI's table leaves n/a, where it says allowEmptyValue
// SHALL be ignored, at every minor, against the styles that can write one. A
// location other than query draws the forbidden-field error alone, so the two
// rules never report one keyword twice.
func TestCheck_AllowEmptyValueWarnsUnderAStyleThatCannotWriteIt(t *testing.T) {
	param := func(style string) string {
		return "{name: a, in: query, style: " + style + ", allowEmptyValue: true, schema: {}}"
	}
	scenarios := make([]scenario, 0, 15)
	for _, minor := range []string{"3.0.3", "3.1.0", "3.2.0"} {
		for _, style := range []string{"spaceDelimited", "pipeDelimited", "deepObject"} {
			scenarios = append(scenarios, scenario{minor + " " + style, minor, specrules.Parameter, param(style),
				[]finding{findingAtPointer(noEffect, "/allowEmptyValue")}})
		}
		scenarios = append(scenarios,
			scenario{minor + " form", minor, specrules.Parameter, param("form"), nil},
			scenario{minor + " no style", minor, specrules.Parameter,
				"{name: a, in: query, allowEmptyValue: false, schema: {}}", nil})
	}
	verify(t, scenarios)
	verify(t, []scenario{{"a header in deepObject", "3.1.0", specrules.Parameter,
		"{name: a, in: header, style: deepObject, allowEmptyValue: true, schema: {}}",
		[]finding{findingAtPointer(forbidden, "/allowEmptyValue"), findingAtPointer(allowed, "/style")}}})
	diags := run(t, "3.1.0", specrules.Parameter, param("deepObject"))
	require.Len(t, diags, 1)
	assert.Equal(t, ir.SeverityWarning, diags[0].Severity)
	assert.Equal(t, "parameter field allowEmptyValue has no effect for in=query with style=deepObject in "+
		"OpenAPI 3.1, which applies it only under a style that can write an empty value; lowered as declared",
		diags[0].Message)
}

// TestCheck_DuplicatesAreFound pins the uniqueness rules the engine owns.
func TestCheck_DuplicatesAreFound(t *testing.T) {
	const head = "openapi: 3.1.0\ninfo: {title: t, version: v}\ncomponents: {}\n"
	verify(t, []scenario{
		{"tag names", "3.1.0", specrules.Document, head + "tags: [{name: a}, {name: b}, {name: a}]\n", []finding{findingAtPointer(duplicate, "/tags/2/name")}},
		{"distinct tags", "3.1.0", specrules.Document, head + "tags: [{name: a}, {name: b}]\n", nil},
		{"tags without names", "3.1.0", specrules.Document, head + "tags: [{}, {}]\n", []finding{findingAtPointer(required, "/tags/0"), findingAtPointer(required, "/tags/1")}},
		{"tags that are no list", "3.1.0", specrules.Document, head + "tags: {}\n", []finding{findingAtPointer(mismatch, "/tags")}},
		{"names compared as written", "3.1.0", specrules.Document, head + "tags: [{name: a}, {name: A}]\n", nil},
	})
	diags := run(t, "3.1.0", specrules.Document, head+"tags: [{name: a}, {name: b}, {name: a}, {name: a}]\n")
	require.Len(t, diags, 2)
	assert.Equal(t, `OpenAPI Object field "tags" repeats the name "a" first written at /tags/0/name in OpenAPI 3.1 (OpenAPI Object)`,
		diags[1].Message, "each repeat names the first (GitHub #881)")
}

// TestCheck_RulesAnotherPackageOwnsAreNotApplied pins that a finding another
// package reports is not made twice.
func TestCheck_RulesAnotherPackageOwnsAreNotApplied(t *testing.T) {
	const head = "openapi: 3.2.0\ninfo: {title: t, version: v}\n"
	verify(t, []scenario{
		{"repeated parameters", "3.1.0", specrules.Operation, `{responses: {}, parameters: [{name: a, in: query, schema: {}}, {name: a, in: query, schema: {}}]}`,
			[]finding{findingAtPointer(entries, "/responses")}},
		{"repeated server names", "3.2.0", specrules.Document, head + "components: {}\nservers: [{url: a, name: n}, {url: b, name: n}]\n", nil},
		{"operation ids", "3.1.0", specrules.Document, "openapi: 3.1.0\ninfo: {title: t, version: v}\npaths:\n  /a: {get: {operationId: x, responses: {}}, put: {operationId: x, responses: {}}}\n",
			[]finding{findingAtPointer(entries, "/paths/~1a/get/responses"), findingAtPointer(entries, "/paths/~1a/put/responses")}},
		{"path keys", "3.1.0", specrules.Paths, `{a: {}, "/b?x": {}}`, nil},
		{"status keys", "3.1.0", specrules.Responses, `{ninety: {description: d}}`, nil},
		{"3.0 array without items", "3.0.3", specrules.Schema, `{type: array}`, nil},
	})
}

// TestCheck_AllowReservedWordsWhereItAppliesAndWhereItDoesNot pins the words of
// the warning, which name the place the keyword is written at and where the
// declared minor applies it.
func TestCheck_AllowReservedWordsWhereItAppliesAndWhereItDoesNot(t *testing.T) {
	for _, c := range []struct{ version, doc, want string }{
		{"3.1.0", `{name: n, in: path, required: true, allowReserved: true, schema: {}}`,
			"parameter field allowReserved has no effect for in=path in OpenAPI 3.1, which applies it to in=query alone; lowered as declared"},
		{"3.2.0", `{name: n, in: header, allowReserved: true, schema: {}}`,
			"parameter field allowReserved has no effect for in=header in OpenAPI 3.2, which applies it only where the value is percent-encoded; lowered as declared"},
		{"3.2.0", `{name: n, in: cookie, style: cookie, allowReserved: true, schema: {}}`,
			"parameter field allowReserved has no effect for in=cookie with style=cookie in OpenAPI 3.2, which applies it only where the value is percent-encoded; lowered as declared"},
	} {
		diags := run(t, c.version, specrules.Parameter, c.doc)
		require.Len(t, diags, 1, c.doc)
		assert.Equal(t, noEffect, diags[0].Code)
		assert.Equal(t, c.want, diags[0].Message)
	}
}

// TestCheck_KeywordsHoldWhatJSONSchemaRequires pins what JSON Schema requires of
// a keyword's value beyond its shape: a list of schemas holds an entry, the
// names required lists and the types a list gives differ, dependentRequired
// lists strings once each, $vocabulary maps URIs to booleans, and a $id carries
// no fragment but an empty one.
func TestCheck_KeywordsHoldWhatJSONSchemaRequires(t *testing.T) {
	verify(t, []scenario{
		{"empty lists of schemas", "3.1.0", specrules.Schema, `{allOf: [], anyOf: [], oneOf: []}`,
			[]finding{findingAtPointer(entries, "/allOf"), findingAtPointer(entries, "/anyOf"), findingAtPointer(entries, "/oneOf")}},
		{"an empty list of schemas in 3.0", "3.0.3", specrules.Schema, `{allOf: []}`, []finding{findingAtPointer(entries, "/allOf")}},
		{"empty prefixItems and types", "3.1.0", specrules.Schema, `{prefixItems: [], type: []}`,
			[]finding{findingAtPointer(entries, "/prefixItems"), findingAtPointer(entries, "/type")}},
		{"a list of schemas of another shape", "3.1.0", specrules.Schema, `{allOf: {}}`, []finding{findingAtPointer(mismatch, "/allOf")}},
		{"a type that is no list", "3.1.0", specrules.Schema, `{type: string}`, nil},
		{"a required name twice", "3.0.3", specrules.Schema, `{required: [a, b, a]}`, []finding{findingAtPointer(duplicate, "/required/2")}},
		{"a type twice", "3.1.0", specrules.Schema, `{type: [string, string]}`, []finding{findingAtPointer(duplicate, "/type/1")}},
		{"dependentRequired", "3.1.0", specrules.Schema, `{dependentRequired: {a: [b, b], c: x, d: [e]}}`,
			[]finding{findingAtPointer(duplicate, "/dependentRequired/a/1"), findingAtPointer(mismatch, "/dependentRequired/c")}},
		{"vocabulary", "3.1.0", specrules.Schema,
			`{$vocabulary: {"https://e.com/v": true, rel: false, "https://e.com/w": "true"}}`,
			[]finding{findingAtPointer(badFormat, "/$vocabulary/rel"), findingAtPointer(mismatch, "/$vocabulary/https:~1~1e.com~1w")}},
		{"an id with a fragment", "3.1.0", specrules.Schema, `{$id: "https://e.com/a#frag"}`, []finding{findingAtPointer(badFormat, "/$id")}},
		{"an id with an empty fragment", "3.1.0", specrules.Schema, `{$id: "https://e.com/a#"}`, nil},
	})
	diags := run(t, "3.1.0", specrules.Schema, `{required: [a, a]}`)
	require.Len(t, diags, 1)
	assert.Equal(t, `Schema Object field "required" repeats "a" first written at /required/0 in OpenAPI 3.1 (Schema Object)`,
		diags[0].Message)
}

// TestCheck_ARepeatedEntryNamesItsFirstCopyWhereItIsPlaced holds the copy a
// repeated entry's message names to where the Locator places it, and, where
// the Locator places it at no pointer, to where it was read, under Root.
func TestCheck_ARepeatedEntryNamesItsFirstCopyWhereItIsPlaced(t *testing.T) {
	for name, c := range map[string]struct {
		locate specrules.Locator
		want   string
	}{
		"placed": {func(p jsontext.Pointer) ir.Provenance { return ir.Provenance{Pointer: "/placed" + p} },
			"/placed/required/0"},
		"placed at no pointer": {nil, "/x-raw/T/required/0"},
	} {
		t.Run(name, func(t *testing.T) {
			var n yaml.Node
			require.NoError(t, yaml.Unmarshal([]byte(`{required: [a, a]}`), &n))

			diags := specrules.NewChecker().Check(t.Context(), &n, "3.1.0",
				specrules.Options{Start: specrules.Schema, Locate: c.locate, Root: "/x-raw/T"})

			require.Len(t, diags, 1)
			assert.Equal(t, `Schema Object field "required" repeats "a" first written at `+c.want+
				` in OpenAPI 3.1 (Schema Object)`, diags[0].Message)
		})
	}
}

// TestCheck_AFieldBesideContentNamesItsCarrier holds the warning a
// serialization field written beside content draws to the object writing it:
// a header's names a header serialized through its schema, as a parameter's
// names a parameter.
func TestCheck_AFieldBesideContentNamesItsCarrier(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		kind    specrules.Kind
		doc     string
		carrier string
	}{
		{specrules.Parameter, "{name: p, in: query, style: form, content: {application/json: {schema: {}}}}", "parameter"},
		{specrules.Header, "{style: simple, content: {application/json: {schema: {}}}}", "header"},
	} {
		diags := run(t, "3.2.0", tc.kind, tc.doc)
		require.Len(t, diags, 1)
		assert.Equal(t, noEffect, diags[0].Code)
		assert.Contains(t, diags[0].Message, "which applies it to a "+tc.carrier+" serialized through its schema")
	}
}
