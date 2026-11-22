// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

// This file covers one property: OpenAPI says a header parameter named Accept,
// Content-Type or Authorization, and a Content-Type entry in a headers map,
// SHALL be ignored, and the rule table reports each once, wherever the object
// is written, whether or not the lowering reads it.
package e2e_test

import (
	"encoding/json/v2"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/ir"
)

// reservedNamesBody writes each spelling of a reserved header name beside the
// ordinary names that must draw nothing: parameters, a response's headers, an
// encoding's headers, a component two operations use and one nothing uses.
const reservedNamesBody = `info: {title: T, version: "1"}
paths:
  /a:
    get:
      operationId: g
      parameters:
        - {name: authorization, in: header, schema: {type: string}}
        - {name: ACCEPT, in: header, schema: {type: string}}
        - {name: Content-Type, in: header, schema: {type: string}}
        - {name: X-Trace, in: header, schema: {type: string}}
        - {name: Accept, in: query, schema: {type: string}}
      responses:
        "200":
          description: ok
          headers:
            content-type: {schema: {type: string}}
            Accept: {schema: {type: string}}
            X-Other: {schema: {type: string}}
    post:
      operationId: p
      parameters:
        - {$ref: '#/components/parameters/Shared'}
      requestBody:
        content:
          multipart/form-data:
            schema: {type: object, properties: {file: {type: string}}}
            encoding:
              file:
                headers:
                  Content-Type: {schema: {type: string}}
                  X-Part: {schema: {type: string}}
      responses: {"204": {description: ok}}
  /b:
    get:
      operationId: b
      parameters:
        - {$ref: '#/components/parameters/Shared'}
      responses: {"204": {description: ok}}
components:
  parameters:
    Shared: {name: Authorization, in: header, schema: {type: string}}
    Unused: {name: Content-Type, in: header, schema: {type: string}}
`

// TestReservedHeaderNames_ReportedOnceWhereverWritten holds each reserved name,
// in every object that can write one and in OpenAPI 3.0, 3.1 and 3.2, to one
// warning: a parameter's at its name, not at the parameter, a headers map's at
// the entry, and a component's at its own name however many operations use it.
func TestReservedHeaderNames_ReportedOnceWhereverWritten(t *testing.T) {
	t.Parallel()
	pointers := []string{
		"/paths/~1a/get/parameters/0/name",
		"/paths/~1a/get/parameters/1/name",
		"/paths/~1a/get/parameters/2/name",
		"/paths/~1a/get/responses/200/headers/content-type",
		"/paths/~1a/post/requestBody/content/multipart~1form-data/encoding/file/headers/Content-Type",
		"/components/parameters/Shared/name",
		"/components/parameters/Unused/name",
	}
	want := make([]string, 0, len(pointers))
	for _, p := range pointers {
		want = append(want, "warning "+diag.ReservedHeaderName+" "+p)
	}
	slices.Sort(want)

	for _, version := range []string{"3.0.3", "3.1.0", "3.2.0"} {
		t.Run(version, func(t *testing.T) {
			t.Parallel()
			_, diags := compileAnnotationSpec(t, "reserved", "openapi: "+version+"\n"+reservedNamesBody)
			requireNoErrorDiagnostics(t, diags)

			assert.Equal(t, want, findingsOf(diags, diag.ReservedHeaderName), "each name once, and no other")
			for _, p := range pointers {
				assert.Equal(t, []string{diag.ReservedHeaderName}, codesAt(diags, p), "no second code about %s", p)
			}
			for _, diagnostic := range diags {
				if diagnostic.Code == diag.ReservedHeaderName {
					assert.Contains(t, diagnostic.Message, "SHALL be ignored", "the rule's own words: %s", diagnostic.Message)
				}
			}
		})
	}
}

// acceptReserved is the finding at a header parameter named Accept, whatever
// becomes of the definition.
const acceptReserved = `header parameter "Accept" is reserved: OpenAPI says a definition for Accept SHALL be ` +
	"ignored, a rule the compiler leaves to the emitter"

