package operation_test

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/engine/reader"
	"github.com/dexpace/morphic/ir"
)

func TestContent_AllMediaTypesKeptInOrder(t *testing.T) {
	t.Parallel()
	spec := openapitest.PathsSpec(`  /docs:
    post:
      operationId: createDoc
      requestBody:
        required: true
        content:
          application/json: {schema: {type: object, properties: {n: {type: string}}}}
          application/xml: {schema: {type: object, properties: {n: {type: string}}}}
      responses: {"201": {description: created}}
`)
	_, svc, diags := lowerServiceSpec(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)
	op := openapitest.FirstOp(t, svc)
	require.NotNil(t, op.Request)
	require.Len(t, op.Request.Contents, 2, "no primary-content selection in the IR")
	assert.Equal(t, "application/json", op.Request.Contents[0].MediaType)
	assert.Equal(t, "application/xml", op.Request.Contents[1].MediaType)
	assert.Equal(t, []string{"application/json", "application/xml"},
		op.Bindings.HTTP[0].RequestContentTypes)
}

// TestContent_MixedCaseFormContentKeepsEncoding drives the form arm of
// lowerContent's switch with spellings RFC 6838 §4.2 treats as identical to
// multipart/form-data: classification is case- and parameter-insensitive, while
// the IR keeps the declared spelling (#603).
func TestContent_MixedCaseFormContentKeepsEncoding(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		mt   string
	}{
		{"mixed case", "Multipart/Form-Data"},
		{"mixed case with parameters", "Multipart/Form-Data; charset=utf-8"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			spec := openapitest.PathsSpec(`  /upload:
    post:
      operationId: upload
      requestBody:
        content:
          "` + tc.mt + `":
            schema:
              type: object
              properties:
                meta: {type: object, properties: {k: {type: string}}}
            encoding:
              meta:
                contentType: application/json
      responses: {"200": {description: ok}}
`)
			_, svc, diags := lowerServiceSpec(t, spec)
			openapitest.RequireNoErrorDiags(t, diags)
			content := openapitest.FirstOp(t, svc).Request.Contents[0]
			assert.Equal(t, tc.mt, content.MediaType, "the declared spelling is what the IR records")
			metaProp := ir.PropID("p/openapi" + ids.Ptr("paths", "/upload", "post", "requestBody", "content", tc.mt, "schema", "properties", "meta"))
			enc, ok := content.Encoding[metaProp]
			require.True(t, ok, "encoding keyed by the part property's PropID; got keys %v", content.Encoding)
			assert.Equal(t, []string{"application/json"}, enc.ContentTypes)
		})
	}
}

// TestContent_MixedCaseURLEncodedKeepsPartEncoding drives the form arm with
// spellings of application/x-www-form-urlencoded that differ in case, in
// parameters, or in both. The body declares no explicit encoding, so the
// part's encoding is the one the lowering synthesizes for a repeated part.
func TestContent_MixedCaseURLEncodedKeepsPartEncoding(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		mt   string
	}{
		{"mixed case", "Application/X-WWW-Form-Urlencoded"},
		{"parameters", "application/x-www-form-urlencoded; charset=utf-8"},
		{"mixed case with parameters", "Application/X-WWW-Form-Urlencoded; charset=utf-8"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			spec := openapitest.PathsSpec(`  /submit:
    post:
      operationId: submit
      requestBody:
        content:
          "` + tc.mt + `":
            schema:
              type: object
              properties:
                tags: {type: array, items: {type: string}}
      responses: {"200": {description: ok}}
`)
			_, svc, diags := lowerServiceSpec(t, spec)
			openapitest.RequireNoErrorDiags(t, diags)
			content := openapitest.FirstOp(t, svc).Request.Contents[0]
			assert.Equal(t, tc.mt, content.MediaType, "the declared spelling is what the IR records")
			tagsProp := ir.PropID("p/openapi" + ids.Ptr("paths", "/submit", "post", "requestBody", "content", tc.mt, "schema", "properties", "tags"))
			enc, ok := content.Encoding[tagsProp]
			require.True(t, ok, "a repeated part carries a PartEncoding keyed by its PropID; got keys %v", content.Encoding)
			assert.True(t, enc.Multi, "the structural flag is read off the part")
		})
	}
}

