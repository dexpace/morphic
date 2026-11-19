// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package specrules_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/specrules"
	"github.com/dexpace/morphic/ir"
)

// The codes the table draws for a key no object defines and for a name
// OpenAPI says SHALL be ignored.
const (
	unknownKey     = diag.UnknownObjectKey
	reservedHeader = diag.ReservedHeaderName
)

// TestCheck_ReservedHeaderParameter pins where the table reports a header
// parameter named Accept, Content-Type or Authorization: at its name, once,
// whether it is written in an operation, in a component nothing references, or
// in a component an operation uses. Names compare without regard to ASCII case,
// and a query parameter of the same name is no header.
func TestCheck_ReservedHeaderParameter(t *testing.T) {
	const head = "{openapi: 3.1.0, info: {title: t, version: '1'}, "
	verify(t, []scenario{
		{"unreferenced component", "3.1.0", specrules.Document,
			head + "components: {parameters: {U: {name: authorization, in: header, schema: {type: string}}}}}",
			[]finding{findingAtPointer(reservedHeader, "/components/parameters/U/name")}},
		{"a component an operation uses", "3.1.0", specrules.Document,
			head + "paths: {/a: {get: {parameters: [{$ref: '#/components/parameters/U'}], responses: {'200': {description: ok}}}}}, " +
				"components: {parameters: {U: {name: authorization, in: header, schema: {type: string}}}}}",
			[]finding{findingAtPointer(reservedHeader, "/components/parameters/U/name")}},
		{"an inline parameter", "3.1.0", specrules.Document,
			head + "paths: {/a: {get: {parameters: [{name: ACCEPT, in: header, schema: {type: string}}], " +
				"responses: {'200': {description: ok}}}}}}",
			[]finding{findingAtPointer(reservedHeader, "/paths/~1a/get/parameters/0/name")}},
		{"each of the three names, in any case", "3.0.3", specrules.Document,
			"{openapi: 3.0.3, info: {title: t, version: '1'}, paths: {/a: {get: {parameters: [" +
				"{name: Accept, in: header, schema: {type: string}}, " +
				"{name: content-type, in: header, schema: {type: string}}, " +
				"{name: AUTHORIZATION, in: header, schema: {type: string}}], responses: {'200': {description: ok}}}}}}",
			[]finding{
				findingAtPointer(reservedHeader, "/paths/~1a/get/parameters/0/name"),
				findingAtPointer(reservedHeader, "/paths/~1a/get/parameters/1/name"),
				findingAtPointer(reservedHeader, "/paths/~1a/get/parameters/2/name"),
			}},
		{"an ordinary header name", "3.1.0", specrules.Parameter, `{name: X-Trace, in: header, schema: {type: string}}`, nil},
		{"a name that only contains a reserved one", "3.1.0", specrules.Parameter,
			`{name: Accept-Language, in: header, schema: {type: string}}`, nil},
		{"the same name in a query", "3.1.0", specrules.Parameter, `{name: Accept, in: query, schema: {type: string}}`, nil},
		{"the same name in a cookie", "3.1.0", specrules.Parameter, `{name: Authorization, in: cookie, schema: {type: string}}`, nil},
		{"a header parameter on its own", "3.1.0", specrules.Parameter,
			`{name: Authorization, in: header, schema: {type: string}}`,
			[]finding{findingAtPointer(reservedHeader, "/name")}},
	})
}

// TestCheck_ReservedHeaderEntries pins the headers map half of the rule: a
// Content-Type entry of a Response Object's or an Encoding Object's headers, in
// any case, is reported at the entry, and no other name is, Accept included.
func TestCheck_ReservedHeaderEntries(t *testing.T) {
	verify(t, []scenario{
		{"a response's Content-Type", "3.1.0", specrules.Response,
			`{description: ok, headers: {Content-Type: {schema: {type: string}}}}`,
			[]finding{findingAtPointer(reservedHeader, "/headers/Content-Type")}},
		{"a response's lower-case spelling", "3.0.3", specrules.Response,
			`{description: ok, headers: {content-type: {schema: {type: string}}}}`,
			[]finding{findingAtPointer(reservedHeader, "/headers/content-type")}},
		{"an encoding's Content-Type", "3.2.0", specrules.Encoding,
			`{headers: {CONTENT-TYPE: {schema: {type: string}}, X-Part: {schema: {type: string}}}}`,
			[]finding{findingAtPointer(reservedHeader, "/headers/CONTENT-TYPE")}},
		{"Accept and ordinary names are not reserved in a map", "3.1.0", specrules.Response,
			`{description: ok, headers: {Accept: {schema: {type: string}}, X-Other: {schema: {type: string}}}}`, nil},
	})
}

