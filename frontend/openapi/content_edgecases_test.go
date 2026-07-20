package openapi

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
)

const contentSpec = `openapi: 3.2.0
info: {title: T, version: "1"}
paths:
  /upload:
    post:
      operationId: upload
      requestBody:
        required: false
        content:
          multipart/form-data:
            schema:
              type: object
              properties:
                file: {type: string, format: binary}
                tags: {type: array, items: {type: string}}
                note: {type: string}
            encoding:
              file:
                contentType: image/png, image/jpeg
                headers:
                  X-Rate: {schema: {type: integer}}
              tags:
                style: form
                explode: true
      responses:
        "200":
          description: ok
          content:
            application/json:
              schema: {type: object, properties: {ok: {type: boolean}}}
              example: {ok: true}
              examples:
                sample: {value: {ok: false}}
            application/xml:
              schema: {type: string}
          headers:
            X-Trace: {schema: {type: string}}
          links:
            self: {operationId: upload}
  /raw:
    post:
      requestBody:
        content:
          application/octet-stream: {}
      responses:
        "200": {description: ok}
        "400":
          description: bad
          content:
            application/json: {schema: {type: object}}
            application/problem+json: {schema: {type: string}}
  /stream:
    get:
      operationId: stream
      responses:
        "200":
          description: ok
          content:
            application/jsonl:
              itemSchema: {type: object, properties: {a: {type: string}}}
              itemEncoding: {contentType: application/json}
              x-note: streamy
  /empty:
    post:
      operationId: emptyBody
      requestBody:
        content: {}
      responses:
        "204": {description: no content}
`

func TestContent_FullPipeline(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, contentSpec)
	upload := findOp(t, doc, "upload")

	// Non-required body preserved as present with optionality under extensions.
	require.NotNil(t, upload.Request)
	_, hasReq := upload.Request.Extensions["openapi:required"]
	assert.True(t, hasReq, "non-required optionality preserved")

	// Multipart encoding: comma-split content types, header, style/explode, file flag.
	hb := upload.Bindings.HTTP[0]
	require.NotEmpty(t, hb.RequestContentTypes)
	var filePart, tagsPart ir.PartEncoding
	for _, pe := range multipartEncoding(t, upload) {
		if pe.Filename {
			filePart = pe
		}
		if pe.Multi {
			tagsPart = pe
		}
	}
	assert.Equal(t, []string{"image/png", "image/jpeg"}, filePart.ContentTypes)
	assert.NotEmpty(t, filePart.Headers)
	assert.True(t, tagsPart.Multi)

	// Response: multiple media types kept, example + examples, headers, links raw.
	resp := upload.Responses[0]
	require.NotNil(t, resp.Payload)
	assert.Len(t, resp.Payload.Contents, 2)
	assert.GreaterOrEqual(t, len(resp.Payload.Contents[0].Examples), 2)
	assert.NotEmpty(t, resp.Headers)
	_, hasLinks := resp.Extensions["openapi:links"]
	assert.True(t, hasLinks)

	var sawDegraded bool
	for _, d := range diags {
		if d.Code == codeDegradedConstruct {
			sawDegraded = true
		}
	}
	assert.True(t, sawDegraded)
}

func TestContent_OctetAndErrorMulti(t *testing.T) {
	t.Parallel()
	doc, _ := parseFull(t, contentSpec)
	// The /raw octet-stream body without a schema is a binary file body.
	raw := opByPath(t, doc, "POST", "/raw")
	require.NotNil(t, raw.Request)
	require.NotEmpty(t, raw.Request.Contents)
	assert.NotNil(t, raw.Request.Contents[0].File)
	// Its 400 error has two media types → content preserved raw.
	require.NotEmpty(t, raw.Errors)
	var multi ir.ErrorCase
	for _, ec := range raw.Errors {
		if len(ec.Extensions) > 0 {
			multi = ec
		}
	}
	_, hasContent := multi.Extensions["openapi:content"]
	assert.True(t, hasContent, "multi-media error content preserved")
}

func TestContent_SequentialAndEmptyBody(t *testing.T) {
	t.Parallel()
	doc, _ := parseFull(t, contentSpec)
	stream := findOp(t, doc, "stream")
	resp := stream.Responses[0]
	require.NotNil(t, resp.Payload)
	c := resp.Payload.Contents[0]
	require.NotNil(t, c.Item, "itemSchema becomes the element type")
	_, hasItemEnc := c.Extensions["openapi:itemEncoding"]
	assert.True(t, hasItemEnc, "itemEncoding preserved")

	// Empty request-body content yields no Request payload.
	empty := findOp(t, doc, "emptyBody")
	assert.Nil(t, empty.Request)
}

// multipartEncoding returns the part-encoding map of an operation's request.
func multipartEncoding(t *testing.T, op ir.Operation) map[string]ir.PartEncoding {
	t.Helper()
	require.NotNil(t, op.Request)
	for _, c := range op.Request.Contents {
		if len(c.Encoding) > 0 {
			return c.Encoding
		}
	}
	t.Fatal("no encoding map")
	return nil
}

// opByPath finds an operation by HTTP method and URI template.
func opByPath(t *testing.T, doc *ir.Document, method, uri string) ir.Operation {
	t.Helper()
	for _, g := range doc.Services[0].Groups {
		for _, op := range g.Operations {
			for _, hb := range op.Bindings.HTTP {
				if hb.Method == method && hb.URITemplate == uri {
					return op
				}
			}
		}
	}
	t.Fatalf("op %s %s not found", method, uri)
	return ir.Operation{}
}
