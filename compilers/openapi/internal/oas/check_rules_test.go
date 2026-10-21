package oas_test

import (
	"testing"

	"github.com/dexpace/morphic/compilers/openapi/internal/oas"
)

// TestCheck_ExclusiveFieldsAreNotWrittenTogether pins the exclusion rules at
// each minor that states them.
func TestCheck_ExclusiveFieldsAreNotWrittenTogether(t *testing.T) {
	verify(t, []scenario{
		{"license url and identifier in 3.1", "3.1.0", oas.License, `{name: n, identifier: i, url: u}`, []finding{at(exclusive, "")}},
		{"license url alone", "3.1.0", oas.License, `{name: n, url: u}`, nil},
		{"license in 3.0 knows no identifier", "3.0.3", oas.License, `{name: n, identifier: i, url: u}`,
			[]finding{at(later, "/identifier")}},
		{"example and examples", "3.1.0", oas.MediaType, `{example: 1, examples: {}}`, []finding{at(exclusive, "")}},
		{"example alone", "3.1.0", oas.MediaType, `{example: 1}`, nil},
		{"parameter example and examples", "3.0.3", oas.Parameter, `{name: a, in: query, schema: {}, example: 1, examples: {}}`,
			[]finding{at(exclusive, "")}},
		{"header example and examples", "3.2.0", oas.Header, `{schema: {}, example: 1, examples: {}}`, []finding{at(exclusive, "")}},
		{"example value and externalValue", "3.0.3", oas.Example, `{value: 1, externalValue: u}`, []finding{at(exclusive, "")}},
		{"example dataValue and value in 3.2", "3.2.0", oas.Example, `{dataValue: 1, value: 1}`, []finding{at(exclusive, "")}},
		{"example serializedValue and value in 3.2", "3.2.0", oas.Example, `{serializedValue: s, value: 1}`, []finding{at(exclusive, "")}},
		{"example serializedValue and externalValue in 3.2", "3.2.0", oas.Example, `{serializedValue: s, externalValue: u}`,
			[]finding{at(exclusive, "")}},
		{"example dataValue and serializedValue in 3.2", "3.2.0", oas.Example, `{dataValue: 1, serializedValue: s}`, nil},
		{"example dataValue and value in 3.1", "3.1.0", oas.Example, `{dataValue: 1, value: 1}`, []finding{at(later, "/dataValue")}},
		{"media type encodings", "3.2.0", oas.MediaType, `{encoding: {}, prefixEncoding: [], itemEncoding: {}}`,
			[]finding{at(exclusive, ""), at(exclusive, "")}},
		{"media type encoding alone", "3.2.0", oas.MediaType, `{encoding: {}}`, nil},
		{"nested encodings", "3.2.0", oas.Encoding, `{encoding: {}, itemEncoding: {}}`, []finding{at(exclusive, "")}},
		{"xml node type and attribute", "3.2.0", oas.XML, `{nodeType: element, attribute: true}`, []finding{at(exclusive, "")}},
		{"xml node type and wrapped", "3.2.0", oas.XML, `{nodeType: element, wrapped: true}`, []finding{at(exclusive, "")}},
		{"xml attribute alone", "3.2.0", oas.XML, `{attribute: true}`, nil},
		{"header schema and content", "3.1.0", oas.Header, `{schema: {}, content: {a/b: {}}}`, []finding{at(exclusive, "")}},
		{"header with neither", "3.1.0", oas.Header, `{description: d}`, nil},
	})
}

// TestCheck_ExactlyOneOfTheFieldsIsWritten pins the rules that demand a choice.
func TestCheck_ExactlyOneOfTheFieldsIsWritten(t *testing.T) {
	verify(t, []scenario{
		{"parameter with neither", "3.1.0", oas.Parameter, `{name: a, in: query}`, []finding{at(required, "")}},
		{"parameter with both", "3.1.0", oas.Parameter, `{name: a, in: query, schema: {}, content: {a/b: {}}}`, []finding{at(exclusive, "")}},
		{"parameter with a schema", "3.1.0", oas.Parameter, `{name: a, in: query, schema: {}}`, nil},
		{"parameter with a content", "3.1.0", oas.Parameter, `{name: a, in: query, content: {a/b: {}}}`, nil},
		{"link with neither", "3.1.0", oas.Link, `{}`, []finding{at(required, "")}},
		{"link with both", "3.1.0", oas.Link, `{operationRef: a, operationId: b}`, []finding{at(exclusive, "")}},
		{"link with an operation id", "3.1.0", oas.Link, `{operationId: b}`, nil},
	})
}