// besideRefDocument writes a key beside the $ref of a Reference Object at every
// position one is read, and a path item beside its own $ref, which gives the
// path item's fields their meaning.
const besideRefDocument = `
openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /a:
    post:
      operationId: a
      parameters:
        - {$ref: '#/components/parameters/P', x-note: 'kept?', required: true, summary: use-summary}
        - {$ref: '#/components/parameters/Alias'}
      requestBody: {$ref: '#/components/requestBodies/B', x-note: 'kept?'}
      responses:
        "200": {$ref: '#/components/responses/R', x-note: 'kept?', headers: {X-A: {schema: {type: string}}}}
      callbacks:
        cb: {$ref: '#/components/callbacks/C', description: cbd, summary: cbs, x-cb: 1}
  /b:
    get:
      operationId: b
      parameters: [{$ref: '#/components/parameters/Alias', x-alias: 0}]
      responses: {"200": {description: ok}}
components:
  parameters:
    P: {name: p, in: query, schema: {type: string}}
    Q: {name: q, in: query, schema: {type: string}}
    Alias: {$ref: '#/components/parameters/Q', x-alias: 1, summary: s}
  requestBodies:
    B: {content: {application/json: {schema: {type: string}}}}
  responses:
    R: {description: ok}
  callbacks:
    C:
      '{$request.body#/u}':
        post: {responses: {"200": {description: ok}}}
`

// TestCheck_KeysBesideARef pins the Reference Object frame: a key written beside
// a $ref is judged against $ref, summary and description, the three fields a
// Reference Object defines, so an x- extension, a field of the referenced kind
// and any other key are each reported once at the key, while summary and
// description are not, anywhere.
func TestCheck_KeysBesideARef(t *testing.T) {
	verify(t, []scenario{
		{"every position a Reference Object is read", "3.1.0", specrules.Document, besideRefDocument, []finding{
			findingAtPointer(unknownKey, "/paths/~1a/post/parameters/0/x-note"),
			findingAtPointer(unknownKey, "/paths/~1a/post/parameters/0/required"),
			findingAtPointer(unknownKey, "/paths/~1a/post/requestBody/x-note"),
			findingAtPointer(unknownKey, "/paths/~1a/post/responses/200/x-note"),
			findingAtPointer(unknownKey, "/paths/~1a/post/responses/200/headers"),
			findingAtPointer(unknownKey, "/paths/~1a/post/callbacks/cb/x-cb"),
			findingAtPointer(unknownKey, "/paths/~1b/get/parameters/0/x-alias"),
			findingAtPointer(unknownKey, "/components/parameters/Alias/x-alias"),
		}},
		{"summary and description alone", "3.1.0", specrules.Parameter,
			`{$ref: '#/components/parameters/P', summary: s, description: d}`, nil},
		{"a path item keeps its fields beside its $ref", "3.2.0", specrules.Document,
			`{openapi: 3.2.0, info: {title: t, version: '1'}, paths: {/a: {$ref: '#/components/pathItems/P', ` +
				`get: {operationId: g, responses: {'200': {description: ok}}}}}, ` +
				`components: {pathItems: {P: {post: {operationId: p, responses: {'200': {description: ok}}}}}}}`,
			nil},
		{"one bogus key beside a parameter's $ref", "3.0.3", specrules.Parameter,
			`{$ref: '#/components/parameters/P', bogus: 1}`, []finding{findingAtPointer(unknownKey, "/bogus")}},
	})
}