// TestReservedHeaderNames_MessageHoldsWhereNothingLowers pins the words of the
// finding at three fates: a component an operation lowers, one nothing
// references, kept verbatim under Unmodeled, and a path item's inline parameter
// its operation overrides, which is kept nowhere in the document. The table reports before any
// lowering decides a fate, so the words state the reservation and who applies
// it, and claim no fate.
func TestReservedHeaderNames_MessageHoldsWhereNothingLowers(t *testing.T) {
	t.Parallel()
	doc, diags := compileAnnotationSpec(t, "reserved", `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /a:
    parameters: [{name: Accept, in: header, description: path-level, schema: {type: string}}]
    get:
      operationId: g
      parameters: [{$ref: '#/components/parameters/Used'}]
      responses: {"204": {description: ok}}
components:
  parameters:
    Used: {name: Accept, in: header, description: used, schema: {type: string}}
    Unused: {name: Accept, in: header, schema: {type: string}}
  responses:
    Unused: {description: ok, headers: {Content-Type: {schema: {type: string}}}}
`)
	requireNoErrorDiagnostics(t, diags)

	assert.NotContains(t, doc.Unmodeled, "openapi:components/parameters/Used", "an operation lowers it")
	assert.Contains(t, doc.Unmodeled, "openapi:components/parameters/Unused", "nothing lowers it")
	assert.Contains(t, doc.Unmodeled, "openapi:components/responses/Unused", "nothing lowers it")
	params := operationByID(t, doc, "operation/openapi/paths/~1a/get").Params
	require.Len(t, params, 1, "the operation's Accept overrides its path item's")
	assert.Equal(t, "used", params[0].Docs.Description)
	encoded, err := json.Marshal(doc)
	require.NoError(t, err)
	assert.NotContains(t, string(encoded), "path-level", "the overridden definition is kept nowhere")

	for _, pointer := range []string{
		"/components/parameters/Used/name", "/components/parameters/Unused/name", "/paths/~1a/parameters/0/name",
	} {
		assert.Equal(t, acceptReserved, messageAt(t, diags, diag.ReservedHeaderName, pointer), pointer)
	}
	assert.Equal(t, `header "Content-Type" is reserved: OpenAPI says a Content-Type entry in a headers map SHALL be `+
		"ignored, a rule the compiler leaves to the emitter",
		messageAt(t, diags, diag.ReservedHeaderName, "/components/responses/Unused/headers/Content-Type"))
}

// TestReservedHeaderNames_MessageHoldsWhereAnOverriddenReferenceIsKept writes
// the overridden path-item parameter by $ref. The override drops the
// reference, so no lowering reaches the component, which is kept verbatim
// under Unmodeled like any other nothing references, and the finding at its
// name reads as it does everywhere else.
func TestReservedHeaderNames_MessageHoldsWhereAnOverriddenReferenceIsKept(t *testing.T) {
	t.Parallel()
	doc, diags := compileAnnotationSpec(t, "reserved", `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /a:
    parameters: [{$ref: '#/components/parameters/PathAccept'}]
    get:
      operationId: g
      parameters: [{name: accept, in: header, description: op-level, schema: {type: string}}]
      responses: {"204": {description: ok}}
components:
  parameters:
    PathAccept: {name: Accept, in: header, description: path-level, schema: {type: string}}
`)
	requireNoErrorDiagnostics(t, diags)

	params := operationByID(t, doc, "operation/openapi/paths/~1a/get").Params
	require.Len(t, params, 1, "the operation's accept overrides its path item's Accept")
	assert.Equal(t, "op-level", params[0].Docs.Description)
	kept, ok := doc.Unmodeled["openapi:components/parameters/PathAccept"]
	require.True(t, ok, "the overridden component is kept: %v", doc.Unmodeled)
	assert.Equal(t, ir.ReasonNoIRHome, kept.Reason)
	assert.Contains(t, string(kept.Value), "path-level")

	assert.Equal(t, acceptReserved, messageAt(t, diags, diag.ReservedHeaderName, "/components/parameters/PathAccept/name"))
	assert.Equal(t, []string{
		"warning " + diag.ReservedHeaderName + " /components/parameters/PathAccept/name",
		"warning " + diag.ReservedHeaderName + " /paths/~1a/get/parameters/0/name",
	}, findingsOf(diags, diag.ReservedHeaderName), "once at the component, not again at the $ref")
}
