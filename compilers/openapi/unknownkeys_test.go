// This file covers one property: a key the OpenAPI model names no field for
// reaches the IR rather than vanishing between the parser and the lowering. It
// is separate from annotations_test.go because the subject is the complement of
// what the model names, not any one annotation.
package openapi_test // external test package — exercises only the public API

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
)

// unknownKeysSpec reads the census fixture the corpus sweeps also drive, so the
// assertions below and the six oracles run over the same bytes: putting it under
// testdata is what gets it compiled in both declaration orders, round-tripped and
// verified, none of which a spec written inline reaches.
func unknownKeysSpec(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "openapi", "unknown_keys.yaml"))
	require.NoError(t, err)
	return string(data)
}

// TestUnknownKeys_KeptAtEveryObject holds the whole rule rather than the
// positions that happened to be noticed. A key the model does not name reached
// no IR field, no Unmodeled entry and no diagnostic at every one of these
// objects, so two documents differing only in it compiled to the same IR
// (GitHub #297).
//
// Carriers are derived from the value graph rather than named: each row says
// which Unmodeled map the entry must land on by the path the walk reaches it at,
// so an entry written to the wrong carrier fails here rather than passing
// because the assertion looked only where it expected.
func TestUnknownKeys_KeptAtEveryObject(t *testing.T) {
	t.Parallel()
	doc, diags := compileAnnotationSpec(t, "unknown-keys", unknownKeysSpec(t))
	requireNoErrorDiagnostics(t, diags)
	sites := unmodeledSites(doc)

	for _, tc := range []struct {
		object  string
		key     string
		want    string
		carrier string
	}{
		{"openapi root", "openapi:basePath", `"ROOT"`, "doc.Unmodeled"},
		{"info", "openapi:info/contactEmail", `"INFO"`, "doc.Unmodeled"},
		{"contact", "openapi:info/contact/slack", `"CONTACT"`, "doc.Unmodeled"},
		{"license", "openapi:info/license/spdx", `"LICENSE"`, "doc.Unmodeled"},
		{"externalDocs", "openapi:externalDocs/title", `"EXTERNALDOCS"`, "doc.Unmodeled"},
		{"server", "openapi:host", `"SERVER"`, "doc.Servers[0].Unmodeled"},
		{"server variable", "openapi:example", `"SERVERVARIABLE"`, "doc.Servers[0].Variables[0].Unmodeled"},
		{"tag", "openapi:tags/0/color", `"TAG"`, "doc.Unmodeled"},
		{"components", "openapi:components/definitions", `"COMPONENTS"`, "doc.Unmodeled"},
		{"tag externalDocs", "openapi:tags/0/externalDocs/title", `"TAGEXTERNALDOCS"`, "doc.Unmodeled"},
		{"operation", "openapi:operationid", `"OPERATION"`, ".Unmodeled"},
		// The carrier is named down to the operation rather than left at
		// ".Unmodeled": the document writes this exact key too, and the row is only
		// evidence of the operation's if it cannot match the document's.
		{"operation externalDocs", "openapi:externalDocs/title", `"OPERATIONEXTERNALDOCS"`,
			"Operations[0].Unmodeled"},
		{"parameter", "openapi:collectionFormat", `"PARAMETER"`, ".Params[0].Unmodeled"},
		{"example", "openapi:name", `"EXAMPLE"`, ".Params[0].Examples[0].Unmodeled"},
		{"request body", "openapi:schema", `"REQUESTBODY"`, ".Request.Unmodeled"},
		{"media type", "openapi:format", `"MEDIATYPE"`, ".Contents[0].Unmodeled"},
		// On the content rather than on the part: ir.PartEncoding holds no
		// Unmodeled map, and one content can carry an entry per part, so the part
		// name is what tells two encodings' keys apart on that one map.
		{"encoding", "openapi:encoding/part/contentEncoding", `"ENCODING"`, ".Contents[1].Unmodeled"},
		{"response", "openapi:status", `"RESPONSE"`, ".Responses[0].Unmodeled"},
		{"error response", "openapi:status", `"ERRORRESPONSE"`, ".Errors[0].Unmodeled"},
		{"header", "openapi:in", `"HEADER"`, ".Headers[0].Unmodeled"},
		{"security scheme", "openapi:tokenUrl", `"SECURITYSCHEME"`,
			"doc.Auth[auth/openapi/components/securitySchemes/k].Unmodeled"},
		{"oauth flows", "openapi:flows/application", `"OAUTHFLOWS"`,
			"doc.Auth[auth/openapi/components/securitySchemes/o].Unmodeled"},
		{"oauth flow", "openapi:scope", `"OAUTHFLOW"`,
			"doc.Auth[auth/openapi/components/securitySchemes/o].Flows[0].Unmodeled"},
		{"schema", "openapi:additionalItems", `"SCHEMA"`,
			"doc.Types[t/openapi/components/schemas/S].Unmodeled"},
		// The property's schema reduced to a shared primitive, so it owns no node
		// and its keywords stay on the declaring property — the carrier rule
		// fillPropertyAnnotations already applies to every other annotation.
		{"property schema", "openapi:divisibleBy", `"PROPERTYSCHEMA"`,
			"doc.Types[t/openapi/components/schemas/S].Properties[0].Unmodeled"},
	} {
		site, found := findUnmodeled(sites, tc.key, tc.want)
		if !assert.True(t, found, "%s drops %s = %s: it is nowhere in the document",
			tc.object, tc.key, tc.want) {
			continue
		}
		assert.Contains(t, site.path, tc.carrier, "%s key lands on the wrong carrier", tc.object)
	}
}

