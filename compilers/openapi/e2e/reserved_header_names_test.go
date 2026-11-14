// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

// This file covers one property: OpenAPI says a header parameter named Accept,
// Content-Type or Authorization, and a Content-Type entry in a headers map,
// SHALL be ignored, and the rule table reports each once, wherever the object
// is written, whether or not the lowering reads it.
package e2e_test

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
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
			for _, d := range diags {
				if d.Code == diag.ReservedHeaderName {
					assert.Contains(t, d.Message, "SHALL be ignored", "the rule's own words: %s", d.Message)
				}
			}
		})
	}
}