// TestCheck_AtLeastOneSectionStandsInADocument pins the rule on the root.
func TestCheck_AtLeastOneSectionStandsInADocument(t *testing.T) {
	const head = "openapi: %s\ninfo: {title: t, version: v}\n"
	verify(t, []scenario{
		{"3.0 needs paths", "3.0.3", oas.Document, "openapi: 3.0.3\ninfo: {title: t, version: v}\ncomponents: {}\n",
			[]finding{at(required, "")}},
		{"3.0 with paths", "3.0.3", oas.Document, "openapi: 3.0.3\ninfo: {title: t, version: v}\npaths: {}\n", nil},
		{"3.1 with nothing", "3.1.0", oas.Document, "openapi: 3.1.0\ninfo: {title: t, version: v}\n", []finding{at(required, "")}},
		{"3.1 with components", "3.1.0", oas.Document, "openapi: 3.1.0\ninfo: {title: t, version: v}\ncomponents: {}\n", nil},
		{"3.1 with webhooks", "3.1.0", oas.Document, "openapi: 3.1.0\ninfo: {title: t, version: v}\nwebhooks: {}\n", nil},
		{"3.2 with nothing", "3.2.0", oas.Document, "openapi: 3.2.0\ninfo: {title: t, version: v}\n", []finding{at(required, "")}},
		{"3.2 with paths", "3.2.0", oas.Document, "openapi: 3.2.0\ninfo: {title: t, version: v}\npaths: {}\n", nil},
		{"the root lacks its basics", "3.1.0", oas.Document, "components: {}\n", []finding{at(required, ""), at(required, "")}},
	})
	_ = head
}

// TestCheck_CountsBoundTheEntries pins the rules that count a map or list.
func TestCheck_CountsBoundTheEntries(t *testing.T) {
	verify(t, []scenario{
		{"no responses", "3.1.0", oas.Responses, `{}`, []finding{at(entries, "")}},
		{"extensions are not responses", "3.1.0", oas.Responses, `{x-a: 1}`, []finding{at(entries, "")}},
		{"one response", "3.1.0", oas.Responses, `{default: {description: d}}`, nil},
		{"two contents", "3.1.0", oas.Parameter, `{name: a, in: query, content: {a/b: {}, c/d: {}}}`, []finding{at(entries, "/content")}},
		{"one content", "3.1.0", oas.Parameter, `{name: a, in: query, content: {a/b: {}}}`, nil},
		{"empty content", "3.1.0", oas.Parameter, `{name: a, in: query, content: {}}`, []finding{at(entries, "/content")}},
		{"header two contents", "3.1.0", oas.Header, `{content: {a/b: {}, c/d: {}}}`, []finding{at(entries, "/content")}},
		{"empty enum in 3.0 is a warning", "3.0.3", oas.ServerVariable, `{default: a, enum: []}`, []finding{at(entries, "/enum")}},
		{"empty enum in 3.1", "3.1.0", oas.ServerVariable, `{default: a, enum: []}`, []finding{at(entries, "/enum")}},
		{"enum that is no list", "3.1.0", oas.ServerVariable, `{default: a, enum: x}`, []finding{at(mismatch, "/enum")}},
		{"enum with entries", "3.1.0", oas.ServerVariable, `{default: a, enum: [a]}`, nil},
		{"request body content is the load package's", "3.1.0", oas.RequestBody, `{content: {}}`, nil},
	})
}