func TestContent_BinaryOctetStreamBody(t *testing.T) {
	t.Parallel()
	spec := openapitest.PathsSpec(`  /raw:
    post:
      operationId: putRaw
      requestBody:
        required: true
        content:
          application/octet-stream:
            schema: {type: string, format: binary}
      responses: {"200": {description: ok}}
`)
	_, svc, diags := lowerServiceSpec(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)
	op := openapitest.FirstOp(t, svc)
	require.NotNil(t, op.Request)
	require.Len(t, op.Request.Contents, 1)
	content := op.Request.Contents[0]
	require.NotNil(t, content.File, "binary body lowers to a FileInfo")
	assert.False(t, content.File.IsText)
	assert.Equal(t, ir.TypeID("t/prim/bytes"), content.Type.Target)
}

// TestContent_MixedCaseOctetStreamBodyIsFile drives the binary arm with
// spellings RFC 6838 §4.2 treats as identical to application/octet-stream: the
// body without a schema is a file, and FileInfo.ContentTypes keeps the declared
// spelling (#603).
func TestContent_MixedCaseOctetStreamBodyIsFile(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		mt   string
	}{
		{"mixed case", "Application/Octet-Stream"},
		{"mixed case with parameters", "Application/Octet-Stream; charset=utf-8"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			spec := openapitest.PathsSpec(`  /raw:
    post:
      operationId: putRaw
      requestBody:
        required: true
        content:
          "` + tc.mt + `": {}
      responses: {"200": {description: ok}}
`)
			_, svc, diags := lowerServiceSpec(t, spec)
			openapitest.RequireNoErrorDiags(t, diags)
			content := openapitest.FirstOp(t, svc).Request.Contents[0]
			require.NotNil(t, content.File, "an octet-stream body with no schema is a file body")
			assert.False(t, content.File.IsText)
			assert.Equal(t, []string{tc.mt}, content.File.ContentTypes, "the declared spelling is what the IR records")
			assert.Equal(t, ir.TypeID("t/prim/bytes"), content.Type.Target)
		})
	}
}

func TestContent_BinaryRefBodyDetectedAsFile(t *testing.T) {
	t.Parallel()
	// A binary body referenced via $ref must still be detected as a File body,
	// exactly like the inline string+binary form.
	spec := openapitest.PathsSpec(`  /raw:
    post:
      operationId: putRaw
      requestBody:
        required: true
        content:
          application/octet-stream:
            schema: {$ref: "#/components/schemas/Blob"}
      responses: {"200": {description: ok}}
components:
  schemas:
    Blob: {type: string, format: binary}
`)
	_, svc, diags := lowerServiceSpec(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)
	content := openapitest.FirstOp(t, svc).Request.Contents[0]
	require.NotNil(t, content.File, "binary body behind a $ref lowers to a FileInfo")
	assert.False(t, content.File.IsText)
	assert.Equal(t, ir.TypeID("t/prim/bytes"), content.Type.Target)
}

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
            multipart/mixed:
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
	upload := openapitest.FindOp(t, doc, "upload")

	// Non-required body preserved as present, optionality on the typed field.
	require.NotNil(t, upload.Request)
	require.NotNil(t, upload.Request.Required, "optionality preserved")
	assert.False(t, *upload.Request.Required)

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
	_, hasLinks := resp.Unmodeled["openapi:links"]
	assert.True(t, hasLinks)

	assert.False(t, openapitest.HasDiag(diags, diag.DegradedConstruct),
		"nothing this spec declares is degraded; links are kept verbatim without a report; got %+v", diags)
}

func TestContent_OctetAndErrorMulti(t *testing.T) {
	t.Parallel()
	doc, _ := parseFull(t, contentSpec)
	// The /raw octet-stream body without a schema is a binary file body.
	raw := opByPath(t, doc, "POST", "/raw")
	require.NotNil(t, raw.Request)
	require.NotEmpty(t, raw.Request.Contents)
	assert.NotNil(t, raw.Request.Contents[0].File)
	// Its 400 error declares two media types, and both are Contents (#422).
	require.Len(t, raw.Errors, 1)
	payload := raw.Errors[0].Payload
	require.NotNil(t, payload)
	require.Len(t, payload.Contents, 2, "neither media type is elected over the other")
	assert.Equal(t, []string{"application/json", "application/problem+json"},
		[]string{payload.Contents[0].MediaType, payload.Contents[1].MediaType})
	assert.Empty(t, raw.Errors[0].Unmodeled, "nothing is kept verbatim beside the typed payload")
}

