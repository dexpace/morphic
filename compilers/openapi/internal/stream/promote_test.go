// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package stream_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestPromote_ReadsPastAResponseWithNoBody pins that a success response with
// no body neither states a terminator nor offers a choice: the terminator and
// the condition are read off the response that streams beside it.
func TestPromote_ReadsPastAResponseWithNoBody(t *testing.T) {
	t.Parallel()
	src := `openapi: 3.1.0
info: {title: t, version: "1"}
paths:
  /chat:
    post:
      operationId: chat
` + when("$request.stream") + `      requestBody:
        content:
          application/json:
            schema: {type: object, properties: {stream: {type: boolean}}}
      responses:
        "202": {description: accepted}
        "200":
          description: ok
          content:
` + jsonBody + framed("text/event-stream", sentinel("[DONE]"))

	op, _, diags := compile(t, src, both())
	require.NotNil(t, terminator(op))
	assert.Equal(t, "[DONE]", *terminator(op))
	assert.NotNil(t, op.ResponseStream.Condition)
	assert.Empty(t, findings(t, diags))
}

// TestPromote_AConditionNeedsAStream pins that a key naming the input that
// asks for a stream, on an operation that streams no response, fills nothing
// and is reported at the key.
func TestPromote_AConditionNeedsAStream(t *testing.T) {
	t.Parallel()
	op, _, diags := compile(t, chat(when("$request.stream"), jsonBody), both())
	assert.Nil(t, op.ResponseStream)
	assert.Equal(t, []string{"/paths/~1chat/post/" + whenKey}, findings(t, diags))
	assert.NotContains(t, markers(op.Provenance.Inferred), "extension-promotion")
	assert.Contains(t, op.Unmodeled, "openapi:"+whenKey, "the key stays as written")
}
