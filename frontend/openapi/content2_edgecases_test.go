package openapi

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const multipartVariantsSpec = `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /noschema:
    post:
      operationId: noSchema
      requestBody:
        content:
          multipart/form-data: {}
      responses: {"200": {description: ok}}
  /noprops:
    post:
      operationId: noProps
      requestBody:
        content:
          multipart/form-data:
            schema: {type: object}
      responses: {"200": {description: ok}}
  /plainprops:
    post:
      operationId: plainProps
      requestBody:
        content:
          multipart/form-data:
            schema:
              type: object
              properties: {a: {type: string}, b: {type: integer}}
      responses: {"200": {description: ok}}
  /examples:
    get:
      operationId: exGet
      responses:
        "200":
          description: ok
          content:
            application/json:
              schema: {type: string}
              examples:
                empty: {summary: no value here}
`

func TestContent_MultipartEncodingVariants(t *testing.T) {
	t.Parallel()
	doc, _ := parseFull(t, multipartVariantsSpec)
	for _, name := range []string{"noSchema", "noProps", "plainProps"} {
		op := findOp(t, doc, name)
		require.NotNil(t, op.Request, "%s has a request", name)
		for _, c := range op.Request.Contents {
			assert.Empty(t, c.Encoding, "%s multipart yields no per-part encoding", name)
		}
	}
}

func TestContent_ExampleWithoutValueSkipped(t *testing.T) {
	t.Parallel()
	doc, _ := parseFull(t, multipartVariantsSpec)
	op := findOp(t, doc, "exGet")
	c := op.Responses[0].Payload.Contents[0]
	assert.Empty(t, c.Examples, "an example without a value is skipped")
}

func TestContentTypeKeys_Nil(t *testing.T) {
	t.Parallel()
	assert.Nil(t, contentTypeKeys(nil))
}
