// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package operation_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/ir"
)

// TestKeepEntry_ReasonFollowsEffect holds the reason an encoding entry is kept
// under to what the entry means. An entry the walk decides names no property of
// the body, every entry of a file body and every entry under a media type that
// is neither multipart nor form have no effect where they are written: no IR
// node is coming for them, so they are out of scope. An entry of a body whose
// schema could name the part, a catch-all's, is kept because the compiler has
// no home for it yet, and is neither reported as naming nothing nor judged.
func TestKeepEntry_ReasonFollowsEffect(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /form:
    post:
      operationId: form
      requestBody:
        content:
          multipart/form-data:
            schema:
              type: object
              properties:
                file: {type: string, format: binary}
            encoding:
              file: {contentType: image/png, contentTyp: image/png}
              flie: {contentType: image/png}
      responses: {"204": {description: ok}}
  /bytes:
    post:
      operationId: bytes
      requestBody:
        content:
          multipart/form-data:
            schema: {type: string, format: binary}
            encoding:
              file: {contentType: image/png}
      responses: {"204": {description: ok}}
  /json:
    post:
      operationId: json
      requestBody:
        content:
          application/json:
            schema: {type: object, properties: {a: {type: string}}}
            encoding: {a: {contentType: text/plain}}
      responses: {"204": {description: ok}}
  /open:
    post:
      operationId: open
      requestBody:
        content:
          multipart/form-data:
            schema: {type: object, properties: {a: {type: string}}, additionalProperties: true}
            encoding: {extra: {contentType: text/plain}}
      responses: {"204": {description: ok}}
`)
	openapitest.RequireNoErrorDiags(t, diags)

	for _, tc := range []struct {
		operation, key string
		reason         ir.UnmodeledReason
		// warned is whether the entry is also reported as naming no property.
		warned bool
		at     string
	}{
		{"form", "openapi:encoding/file/contentTyp", ir.ReasonOutOfScope, false,
			"/paths/~1form/post/requestBody/content/multipart~1form-data/encoding/file/contentTyp"},
		{"form", "openapi:encoding/flie", ir.ReasonOutOfScope, true,
			"/paths/~1form/post/requestBody/content/multipart~1form-data/encoding/flie"},
		{"bytes", "openapi:encoding/file", ir.ReasonOutOfScope, true,
			"/paths/~1bytes/post/requestBody/content/multipart~1form-data/encoding/file"},
		{"json", "openapi:encoding/a", ir.ReasonOutOfScope, false,
			"/paths/~1json/post/requestBody/content/application~1json/encoding/a"},
		{"open", "openapi:encoding/extra", ir.ReasonNoIRHome, false,
			"/paths/~1open/post/requestBody/content/multipart~1form-data/encoding/extra"},
	} {
		operation := openapitest.FindOperation(t, doc, tc.operation)
		require.NotNil(t, operation.Request, tc.operation)
		entry, ok := operation.Request.Contents[0].Unmodeled[tc.key]
		if !assert.True(t, ok, "%s keeps %s", tc.operation, tc.key) {
			continue
		}
		assert.Equal(t, tc.reason, entry.Reason, "%s: %s", tc.operation, tc.key)
		if tc.key != "openapi:encoding/file/contentTyp" {
			assert.Equal(t, tc.warned, openapitest.HasDiagCodeAt(diags, diag.UnknownEncodingKey, tc.at),
				"%s: whether the entry is reported as naming no property", tc.operation)
		}
	}
	assert.False(t, openapitest.HasDiagCodeAt(diags, diag.UnknownEncodingKey,
		"/paths/~1open/post/requestBody/content/multipart~1form-data/encoding/extra"),
		"a catch-all may take the key, so the walk does not decide it names nothing")
}
