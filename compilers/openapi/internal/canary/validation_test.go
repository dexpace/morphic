package canary_test

import (
	"strings"
	"testing"

	oas3 "github.com/speakeasy-api/openapi/jsonschema/oas3"
	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/speakeasy-api/openapi/validation"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// findings decodes source and returns what the library's validation found,
// as text.
func findings(t *testing.T, source string) []string {
	t.Helper()
	_, found, err := soa.Unmarshal(t.Context(), strings.NewReader(source))
	require.NoError(t, err)
	text := make([]string, 0, len(found))
	for _, f := range found {
		text = append(text, f.Error())
	}
	return text
}

// validationVersion pins validation-version: validation does not gate a
// construct by the declared version, except where noted.
func validationVersion(t *testing.T) {
	t.Run("a document", documentValidationVersion)
	t.Run("a schema", schemaValidationVersion)
}

// documentValidationVersion checks a document whole: additionalOperations
// aside, a later minor's construct passes at an earlier one, fields 3.1 and
// 3.2 stopped requiring are required, and three constructs 3.2 added fail at
// 3.2.0.
func documentValidationVersion(t *testing.T) {
	t.Parallel()
	later := "openapi: 3.0.3\ninfo: {title: t, version: '1', summary: s}\npaths: {}\nwebhooks: {}\n"
	assert.Empty(t, findings(t, later), "3.1 constructs at 3.0.3: %s", revisit(t, result{}))

	gated := "openapi: 3.1.0\ninfo: {title: t, version: '1'}\npaths:\n  /a:\n" +
		"    additionalOperations: {COPY: {responses: {'200': {description: ok}}}}\n" +
		"  /b:\n    get: {operationId: b}\n"
	got := strings.Join(findings(t, gated), "\n")
	assert.Contains(t, got, "pathItem.additionalOperations is not supported in OpenAPI version 3.1.0",
		revisit(t, result{}))
	assert.Contains(t, got, "`operation.responses` is required", "at 3.1.0: %s", revisit(t, result{}))

	current := "openapi: 3.2.0\ninfo: {title: t, version: '1'}\npaths:\n  /c:\n    get:\n" +
		"      parameters: [{name: s, in: cookie, style: cookie, schema: {type: object}}]\n" +
		"      responses: {'200': {description: ok}}\n" +
		"  /d:\n    get: {operationId: d}\n" +
		"  /e:\n    get: {responses: {'200': {}}}\n" +
		"components:\n  schemas:\n" +
		"    P: {oneOf: [{$ref: '#/components/schemas/Q'}], discriminator: {propertyName: k, defaultMapping: Q}}\n" +
		"    Q: {type: object, xml: {nodeType: element}}\n"
	got = strings.Join(findings(t, current), "\n")
	for _, refused := range []string{
		"`operation.responses` is required",
		"`response.description` is required",
		"parameter.style must be one of [`form`] for in=cookie",
		"schema.discriminator.defaultMapping",
		"schema.xml.nodeType",
	} {
		assert.Contains(t, got, refused, "at 3.2.0: %s", revisit(t, result{}))
	}
}

// schemaValidationVersion checks one schema: its own Validate discards the
// version it is handed, which oas3.Validate reads, so only the latter accepts
// a keyword 3.2 added.
func schemaValidationVersion(t *testing.T) {
	t.Parallel()
	doc, _, err := soa.Unmarshal(t.Context(), strings.NewReader(
		"openapi: 3.2.0\ninfo: {title: t, version: '1'}\npaths: {}\n"+
			"components:\n  schemas:\n    Q: {type: object, xml: {nodeType: element}}\n"))
	require.NoError(t, err)
	q, ok := doc.Components.Schemas.Get("Q")
	require.True(t, ok)
	version := "3.2.0"
	at32 := validation.WithContextObject(&oas3.ParentDocumentVersion{OpenAPI: &version})

	assert.Empty(t, oas3.Validate(t.Context(), q, at32), "oas3.Validate at 3.2: %s", revisit(t, result{}))
	assert.NotEmpty(t, q.Validate(t.Context(), at32), "JSONSchema.Validate at 3.2: %s", revisit(t, result{}))
}

// emptyIsMissing pins empty-is-missing: validation reports a required field
// written as the empty string, or as an empty map, as missing.
func emptyIsMissing(t *testing.T) {
	source := "openapi: 3.1.0\ninfo: {title: '', version: ''}\npaths:\n  /a:\n    post:\n" +
		"      requestBody: {content: {}}\n      responses: {'200': {description: ''}}\n"
	got := strings.Join(findings(t, source), "\n")
	for _, field := range []string{"`info.title` is required", "`info.version` is required",
		"`response.description` is required", "`requestBody.content` is required"} {
		assert.Contains(t, got, field, revisit(t, result{}))
	}
}