// TestCheck_UnknownKeysAreReportedAtEveryObject pins that the table reports a
// key an object neither defines nor admits, once, at the key, for each kind of
// object it judges in every minor, and that an x- extension is admitted and a
// Schema Object's keywords are the dialect's to judge.
func TestCheck_UnknownKeysAreReportedAtEveryObject(t *testing.T) {
	kinds := []struct {
		name string
		kind specrules.Kind
		doc  string
	}{
		{"document", specrules.Document, `{openapi: VERSION, info: {title: t, version: '1'}, paths: {}, bogus: 1}`},
		{"info", specrules.Info, `{title: t, version: '1', bogus: 1}`},
		{"contact", specrules.Contact, `{bogus: 1}`},
		{"license", specrules.License, `{name: n, bogus: 1}`},
		{"server", specrules.Server, `{url: 'https://a.example', bogus: 1}`},
		{"externaldocs", specrules.ExternalDocs, `{url: 'https://a.example', bogus: 1}`},
		{"tag", specrules.Tag, `{name: t, bogus: 1}`},
		{"components", specrules.Components, `{bogus: 1}`},
		{"pathitem", specrules.PathItem, `{bogus: 1}`},
		{"operation", specrules.Operation, `{responses: {'200': {description: ok}}, bogus: 1}`},
		{"parameter", specrules.Parameter, `{name: q, in: query, schema: {type: string}, bogus: 1}`},
		{"requestbody", specrules.RequestBody, `{content: {application/json: {}}, bogus: 1}`},
		{"mediatype", specrules.MediaType, `{bogus: 1}`},
		{"encoding", specrules.Encoding, `{bogus: 1}`},
		{"response", specrules.Response, `{description: ok, bogus: 1}`},
		{"header", specrules.Header, `{schema: {type: string}, bogus: 1}`},
		{"example", specrules.Example, `{value: 1, bogus: 1}`},
		{"link", specrules.Link, `{operationId: x, bogus: 1}`},
		{"discriminator", specrules.Discriminator, `{propertyName: p, bogus: 1}`},
		{"xml", specrules.XML, `{name: n, bogus: 1}`},
		{"securityscheme", specrules.SecurityScheme, `{type: apiKey, name: k, in: header, bogus: 1}`},
		{"oauthflows", specrules.OAuthFlows, `{bogus: 1}`},
	}
	scenarios := make([]scenario, 0, 2*len(kinds)*3+2)
	for _, version := range []string{"3.0.3", "3.1.0", "3.2.0"} {
		for _, k := range kinds {
			doc := strings.ReplaceAll(k.doc, "VERSION", version)
			scenarios = append(scenarios, scenario{
				name: version + " " + k.name, version: version, kind: k.kind, doc: doc,
				want: []finding{findingAtPointer(unknownKey, "/bogus")},
			}, scenario{
				name: version + " " + k.name + " as an extension", version: version, kind: k.kind,
				doc: strings.ReplaceAll(doc, "bogus", "x-bogus"),
			})
		}
	}
	scenarios = append(scenarios,
		scenario{name: "a Schema Object's keywords are the dialect's", version: "3.1.0", kind: specrules.Schema,
			doc: `{type: string, bogus: 1}`},
		scenario{name: "a key a later minor defines is not undefined", version: "3.2.0", kind: specrules.Response,
			doc: `{summary: s, description: ok}`},
	)
	verify(t, scenarios)
}

// TestCheck_TheTableWordsAndGradesWhatItReports holds two things no position
// test reads: a key beside a $ref is worded as the Reference Object's, which
// defines only $ref, summary and description, and a reserved name is graded by
// its rule's level, a SHOULD, so a warning.
func TestCheck_TheTableWordsAndGradesWhatItReports(t *testing.T) {
	beside := run(t, "3.1.0", specrules.Parameter, `{$ref: '#/components/parameters/P', bogus: 1}`)
	require.Len(t, beside, 1)
	assert.Contains(t, beside[0].Message, `key "bogus" is written beside a $ref, where the Reference Object `+
		`defines only $ref, summary and description`)

	reserved := run(t, "3.1.0", specrules.Parameter, `{name: Content-Type, in: header, schema: {type: string}}`)
	require.Len(t, reserved, 1)
	assert.Equal(t, diag.ReservedHeaderName, reserved[0].Code)
	assert.Equal(t, ir.SeverityWarning, reserved[0].Severity)
}
