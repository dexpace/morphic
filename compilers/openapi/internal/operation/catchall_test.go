// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package operation_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestPartKeys_ACatchAllSayingNothingHasNoKey holds a catch-all whose parts
// take no flag and no default content type, a value of a string or an object,
// to no key: its value and its pattern each say nothing, so the holder states
// none, as a part declaring nothing has no Encoding entry.
func TestPartKeys_ACatchAllSayingNothingHasNoKey(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, `openapi: 3.1.0
info: {title: t, version: "1"}
paths:
  /a:
    post:
      operationId: a
      requestBody:
        content:
          multipart/form-data:
            schema:
              type: object
              additionalProperties: {anyOf: [{type: string}, {type: object}]}
              patternProperties: {'^x': {anyOf: [{type: string}, {type: object}]}}
      responses: {"204": {description: ok}}
`)
	assert.Empty(t, diags)
	assert.Nil(t, at(requestContent(t, doc, "a"), "catchAllEncoding"))
}