func TestContent_SequentialAndEmptyBody(t *testing.T) {
	t.Parallel()
	doc, _ := parseFull(t, contentSpec)
	stream := openapitest.FindOp(t, doc, "stream")
	resp := stream.Responses[0]
	require.NotNil(t, resp.Payload)
	c := resp.Payload.Contents[0]
	require.NotNil(t, c.Item, "itemSchema becomes the element type")
	require.NotNil(t, c.ItemEncoding, "itemEncoding lowered structurally")
	assert.Equal(t, []string{"application/json"}, c.ItemEncoding.ContentTypes)
	assert.True(t, c.ItemEncoding.Multi, "itemEncoding governs a repeated tail")

	// Empty request-body content lowers to a body naming no media type.
	empty := openapitest.FindOp(t, doc, "emptyBody")
	require.NotNil(t, empty.Request)
	assert.Empty(t, empty.Request.Contents)
}

// multipartEncoding returns the part-encoding map of an operation's request.
func multipartEncoding(t *testing.T, op ir.Operation) map[ir.PropID]ir.PartEncoding {
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

// opByPath finds an operation by HTTP method and URI template. A webhook
// binding has no URI template, so no webhook is found.
func opByPath(t *testing.T, doc *ir.Document, method, uri string) ir.Operation {
	t.Helper()
	require.NotEmpty(t, doc.Services, "the spec lowers to at least one service")
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

const componentBodyRefSpec = `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /a:
    post:
      operationId: postA
      requestBody: {$ref: '#/components/requestBodies/Body'}
      responses: {"200": {description: ok}}
  /b:
    post:
      operationId: postB
      requestBody: {$ref: '#/components/requestBodies/Body'}
      responses: {"200": {description: ok}}
components:
  requestBodies:
    Body:
      content:
        application/json:
          schema: {type: object, properties: {n: {type: string}}}
`

const headerIdentitySpec = `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /a:
    get:
      operationId: getA
      responses:
        "200":
          description: ok
          headers:
            X-Rate: {$ref: '#/components/headers/Rate'}
            X-Limit: {$ref: '#/components/headers/Rate'}
components:
  headers:
    Rate: {schema: {type: string, enum: [a, b]}}
`

// TestContent_SharedComponentSchemaTakesItsDeclarationHint pins where the name
// hint of a $ref'd component's schema comes from. The node interns once, at the
// component's declaration, so a use-site hint — the referencing operationId for
// a body, the map key for a header — would name the one shared node after
// whichever reference happened to lower first. Naming.Hint is what emitters
// render from, so "post_a_request" on a body two operations share is a wrong
// name, not a cosmetic one. The hint is the component name in neutral words,
// which is what every name channel in the IR carries (invariant 4).
func TestContent_SharedComponentSchemaTakesItsDeclarationHint(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, componentBodyRefSpec)
	openapitest.RequireNoErrorDiags(t, diags)
	bodyID := ir.TypeID("t/anon/components/requestBodies/Body/content/application~1json/schema")
	body, ok := doc.Types[bodyID]
	require.True(t, ok)
	assert.True(t, body.Common().Anonymous, "a requestBody component is not a named type")
	assert.Equal(t, "body", body.Common().Name.Hint,
		"the shared body schema is hinted from its component, not from postA or postB")

	hdrDoc, hdrDiags := parseFull(t, headerIdentitySpec)
	openapitest.RequireNoErrorDiags(t, hdrDiags)
	hdr, ok := hdrDoc.Types[ir.TypeID("t/anon/components/headers/Rate/schema")]
	require.True(t, ok)
	assert.Equal(t, "rate", hdr.Common().Name.Hint,
		"the shared header schema is hinted from its component, not from X-Rate or X-Limit")
}

// TestContent_InlineSchemaKeepsItsUseSiteHint is the other half of the rule
// above: only a component declaration renames the hoisted node. An inline body
// has exactly one use site, so its operationId-derived hint is the best name
// available and must survive.
func TestContent_InlineSchemaKeepsItsUseSiteHint(t *testing.T) {
	t.Parallel()
	spec := openapitest.PathsSpec(`  /a:
    post:
      operationId: postA
      requestBody:
        content:
          application/json:
            schema: {type: object, properties: {n: {type: string}}}
      responses: {"200": {description: ok}}
`)
	doc, diags := parseFull(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)
	td, ok := doc.Types[ir.TypeID("t/anon/paths/~1a/post/requestBody/content/application~1json/schema")]
	require.True(t, ok)
	assert.Equal(t, "post_a_request", td.Common().Name.Hint)
}

// TestContent_UnkeepableMediaTypeRefIsReported pins the media type $ref that
// cannot be kept verbatim, a mapping writing one key twice: it is reported as
// lost, and nothing claims it was kept.
func TestContent_UnkeepableMediaTypeRefIsReported(t *testing.T) {
	t.Parallel()
	_, _, diags := lowerServiceSpec(t, openapitest.PathsSpecVer("3.2.0", `  /a:
    get:
      operationId: a
      responses:
        "200":
          description: ok
          content:
            application/json: {$ref: {a: 1, a: 2}}
`))
	openapitest.AssertHasCode(t, diags, diag.UnpreservableConstruct, ir.SeverityError)
	assert.False(t, openapitest.HasDiagCodeAt(diags, diag.DegradedConstruct,
		"/paths/~1a/get/responses/200/content/application~1json/$ref"), "nothing announces a keep it did not make")
}

// TestContent_MediaTypeRefIsKeptNotFollowed pins a content entry written as a
// Reference Object while this compiler does not follow one (GitHub #856): the
// $ref and the summary beside it are kept verbatim and one warning says the
// entry lowered as written. Below 3.2, which does not allow it, it lowers the
// same, and load adds that the construct is a later version's (GitHub #864).
func TestContent_MediaTypeRefIsKeptNotFollowed(t *testing.T) {
	t.Parallel()
	const at = "/paths/~1a/get/responses/200/content/application~1json/$ref"
	const paramAt = "/paths/~1a/get/parameters/0/content/application~1json/$ref"
	for _, version := range []string{"3.2.0", "3.1.0"} {
		t.Run(version, func(t *testing.T) {
			t.Parallel()
			_, svc, diags := lowerServiceSpec(t, openapitest.PathsSpecVer(version, `  /a:
    get:
      operationId: a
      parameters:
        - name: f
          in: query
          content:
            application/json: {$ref: '#/components/mediaTypes/Json'}
      responses:
        "200":
          description: ok
          content:
            application/json: {$ref: '#/components/mediaTypes/Json', summary: S}
`))
			op := openapitest.FirstOp(t, svc)
			content := op.Responses[0].Payload.Contents[0]
			assert.Equal(t, ir.ReasonDegradedLowering, content.Unmodeled["openapi:$ref"].Reason)
			assert.Contains(t, content.Unmodeled, "openapi:summary", "nothing beside the $ref is dropped")
			assert.True(t, openapitest.HasDiagCodeAt(diags, diag.DegradedConstruct, at), "got %v", diags)
			require.Len(t, op.Params, 1)
			assert.Equal(t, ir.ReasonDegradedLowering, op.Params[0].Unmodeled["openapi:content/application~1json/$ref"].Reason)
			assert.True(t, openapitest.HasDiagCodeAt(diags, diag.DegradedConstruct, paramAt))
			assert.False(t, openapitest.HasDiag(diags, diag.UnknownObjectKey))
			later := version != "3.2.0"
			assert.Equal(t, later, openapitest.HasDiagCodeAt(diags, diag.LaterVersionConstruct, at))
			assert.Equal(t, later, openapitest.HasDiagCodeAt(diags, diag.LaterVersionConstruct, paramAt))
		})
	}
}

// TestContent_MediaTypeDescriptionIsKept pins the description OpenAPI 3.2 gives
// a Media Type Object, which the bundled model names no field for and
// ir.Content has no docs to hold: at a body's content and at a parameter's, it
// is kept verbatim with an info rather than called undefined. Below 3.2 it is
// kept the same, and load adds that the field is a later version's.
func TestContent_MediaTypeDescriptionIsKept(t *testing.T) {
	t.Parallel()
	const at = "/paths/~1a/get/responses/200/content/application~1json/description"
	const paramAt = "/paths/~1a/get/parameters/0/content/application~1json/description"
	for _, version := range []string{"3.2.0", "3.1.0"} {
		t.Run(version, func(t *testing.T) {
			t.Parallel()
			_, svc, diags := lowerServiceSpec(t, openapitest.PathsSpecVer(version, `  /a:
    get:
      operationId: a
      parameters:
        - name: f
          in: query
          content:
            application/json: {description: A filter., schema: {type: string}}
      responses:
        "200":
          description: ok
          content:
            application/json: {description: The body., schema: {type: string}}
`))
			openapitest.RequireNoErrorDiags(t, diags)
			op := openapitest.FirstOp(t, svc)
			kept := op.Responses[0].Payload.Contents[0].Unmodeled["openapi:description"]
			assert.Equal(t, ir.ReasonNoIRHome, kept.Reason)
			assert.JSONEq(t, `"The body."`, string(kept.Value))
			require.Len(t, op.Params, 1)
			assert.Equal(t, ir.ReasonNoIRHome,
				op.Params[0].Unmodeled["openapi:content/application~1json/description"].Reason)
			openapitest.AssertInfoDiagAt(t, diags, at)
			openapitest.AssertInfoDiagAt(t, diags, paramAt)
			assert.False(t, openapitest.HasDiag(diags, diag.UnknownObjectKey))
			assert.Equal(t, version != "3.2.0", openapitest.HasDiagCodeAt(diags, diag.LaterVersionConstruct, at))
		})
	}
}

// TestContent_ASharedBodyOrHeaderIsNamedForItsDeclaration pins how a request
// body or a header two operations reach by reference is named: for its
// declaration, never for whichever reference lowered first, a body for the
// operation declaring it whether or not that operation is lowered, and built in
// the document declaring it. Each spec is compiled with the two referencing
// operations in both orders, and the type registries must match: another
// document's declaration, one there at the pointer a referencing operation has
// in the source, and one the source writes under no operation.
func TestContent_ASharedBodyOrHeaderIsNamedForItsDeclaration(t *testing.T) {
	t.Parallel()
	const other = `openapi: 3.1.0
info: {title: O, version: "1"}
paths:
  /x:
    post:
      operationId: otherPost
      requestBody:
        content: {application/json: {schema: {type: object, properties: {v: {type: string}}}}}
      responses:
        "200":
          description: ok
          headers: {X-H: {schema: {type: object, properties: {h: {type: integer}}}}}
`
	const local = `x-bodies:
  b: {content: {application/json: {schema: {type: object, properties: {v: {type: string}}}}}}
x-headers:
  h: {schema: {type: object, properties: {h: {type: integer}}}}
`
	op := func(path, id, body, header string) string {
		return "  " + path + ":\n    post:\n      operationId: " + id + "\n      requestBody: {$ref: '" + body + "'}\n" +
			"      responses:\n        \"200\":\n          description: ok\n          headers: {X-" + id +
			": {$ref: '" + header + "'}}\n"
	}
	for name, tc := range map[string]struct {
		extra, path, body, header string
		nodes                     []string // each node's ID and hint, sorted
	}{
		"another document's": {"", "/a", "other.yaml#/paths/~1x/post/requestBody",
			"other.yaml#/paths/~1x/post/responses/200/headers/X-H", []string{
				"t/anon@other.yaml/paths/~1x/post/requestBody/content/application~1json/schema other_post_request",
				"t/anon@other.yaml/paths/~1x/post/responses/200/headers/X-H/schema x_h"}},
		"another document's, at the source's own pointer": {"", "/x", "other.yaml#/paths/~1x/post/requestBody",
			"other.yaml#/paths/~1x/post/responses/200/headers/X-H", []string{
				"t/anon@other.yaml/paths/~1x/post/requestBody/content/application~1json/schema other_post_request",
				"t/anon@other.yaml/paths/~1x/post/responses/200/headers/X-H/schema x_h"}},
		"the source's, under no operation": {local, "/a", "#/x-bodies/b", "#/x-headers/h", []string{
			"t/anon/x-bodies/b/content/application~1json/schema request", "t/anon/x-headers/h/schema h"}},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			compile := func(first, second string) ir.TypeRegistry {
				t.Helper()
				dir := t.TempDir()
				require.NoError(t, os.WriteFile(filepath.Join(dir, "other.yaml"), []byte(other), 0o600))
				doc, diags, err := openapi.New().Compile(t.Context(), []compilers.Source{{
					Path: filepath.Join(dir, "root.yaml"),
					Data: []byte("openapi: 3.1.0\ninfo: {title: T, version: \"1\"}\n" + tc.extra + "paths:\n" + first + second),
				}}, compilers.Options{Reader: reader.Disk{}})
				require.NoError(t, err)
				openapitest.RequireNoErrorDiags(t, diags)
				return doc.Types
			}
			alpha, bravo := op(tc.path, "alpha", tc.body, tc.header), op("/b", "bravo", tc.body, tc.header)

			forward, backward := compile(alpha, bravo), compile(bravo, alpha)

			assert.Empty(t, cmp.Diff(forward, backward), "the names must not follow the declaration order")
			var nodes []string
			for _, td := range forward {
				if td.Common().Provenance.Source != ir.NoSource {
					nodes = append(nodes, string(td.Common().ID)+" "+td.Common().Name.Hint)
				}
			}
			slices.Sort(nodes)
			assert.Equal(t, tc.nodes, nodes, "each built in its declaration's document, under its name")
		})
	}
}
