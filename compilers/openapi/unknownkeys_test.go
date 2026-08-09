// This file covers one property: a key the OpenAPI model names no field for
// reaches the IR rather than vanishing between the parser and the lowering. It
// is separate from annotations_test.go because the subject is the complement of
// what the model names, not any one annotation.
package openapi_test // external test package — exercises only the public API

import (
	"os"
	"path/filepath"
	"reflect"
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
	sites := unmodeledSitesOf(doc)

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
		site, found := findUnmodeledSite(sites, tc.key, tc.want)
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

	assert.Empty(t, unmodeledSitesOf(doc), "nothing undeclared, so nothing kept")
	for _, d := range diags {
		assert.NotContains(t, d.Code, "unknown-", "a well-formed document reports no unknown key: %+v", d)
	}
}

// unmodeledSite is one Unmodeled entry paired with the walk path of the map
// holding it.
type unmodeledSite struct {
	key   string
	path  string
	entry ir.UnmodeledEntry
}

// unmodeledSitesOf returns every Unmodeled entry the document holds, found by
// walking the value graph rather than by naming the carriers a test expects.
func unmodeledSitesOf(doc *ir.Document) []unmodeledSite {
	unmodeledType := reflect.TypeOf(ir.Unmodeled(nil))
	var out []unmodeledSite
	ir.WalkValues(doc, ir.DocumentPath, func(v reflect.Value, path string) bool {
		if v.Type() != unmodeledType || !v.CanInterface() {
			return true
		}
		u, ok := v.Interface().(ir.Unmodeled)
		if !ok {
			return true
		}
		for key, entry := range u {
			out = append(out, unmodeledSite{key: key, path: path, entry: entry})
		}
		return true
	})
	return out
}

// findUnmodeledSite returns the site holding key with the given JSON value. The
// value is part of the match because one key spelling occurs at several
// carriers — "openapi:status" is written on two responses in this fixture — so
// matching on the key alone would find another object's entry and call it a
// pass.
func findUnmodeledSite(sites []unmodeledSite, key, wantJSON string) (unmodeledSite, bool) {
	for _, site := range sites {
		if site.key == key && string(site.entry.Value) == wantJSON {
			return site, true
		}
	}
	return unmodeledSite{}, false
}

// requireNoErrorDiagnostics fails the test on the first error-severity
// diagnostic, naming it.
func requireNoErrorDiagnostics(t *testing.T, diags []ir.Diagnostic) {
	t.Helper()
	d, ok := ir.FirstError(diags)
	require.False(t, ok, "unexpected error diagnostic: %+v", d)
}