// TestUnknownKeys_SchemaAndObjectAreGradedApart pins the one distinction the
// census turns on, at the two keys the fixture picks for it: a draft-07
// additionalItems in a schema, and an operationId with the case wrong on an
// operation.
//
// JSON Schema states that an implementation must ignore a keyword it does not
// recognize, so an unrecognized keyword in a schema is legal input and may carry
// meaning for other tooling; OpenAPI states that an extension key must be
// prefixed x-, so an undefined key on one of its objects is not an extension but
// a defect in the document.
//
// Both are kept — invariant 2 does not bend for invalid input — and both carry
// the same reason, because "no IR node is coming" is true of each. What differs
// is what the document did, which is the diagnostic channel's subject: an
// unrecognized keyword records a decision at info, and an undefined key reports
// a fault at warning.
func TestUnknownKeys_SchemaAndObjectAreGradedApart(t *testing.T) {
	t.Parallel()
	doc, diags := compileAnnotationSpec(t, "unknown-keys", unknownKeysSpec(t))
	requireNoErrorDiagnostics(t, diags)

	schema, ok := doc.Types[ir.TypeID("t/openapi/components/schemas/S")]
	require.True(t, ok)
	keyword := unmodeledEntry(t, schema.Common().Unmodeled, "openapi:additionalItems")
	assert.Equal(t, ir.ReasonOutOfScope, keyword.Reason,
		"no IR node is coming for a keyword this compiler does not model")
	assert.Equal(t, "/components/schemas/S/additionalItems", keyword.Provenance.Pointer)
	assert.Equal(t, []ir.Severity{ir.SeverityInfo},
		diagsAt(diags, "openapi/unknown-schema-keyword", "/components/schemas/S/additionalItems"))

	op, ok := opByName(doc, "listWidgets")
	require.True(t, ok)
	key := unmodeledEntry(t, op.Unmodeled, "openapi:operationid")
	assert.Equal(t, ir.ReasonOutOfScope, key.Reason,
		"OpenAPI defines no such key, so no IR node is coming for it either")
	assert.Equal(t, "/paths/~1widgets/get/operationid", key.Provenance.Pointer)
	assert.Equal(t, []ir.Severity{ir.SeverityWarning},
		diagsAt(diags, "openapi/unknown-object-key", "/paths/~1widgets/get/operationid"))
}

