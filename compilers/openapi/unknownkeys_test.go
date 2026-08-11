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

// specFile reads a census fixture the corpus sweeps also drive, so the
// assertions below and the six oracles run over the same bytes: putting one
// under testdata is what gets it compiled in both declaration orders,
// round-tripped and verified, none of which a spec written inline reaches.
func specFile(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "openapi", name))
	require.NoError(t, err)
	return string(data)
}

// unknownKeysSpec is the census fixture writing one undeclared key per object.
func unknownKeysSpec(t *testing.T) string {
	t.Helper()
	return specFile(t, "unknown_keys.yaml")
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
		{"operation", "openapi:operationid", `"OPERATION"`, ".Unmodeled"},
		{"parameter", "openapi:collectionFormat", `"PARAMETER"`, ".Params[0].Unmodeled"},
		{"request body", "openapi:schema", `"REQUESTBODY"`, ".Request.Unmodeled"},
		{"media type", "openapi:format", `"MEDIATYPE"`, ".Contents[0].Unmodeled"},
		{"response", "openapi:status", `"RESPONSE"`, ".Responses[0].Unmodeled"},
		{"error response", "openapi:status", `"ERRORRESPONSE"`, ".Errors[0].Unmodeled"},
		{"header", "openapi:in", `"HEADER"`, ".Headers[0].Unmodeled"},
		{"components", "openapi:components/definitions", `"COMPONENTS"`, "doc.Unmodeled"},
		{"security scheme", "openapi:tokenUrl", `"SECURITYSCHEME"`,
			"doc.Auth[auth/openapi/components/securitySchemes/k].Unmodeled"},
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

// TestUnknownKeys_SpellingDecidesNeitherEntryNorCarrier holds the census to the
// two spellings that used to lose a key outright, each of which put a document
// writing it and a document writing nothing at all into the same IR.
//
// An aliased key is reported by the parser under the name it resolves to, while
// the mapping node it was written on still holds an alias whose own value is the
// anchor. Searching that mapping for the resolved name found nothing, and a key
// with no node kept nothing and said nothing.
//
// A key holding a "/" collided with the scope of the object that path names:
// entries for the objects with no Unmodeled map of their own are keyed by the
// path from the carrier down, so a root key spelled "info/contact/slack" was the
// same entry as the contact object's own "slack", and the site that reached the
// carrier second found it taken and dropped its key in silence. Escaping the key
// as one segment is what separates them, per ids.Scope.
func TestUnknownKeys_SpellingDecidesNeitherEntryNorCarrier(t *testing.T) {
	t.Parallel()
	doc, diags := compileAnnotationSpec(t, "spellings", specFile(t, "unknown_key_spellings.yaml"))
	requireNoErrorDiagnostics(t, diags)
	sites := unmodeledSites(doc)

	for _, tc := range []struct {
		spelling string
		key      string
		want     string
	}{
		{"a key written as an alias", "openapi:info/license/spdx", `"LICENSE_ALIASED"`},
		{"a root key spelling another object's scope", "openapi:info~1contact~1slack", `"ROOT_SLASHED"`},
		{"the scope that key spells", "openapi:info/contact/slack", `"CONTACT_PLAIN"`},
	} {
		site, found := findUnmodeled(sites, tc.key, tc.want)
		if !assert.True(t, found, "%s drops %s = %s: it is nowhere in the document",
			tc.spelling, tc.key, tc.want) {
			continue
		}
		assert.Equal(t, ir.ReasonOutOfScope, site.entry.Reason,
			"%s is the census's, not the extension reader's", tc.spelling)
	}
	assert.Len(t, diagsAt(diags, "openapi/unknown-object-key", "/info/license/spdx"), 1,
		"an aliased key is announced at the name it resolves to")
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

// requireNoErrorDiagnostics fails the test on the first error-severity
// diagnostic, naming it.
func requireNoErrorDiagnostics(t *testing.T, diags []ir.Diagnostic) {
	t.Helper()
	d, ok := ir.FirstError(diags)
	require.False(t, ok, "unexpected error diagnostic: %+v", d)
}
