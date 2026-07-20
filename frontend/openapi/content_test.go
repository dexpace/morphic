package openapi

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
)

func TestContent_AllMediaTypesKeptInOrder(t *testing.T) {
	t.Parallel()
	spec := `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /docs:
    post:
      operationId: createDoc
      requestBody:
        required: true
        content:
          application/json: {schema: {type: object, properties: {n: {type: string}}}}
          application/xml: {schema: {type: object, properties: {n: {type: string}}}}
      responses: {"201": {description: created}}
`
	_, svc, diags := lowerServiceSpec(t, spec)
	requireNoErrorDiags(t, diags)
	op := svc.Groups[0].Operations[0]
	require.NotNil(t, op.Request)
	require.Len(t, op.Request.Contents, 2, "no primary-content selection in the IR")
	assert.Equal(t, "application/json", op.Request.Contents[0].MediaType)
	assert.Equal(t, "application/xml", op.Request.Contents[1].MediaType)
	assert.Equal(t, []string{"application/json", "application/xml"},
		op.Bindings.HTTP[0].RequestContentTypes)
}

func TestContent_MultipartPartEncoding(t *testing.T) {
	t.Parallel()
	spec := `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /upload:
    post:
      operationId: upload
      requestBody:
        content:
          multipart/form-data:
            schema:
              type: object
              properties:
                meta: {type: object, properties: {k: {type: string}}}
                file: {type: string, format: binary}
            encoding:
              meta:
                contentType: application/json
                headers:
                  X-Part: {schema: {type: string}}
      responses: {"200": {description: ok}}
`
	_, svc, diags := lowerServiceSpec(t, spec)
	requireNoErrorDiags(t, diags)
	content := svc.Groups[0].Operations[0].Request.Contents[0]
	metaProp := ir.PropID("p/openapi" + ptr("paths", "/upload", "post", "requestBody", "content", "multipart/form-data", "schema", "properties", "meta"))
	enc, ok := content.Encoding[string(metaProp)]
	require.True(t, ok, "encoding keyed by the part property's PropID; got keys %v", content.Encoding)
	assert.Equal(t, []string{"application/json"}, enc.ContentTypes)
	require.Len(t, enc.Headers, 1)
	assert.Equal(t, "X-Part", enc.Headers[0].WireName)

	fileProp := ir.PropID("p/openapi" + ptr("paths", "/upload", "post", "requestBody", "content", "multipart/form-data", "schema", "properties", "file"))
	fileEnc, ok := content.Encoding[string(fileProp)]
	require.True(t, ok, "binary part gets a synthesized file PartEncoding")
	assert.True(t, fileEnc.Filename)
}

func TestContent_BinaryOctetStreamBody(t *testing.T) {
	t.Parallel()
	spec := `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /raw:
    post:
      operationId: putRaw
      requestBody:
        required: true
        content:
          application/octet-stream:
            schema: {type: string, format: binary}
      responses: {"200": {description: ok}}
`
	_, svc, diags := lowerServiceSpec(t, spec)
	requireNoErrorDiags(t, diags)
	op := svc.Groups[0].Operations[0]
	require.NotNil(t, op.Request)
	require.Len(t, op.Request.Contents, 1)
	content := op.Request.Contents[0]
	require.NotNil(t, content.File, "binary body lowers to a FileInfo")
	assert.False(t, content.File.IsText)
	assert.Equal(t, ir.TypeID("t/prim/bytes"), content.Type.Target)
}

func TestContent_NonRequiredRequestBody(t *testing.T) {
	t.Parallel()
	spec := `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /maybe:
    post:
      operationId: maybe
      requestBody:
        content:
          application/json: {schema: {type: object, properties: {n: {type: string}}}}
      responses: {"200": {description: ok}}
`
	_, svc, diags := lowerServiceSpec(t, spec)
	requireNoErrorDiags(t, diags)
	op := svc.Groups[0].Operations[0]
	require.NotNil(t, op.Request, "a non-required body still lowers to a present Payload")
	raw, ok := op.Request.Extensions["openapi:required"]
	require.True(t, ok, "body optionality preserved under extensions")
	assert.Equal(t, "false", string(raw))
	found := false
	for _, d := range diags {
		if d.Severity == ir.SeverityInfo && strings.Contains(d.Message, "request body") {
			found = true
		}
	}
	assert.True(t, found, "non-required body emits one info diagnostic")
}

func TestContent_ArrayMultipartPartMulti(t *testing.T) {
	t.Parallel()
	spec := `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /bulk:
    post:
      operationId: bulk
      requestBody:
        content:
          multipart/form-data:
            schema:
              type: object
              properties:
                tags: {type: array, items: {type: string}}
      responses: {"200": {description: ok}}
`
	_, svc, diags := lowerServiceSpec(t, spec)
	requireNoErrorDiags(t, diags)
	content := svc.Groups[0].Operations[0].Request.Contents[0]
	tagsProp := "p/openapi" + ptr("paths", "/bulk", "post", "requestBody", "content", "multipart/form-data", "schema", "properties", "tags")
	enc, ok := content.Encoding[tagsProp]
	require.True(t, ok, "array part gets a synthesized PartEncoding; got keys %v", content.Encoding)
	assert.True(t, enc.Multi, "array-typed part repeats per item")
}