// TestUnknownKeys_WellFormedDocumentRecordsNothing is the control the census
// needs: a document writing only what the model names keeps no entry and reports
// no finding, so the rows above are evidence of the keys they name rather than
// of a sweep that fires on everything.
func TestUnknownKeys_WellFormedDocumentRecordsNothing(t *testing.T) {
	t.Parallel()
	doc, diags := compileAnnotationSpec(t, "clean", `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /widgets:
    get:
      operationId: listWidgets
      responses: {"200": {description: ok}}
components:
  schemas:
    S: {type: object, properties: {a: {type: string}}}
`)
	requireNoErrorDiagnostics(t, diags)

	assert.Empty(t, unmodeledSites(doc), "nothing undeclared, so nothing kept")
	for _, d := range diags {
		assert.NotContains(t, d.Code, "unknown-", "a well-formed document reports no unknown key: %+v", d)
	}
}

// TestUnknownKeys_SchemaSubObjects covers the three objects that hang off a
// schema — its xml, its discriminator and its externalDocs — which the fixture
// above deliberately leaves out.
//
// It leaves them out because the OpenAPI dialect meta-schema closes all three to
// anything but an x- key, so the library reports a validation error on each and
// harness.Check returns at the first one, before the oracles that fixture exists
// to reach. The keys are kept and announced all the same, which is what this
// asserts: an invalid document is still not a document whose keys may vanish.
//
// Graded as an OpenAPI object's keys rather than as schema keywords, at warning
// rather than info: JSON Schema's rule that an unrecognized keyword is legal
// governs the schema, and these three are OpenAPI objects the schema vocabulary
// says nothing about. All three ride on the schema's own map, since none of
// ir.XMLHints, ir.Discriminator or ir.Link holds one, and the keyword each was
// written under is what keeps them apart there.
func TestUnknownKeys_SchemaSubObjects(t *testing.T) {
	t.Parallel()
	doc, diags := compileAnnotationSpec(t, "schema-sub-objects", `openapi: 3.1.0
info: {title: T, version: "1"}
paths: {}
components:
  schemas:
    S:
      type: object
      xml: {name: s, attribute2: XML}
      discriminator: {propertyName: k, mapping2: DISCRIMINATOR}
      externalDocs: {url: 'https://d.example', title: SCHEMAEXTERNALDOCS}
      properties: {k: {type: string}}
`)
	schema, ok := doc.Types[ir.TypeID("t/openapi/components/schemas/S")]
	require.True(t, ok)

	// assert rather than require on the lookup, so a keyword missing from the map
	// reports itself and leaves the other two still checked. The three share one
	// reader, and a row that never runs is no evidence about the keyword it names.
	for _, tc := range []struct{ keyword, key, want string }{
		{"xml", "openapi:xml/attribute2", `"XML"`},
		{"discriminator", "openapi:discriminator/mapping2", `"DISCRIMINATOR"`},
		{"externalDocs", "openapi:externalDocs/title", `"SCHEMAEXTERNALDOCS"`},
	} {
		entry, found := schema.Common().Unmodeled[tc.key]
		if !assert.True(t, found, "schema %s drops %s: it is nowhere on the schema", tc.keyword, tc.key) {
			continue
		}
		assert.Equal(t, tc.want, string(entry.Value), "%s key keeps what the source wrote", tc.keyword)
		assert.Equal(t, ir.ReasonOutOfScope, entry.Reason, "%s key reason", tc.keyword)
		at := "/components/schemas/S/" + tc.keyword + "/" + tc.key[len("openapi:"+tc.keyword+"/"):]
		assert.Equal(t, at, entry.Provenance.Pointer, "%s key provenance", tc.keyword)
		assert.Equal(t, []ir.Severity{ir.SeverityWarning},
			diagsAt(diags, "openapi/unknown-object-key", at),
			"%s key is announced as an object's, not as a schema keyword's", tc.keyword)
	}
}

// requireNoErrorDiagnostics fails the test on the first error-severity
// diagnostic, naming it.
func requireNoErrorDiagnostics(t *testing.T, diags []ir.Diagnostic) {
	t.Helper()
	d, ok := ir.FirstError(diags)
	require.False(t, ok, "unexpected error diagnostic: %+v", d)
}
