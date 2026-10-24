package e2e_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/ir"
)

// assertNoValidationFinding requires that the rule table reported nothing
// about the document, which is what the required-field cases below are about.
func assertNoValidationFinding(t *testing.T, diags []ir.Diagnostic) {
	t.Helper()
	for _, d := range diags {
		assert.False(t, strings.HasPrefix(d.Code, diag.Validation+"/"), "unexpected validation finding: %+v", d)
	}
}

// assertEmptyRequiredValues pins that a REQUIRED field written as the empty
// string, which every minor's schema admits, is no finding and lowers as
// written: the empty server URL, the parameter the document named "", and each
// security scheme with an empty name or URL (GitHub #622).
func assertEmptyRequiredValues(t *testing.T, doc *ir.Document, diags []ir.Diagnostic) {
	assertNoValidationFinding(t, diags)
	require.Len(t, doc.Servers, 2)
	assert.Empty(t, doc.Servers[0].URLTemplate)
	require.Len(t, doc.Servers[1].Variables, 2)
	assert.Equal(t, "base", doc.Servers[1].Variables[1].Name)

	ops := allOperations(doc)
	require.Len(t, ops, 1)
	require.Len(t, ops[0].Params, 1)
	assert.Equal(t, ir.Naming{Hint: "empty"}, ops[0].Params[0].Name, "the empty name is minted a hint")
	require.Len(t, ops[0].Responses, 1)
	assert.Empty(t, ops[0].Responses[0].Docs.Description)

	require.Len(t, doc.Auth, 3, "every scheme interns")
	assert.Len(t, doc.Auth["auth/openapi/components/securitySchemes/oauth"].Flows, 4)
	assert.Equal(t, ir.AuthKindAPIKey, doc.Auth["auth/openapi/components/securitySchemes/key"].Kind)
	assert.Equal(t, ir.AuthKindOpenIDConnect, doc.Auth["auth/openapi/components/securitySchemes/oidc"].Kind)
}

// assertRelaxedRequiredFields pins the two fields a minor stopped requiring,
// left out of a 3.2 document: an operation declaring no responses lowers with
// none, and a response declaring no description lowers undocumented, inline
// and as a component alike (GitHub #622).
func assertRelaxedRequiredFields(t *testing.T, doc *ir.Document, diags []ir.Diagnostic) {
	assertNoValidationFinding(t, diags)
	byName := map[string]ir.Operation{}
	for _, op := range allOperations(doc) {
		byName[op.Name.Source] = op
	}
	send := byName["sendEvent"]
	assert.Empty(t, send.Responses, "no responses declared, none lowered")
	assert.Empty(t, send.Errors)

	list := byName["listEvents"]
	require.Len(t, list.Responses, 1)
	assert.Empty(t, list.Responses[0].Docs.Description)
	assert.Len(t, list.Responses[0].Headers, 1)
	require.Len(t, list.Errors, 1, "the component response lowers at its status")
	assert.Empty(t, list.Errors[0].Docs.Description)
	require.NotNil(t, list.Errors[0].Payload)
}

// assertEmptyRequestContent pins a request body whose content map is empty: it
// lowers to a Payload naming no media type that keeps whether it must be sent,
// its description and its x-*, and is warned once at each declaration,
// whichever operations share it (GitHub #622).
func assertEmptyRequestContent(t *testing.T, doc *ir.Document, diags []ir.Diagnostic) {
	const code = "openapi/empty-request-content"
	byName := map[string]ir.Operation{}
	for _, op := range allOperations(doc) {
		byName[op.Name.Source] = op
	}
	inline := byName["postInline"].Request
	require.NotNil(t, inline)
	assert.Empty(t, inline.Contents)
	assert.Equal(t, new(true), inline.Required)
	assert.Equal(t, "A body naming no media type.", inline.Docs.Description)
	assert.Contains(t, inline.Unmodeled, "openapi:x-origin")
	for _, name := range []string{"postShared", "putShared"} {
		shared := byName[name].Request
		require.NotNil(t, shared, name)
		assert.Empty(t, shared.Contents, name)
	}
	n := 0
	for _, d := range diags {
		if d.Code == code {
			n++
		}
	}
	assert.Equal(t, 2, n, "two declarations, two reports: %+v", diags)
	for _, at := range []string{"/paths/~1inline/post/requestBody/content", "/components/requestBodies/Empty/content"} {
		assert.Equal(t, []ir.Severity{ir.SeverityWarning}, diagsAt(diags, code, at), at)
	}
}

// assertReachedOutsideModel pins that an object written under an x- key is
// checked as one the model declares when a $ref reaches it, a discriminator
// mapping names it, or a $ref nested in such an object does (GitHub #872):
// each construct OpenAPI 3.2 added draws its warning once, at the pointer it is
// written at, and a finding about a scheme reached that way is placed where its
// field is written, as for the scheme written inline (#879).
func assertReachedOutsideModel(t *testing.T, _ *ir.Document, diags []ir.Diagnostic) {
	const later = "openapi/later-version-construct"
	written := []string{
		"/x-lib/Pet/xml/nodeType", "/x-lib/Owner/xml/nodeType", "/x-lib/Cat/xml/nodeType", "/x-responses/Listed/summary",
	}
	for _, at := range written {
		assert.Equal(t, []ir.Severity{ir.SeverityWarning}, diagsAt(diags, later, at), "%s", at)
	}
	n := 0
	for _, d := range diags {
		if d.Code == later {
			n++
		}
	}
	assert.Equal(t, len(written), n, "one warning per construct, none elsewhere: %+v", diags)
	assert.Equal(t, []ir.Severity{ir.SeverityWarning},
		diagsAt(diags, "openapi/validation/validation-type-mismatch", "/x-schemes/key/description"),
		"the table's finding on the reached scheme, where the field is written (GitHub #879)")
}