// TestCheck_RulesGradeByLevel pins that a SHOULD is a warning and a MUST an
// error.
func TestCheck_RulesGradeByLevel(t *testing.T) {
	sev := func(version, doc string) string {
		diags := run(t, version, oas.ServerVariable, doc)
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

// TestCheck_DefaultsMustBeAmongTheEnum pins the member rule.
func TestCheck_DefaultsMustBeAmongTheEnum(t *testing.T) {
	verify(t, []scenario{
		{"default in enum", "3.1.0", oas.ServerVariable, `{default: a, enum: [a, b]}`, nil},
		{"default outside the enum", "3.1.0", oas.ServerVariable, `{default: c, enum: [a, b]}`, []finding{at(allowed, "/default")}},
		{"default outside the enum in 3.0", "3.0.3", oas.ServerVariable, `{default: c, enum: [a, b]}`, []finding{at(allowed, "/default")}},
		{"no enum", "3.1.0", oas.ServerVariable, `{default: c}`, nil},
		{"empty enum is counted, not compared", "3.1.0", oas.ServerVariable, `{default: c, enum: []}`, []finding{at(entries, "/enum")}},
		{"enum of aliases", "3.1.0", oas.ServerVariable, "{default: &d a, enum: [*d]}", nil},
		{"default that is no scalar", "3.1.0", oas.ServerVariable, `{default: [a], enum: [a]}`, []finding{at(mismatch, "/default")}},
	})
}

// TestCheck_PathParametersAreRequired pins the conditional rules.
func TestCheck_PathParametersAreRequired(t *testing.T) {
	verify(t, []scenario{
		{"path without required", "3.1.0", oas.Parameter, `{name: a, in: path, schema: {}}`, []finding{at(required, "")}},
		{"path with required false", "3.1.0", oas.Parameter, `{name: a, in: path, required: false, schema: {}}`, []finding{at(allowed, "/required")}},
		{"path with required true", "3.1.0", oas.Parameter, `{name: a, in: path, required: true, schema: {}}`, nil},
		{"path with required True", "3.1.0", oas.Parameter, `{name: a, in: path, required: True, schema: {}}`, nil},
		{"query without required", "3.1.0", oas.Parameter, `{name: a, in: query, schema: {}}`, nil},
		{"query with required false", "3.1.0", oas.Parameter, `{name: a, in: query, required: false, schema: {}}`, nil},
		{"unknown location asks nothing", "3.1.0", oas.Parameter, `{name: a, schema: {}}`, []finding{at(required, "")}},
	})
}

// TestCheck_SecuritySchemesNeedWhatTheirTypeNeeds pins the per-type fields.
func TestCheck_SecuritySchemesNeedWhatTheirTypeNeeds(t *testing.T) {
	verify(t, []scenario{
		{"api key", "3.1.0", oas.SecurityScheme, `{type: apiKey}`, []finding{at(required, ""), at(required, "")}},
		{"api key complete", "3.1.0", oas.SecurityScheme, `{type: apiKey, name: n, in: header}`, nil},
		{"http scheme is the auth package's", "3.1.0", oas.SecurityScheme, `{type: http}`, nil},
		{"oauth2 flows", "3.1.0", oas.SecurityScheme, `{type: oauth2}`, []finding{at(required, "")}},
		{"openid url", "3.1.0", oas.SecurityScheme, `{type: openIdConnect}`, []finding{at(required, "")}},
		{"openid complete", "3.1.0", oas.SecurityScheme, `{type: openIdConnect, openIdConnectUrl: u}`, nil},
		{"mutual tls needs nothing", "3.1.0", oas.SecurityScheme, `{type: mutualTLS}`, nil},
		{"a type not written", "3.1.0", oas.SecurityScheme, `{name: n}`, nil},
	})
}

// TestCheck_OAuthFlowsNeedTheirUrls pins that what a flow requires depends on
// which flow it is.
func TestCheck_OAuthFlowsNeedTheirUrls(t *testing.T) {
	flows := func(body string) string { return "{" + body + "}" }
	verify(t, []scenario{
		{"implicit", "3.1.0", oas.OAuthFlows, flows(`implicit: {scopes: {}}`), []finding{at(required, "/implicit")}},
		{"implicit complete", "3.1.0", oas.OAuthFlows, flows(`implicit: {scopes: {}, authorizationUrl: u}`), nil},
		{"password", "3.1.0", oas.OAuthFlows, flows(`password: {scopes: {}}`), []finding{at(required, "/password")}},
		{"client credentials", "3.1.0", oas.OAuthFlows, flows(`clientCredentials: {scopes: {}}`), []finding{at(required, "/clientCredentials")}},
		{"authorization code", "3.1.0", oas.OAuthFlows, flows(`authorizationCode: {scopes: {}}`),
			[]finding{at(required, "/authorizationCode"), at(required, "/authorizationCode")}},
		{"scopes", "3.1.0", oas.OAuthFlows, flows(`password: {tokenUrl: u}`), []finding{at(required, "/password")}},
		{"device authorization in 3.1", "3.1.0", oas.OAuthFlows, flows(`deviceAuthorization: {scopes: {}}`),
			[]finding{at(later, "/deviceAuthorization")}},
		{"device authorization in 3.2", "3.2.0", oas.OAuthFlows, flows(`deviceAuthorization: {scopes: {}}`),
			[]finding{at(required, "/deviceAuthorization"), at(required, "/deviceAuthorization")}},
		{"device authorization complete", "3.2.0", oas.OAuthFlows,
			flows(`deviceAuthorization: {scopes: {}, tokenUrl: t, deviceAuthorizationUrl: d}`), nil},
		{"password in 3.2", "3.2.0", oas.OAuthFlows, flows(`password: {scopes: {}}`), []finding{at(required, "/password")}},
		{"a flow read alone does not know its kind", "3.2.0", oas.OAuthFlow, `{scopes: {}}`, nil},
	})
}

// TestCheck_HeadersRefuseWhatAParameterNames pins the bans on the Header Object.
func TestCheck_HeadersRefuseWhatAParameterNames(t *testing.T) {
	verify(t, []scenario{
		{"name and in", "3.1.0", oas.Header, `{name: n, in: header, schema: {}}`, []finding{at(forbidden, "/name"), at(forbidden, "/in")}},
		{"allowEmptyValue in 3.1", "3.1.0", oas.Header, `{allowEmptyValue: true, schema: {}}`, []finding{at(forbidden, "/allowEmptyValue")}},
		{"allowReserved in 3.1", "3.1.0", oas.Header, `{allowReserved: true, schema: {}}`, []finding{at(forbidden, "/allowReserved")}},
		{"allowEmptyValue in 3.2", "3.2.0", oas.Header, `{allowEmptyValue: true, schema: {}}`, []finding{at(forbidden, "/allowEmptyValue")}},
		{"allowReserved in 3.2 is not banned", "3.2.0", oas.Header, `{allowReserved: true, schema: {}}`, nil},
		{"allowEmptyValue outside the query", "3.1.0", oas.Parameter, `{name: a, in: header, allowEmptyValue: true, schema: {}}`,
			[]finding{at(forbidden, "/allowEmptyValue")}},
		{"allowEmptyValue in the query", "3.1.0", oas.Parameter, `{name: a, in: query, allowEmptyValue: true, schema: {}}`, nil},
		{"allowEmptyValue with no location", "3.1.0", oas.Parameter, `{name: a, allowEmptyValue: true, schema: {}}`, []finding{at(required, "")}},
	})
}

// TestCheck_QuerystringTakesOnlyContent pins the rules on in: querystring.
func TestCheck_QuerystringTakesOnlyContent(t *testing.T) {
	verify(t, []scenario{
		{"style and schema", "3.2.0", oas.Parameter, `{name: a, in: querystring, style: form, schema: {}}`,
			[]finding{at(forbidden, "/style"), at(forbidden, "/schema")}},
		{"content alone", "3.2.0", oas.Parameter, `{name: a, in: querystring, content: {a/b: {}}}`, nil},
		{"explode and allowReserved are the operation package's", "3.2.0", oas.Parameter,
			`{name: a, in: querystring, content: {a/b: {}}, explode: true, allowReserved: true}`, nil},
	})
}

// TestCheck_AllowReservedWarnsWhereItHasNoEffect pins the minors apart.
func TestCheck_AllowReservedWarnsWhereItHasNoEffect(t *testing.T) {
	param := func(where string) string {
		return "{name: a, in: " + where + ", allowReserved: true, schema: {}, required: true}"
	}
	verify(t, []scenario{
		{"3.0 path", "3.0.3", oas.Parameter, param("path"), []finding{at(noEffect, "/allowReserved")}},
		{"3.0 header", "3.0.3", oas.Parameter, param("header"), []finding{at(noEffect, "/allowReserved")}},
		{"3.0 cookie", "3.0.3", oas.Parameter, param("cookie"), []finding{at(noEffect, "/allowReserved")}},
		{"3.0 query", "3.0.3", oas.Parameter, param("query"), nil},
		{"3.1 path", "3.1.0", oas.Parameter, param("path"), []finding{at(noEffect, "/allowReserved")}},
		{"3.2 path", "3.2.0", oas.Parameter, param("path"), nil},
		{"3.2 header", "3.2.0", oas.Parameter, param("header"), []finding{at(noEffect, "/allowReserved")}},
		{"3.2 cookie", "3.2.0", oas.Parameter, param("cookie"), nil},
		{"3.2 cookie style", "3.2.0", oas.Parameter, "{name: a, in: cookie, style: cookie, allowReserved: true, schema: {}}",
			[]finding{at(noEffect, "/allowReserved")}},
		{"3.2 query", "3.2.0", oas.Parameter, param("query"), nil},
	})
}

// TestCheck_DuplicatesAreFound pins the uniqueness rules the engine owns.
func TestCheck_DuplicatesAreFound(t *testing.T) {
	const head = "openapi: 3.1.0\ninfo: {title: t, version: v}\ncomponents: {}\n"
	verify(t, []scenario{
		{"tag names", "3.1.0", oas.Document, head + "tags: [{name: a}, {name: b}, {name: a}]\n", []finding{at(duplicate, "/tags/2/name")}},
		{"distinct tags", "3.1.0", oas.Document, head + "tags: [{name: a}, {name: b}]\n", nil},
		{"tags without names", "3.1.0", oas.Document, head + "tags: [{}, {}]\n", []finding{at(required, "/tags/0"), at(required, "/tags/1")}},
		{"tags that are no list", "3.1.0", oas.Document, head + "tags: {}\n", []finding{at(mismatch, "/tags")}},
	})
}

// TestCheck_RulesAnotherPackageOwnsAreNotApplied pins that a finding another
// package reports is not made twice.
func TestCheck_RulesAnotherPackageOwnsAreNotApplied(t *testing.T) {
	const head = "openapi: 3.2.0\ninfo: {title: t, version: v}\n"
	verify(t, []scenario{
		{"repeated parameters", "3.1.0", oas.Operation, `{responses: {}, parameters: [{name: a, in: query, schema: {}}, {name: a, in: query, schema: {}}]}`,
			[]finding{at(entries, "/responses")}},
		{"repeated server names", "3.2.0", oas.Document, head + "components: {}\nservers: [{url: a, name: n}, {url: b, name: n}]\n", nil},
		{"operation ids", "3.1.0", oas.Document, "openapi: 3.1.0\ninfo: {title: t, version: v}\npaths:\n  /a: {get: {operationId: x, responses: {}}, put: {operationId: x, responses: {}}}\n",
			[]finding{at(entries, "/paths/~1a/get/responses"), at(entries, "/paths/~1a/put/responses")}},
		{"path keys", "3.1.0", oas.Paths, `{a: {}, "/b?x": {}}`, nil},
		{"status keys", "3.1.0", oas.Responses, `{ninety: {description: d}}`, nil},
		{"3.0 array without items", "3.0.3", oas.Schema, `{type: array}`, nil},
	})
}
