package operation_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
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

func TestContent_MultipartPartEncoding(t *testing.T) {
	t.Parallel()
	spec := openapitest.PathsSpec(`  /upload:
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
`)
	_, svc, diags := lowerServiceSpec(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)
	content := openapitest.FirstOp(t, svc).Request.Contents[0]
	metaProp := ir.PropID("p/openapi" + ids.Ptr("paths", "/upload", "post", "requestBody", "content", "multipart/form-data", "schema", "properties", "meta"))
	enc, ok := content.Encoding[metaProp]
	require.True(t, ok, "encoding keyed by the part property's PropID; got keys %v", content.Encoding)
	assert.Equal(t, []string{"application/json"}, enc.ContentTypes)
	require.Len(t, enc.Headers, 1)
	assert.Equal(t, "X-Part", enc.Headers[0].WireName)

	fileProp := ir.PropID("p/openapi" + ids.Ptr("paths", "/upload", "post", "requestBody", "content", "multipart/form-data", "schema", "properties", "file"))
	fileEnc, ok := content.Encoding[fileProp]
	require.True(t, ok, "binary part gets a synthesized file PartEncoding")
	assert.True(t, fileEnc.Filename)
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

func TestContent_MultipartRefBodyKeepsEncoding(t *testing.T) {
	t.Parallel()
	// A multipart body referenced via $ref must keep its per-part encoding, keyed
	// by the RESOLVED model's property IDs (under the ref target's pointer).
	spec := openapitest.PathsSpec(`  /upload:
    post:
      operationId: upload
      requestBody:
        content:
          multipart/form-data:
            schema: {$ref: "#/components/schemas/Form"}
            encoding:
              meta:
                contentType: application/json
      responses: {"200": {description: ok}}
components:
  schemas:
    Form:
      type: object
      properties:
        meta: {type: object, properties: {k: {type: string}}}
        file: {type: string, format: binary}
`)
	_, svc, diags := lowerServiceSpec(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)
	content := openapitest.FirstOp(t, svc).Request.Contents[0]
	require.NotNil(t, content.Encoding, "referenced multipart body keeps per-part encoding")

	metaProp := ir.PropID("p/openapi" + ids.Ptr("components", "schemas", "Form", "properties", "meta"))
	enc, ok := content.Encoding[metaProp]
	require.True(t, ok, "encoding keyed by the resolved property's PropID; got %v", content.Encoding)
	assert.Equal(t, []string{"application/json"}, enc.ContentTypes)

	fileProp := ir.PropID("p/openapi" + ids.Ptr("components", "schemas", "Form", "properties", "file"))
	fileEnc, ok := content.Encoding[fileProp]
	require.True(t, ok, "binary part gets a synthesized file PartEncoding")
	assert.True(t, fileEnc.Filename)
}

// aliasedMultipartSpec is a multipart body whose media schema is written as
// mediaSchema, over a Form component reachable both directly and through a
// second component that is a bare $ref to it.
func aliasedMultipartSpec(mediaSchema string) string {
	return openapitest.PathsSpec(`  /upload:
    post:
      operationId: upload
      requestBody:
        content:
          multipart/form-data:
            schema: ` + mediaSchema + `
            encoding:
              meta:
                contentType: application/json
      responses: {"200": {description: ok}}
components:
  schemas:
    Form:
      type: object
      properties:
        meta: {type: object, properties: {k: {type: string}}}
        file: {type: string, format: binary}
    AliasForm: {$ref: "#/components/schemas/Form"}
`)
}

// TestContent_MultipartAliasBodyKeyedByAliasedModel covers the spellings that put
// an alias scalar between a content and its body model: a $ref carrying siblings,
// which has nowhere but an alias to keep them, and a $ref to a component that is
// itself a bare $ref.
//
// The parts belong to the model at the end of that chain, so the keys must be its
// property IDs. A pointer cut from the media schema's own $ref names the first
// hop instead, which for an alias component addresses no property anywhere.
func TestContent_MultipartAliasBodyKeyedByAliasedModel(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name        string
		mediaSchema string
	}{
		{"a $ref carrying siblings", `{$ref: "#/components/schemas/Form", title: Upload}`},
		{"a $ref to an alias component", `{$ref: "#/components/schemas/AliasForm"}`},
		{"a $ref carrying siblings to an alias component",
			`{$ref: "#/components/schemas/AliasForm", title: Upload}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc, svc, diags := lowerServiceSpec(t, aliasedMultipartSpec(tc.mediaSchema))
			openapitest.RequireNoErrorDiags(t, diags)
			content := openapitest.FirstOp(t, svc).Request.Contents[0]
			form, isModel := typeByName(doc, "Form").(*ir.Model)
			require.True(t, isModel, "the body model is the component at the end of the chain")

			declared := openapitest.IndexBy(form.Properties, func(p ir.Property) ir.PropID { return p.ID })
			require.NotEmpty(t, content.Encoding, "an aliased multipart body still carries per-part encoding")
			for key := range content.Encoding {
				assert.Contains(t, declared, key,
					"every encoding key addresses a property the aliased model declares")
			}

			byWire := openapitest.PropsByWire(form.Properties)
			enc, ok := content.Encoding[byWire["meta"].ID]
			require.True(t, ok, "the declared encoding entry keys the aliased model's property")
			assert.Equal(t, []string{"application/json"}, enc.ContentTypes)
			assert.True(t, content.Encoding[byWire["file"].ID].Filename,
				"the binary part is still detected through the alias")
		})
	}
}

// TestContent_MultipartBodyStandingForNoModelKeepsItsOwnPointer pins the fallback:
// a schema declaring `properties` beside a contradictory `enum` or scalar `type`
// lowers to a node that holds none of them, so no pointer can name a property
// that was never lowered. The key stays derived from the schema's own position,
// where pass.Validate reports it as addressing nothing — which is the truth about
// this document, not a false alarm about an aliased one.
func TestContent_MultipartBodyStandingForNoModelKeepsItsOwnPointer(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name        string
		mediaSchema string
		want        ir.PropID
	}{
		{
			name:        "an inline body",
			mediaSchema: `{enum: [x], properties: {file: {type: string, format: binary}}}`,
			want: ir.PropID("p/openapi" + ids.Ptr("paths", "/upload", "post", "requestBody",
				"content", "multipart/form-data", "schema", "properties", "file")),
		},
		{
			name:        "a $ref'd component",
			mediaSchema: `{$ref: "#/components/schemas/NotAModel"}`,
			want:        ir.PropID("p/openapi" + ids.Ptr("components", "schemas", "NotAModel", "properties", "file")),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			spec := openapitest.PathsSpec(`  /upload:
    post:
      operationId: upload
      requestBody:
        content:
          multipart/form-data:
            schema: ` + tc.mediaSchema + `
      responses: {"200": {description: ok}}
components:
  schemas:
    NotAModel: {enum: [x], properties: {file: {type: string, format: binary}}}
`)
			_, svc, diags := lowerServiceSpec(t, spec)
			openapitest.RequireNoErrorDiags(t, diags)
			content := openapitest.FirstOp(t, svc).Request.Contents[0]
			require.Contains(t, content.Encoding, tc.want,
				"the key falls back to the schema's own position; got %v", content.Encoding)
		})
	}
}

func TestContent_NonRequiredRequestBody(t *testing.T) {
	t.Parallel()
	spec := openapitest.PathsSpec(`  /maybe:
    post:
      operationId: maybe
      requestBody:
        content:
          application/json: {schema: {type: object, properties: {n: {type: string}}}}
      responses: {"200": {description: ok}}
`)
	_, svc, diags := lowerServiceSpec(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)
	op := openapitest.FirstOp(t, svc)
	require.NotNil(t, op.Request, "a non-required body still lowers to a present Payload")
	raw, ok := op.Request.Unmodeled["openapi:required"]
	require.True(t, ok, "body optionality kept under Unmodeled")
	assert.Equal(t, "false", string(raw.Value))
	assert.Equal(t, ir.ReasonNoIRHome, raw.Reason)
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
	spec := openapitest.PathsSpec(`  /bulk:
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
`)
	_, svc, diags := lowerServiceSpec(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)
	content := openapitest.FirstOp(t, svc).Request.Contents[0]
	tagsProp := ir.PropID("p/openapi" + ids.Ptr("paths", "/bulk", "post", "requestBody", "content", "multipart/form-data", "schema", "properties", "tags"))
	enc, ok := content.Encoding[tagsProp]
	require.True(t, ok, "array part gets a synthesized PartEncoding; got keys %v", content.Encoding)
	assert.True(t, enc.Multi, "array-typed part repeats per item")
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

	// Non-required body preserved as present with optionality under Unmodeled.
	require.NotNil(t, upload.Request)
	_, hasReq := upload.Request.Unmodeled["openapi:required"]
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
	_, hasLinks := resp.Unmodeled["openapi:links"]
	assert.True(t, hasLinks)

	assert.True(t, openapitest.HasDiag(diags, diag.DegradedConstruct))
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
		if len(ec.Unmodeled) > 0 {
			multi = ec
		}
	}
	_, hasContent := multi.Unmodeled["openapi:content"]
	assert.True(t, hasContent, "multi-media error content preserved")
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

	// Empty request-body content yields no Request payload.
	empty := openapitest.FindOp(t, doc, "emptyBody")
	assert.Nil(t, empty.Request)
}

// encodingSpec declares one form part per position an Encoding Object reaches:
// a multipart part, and a 3.2 sequential itemEncoding. Each writes the two
// things ir.PartEncoding has no field for, so both positions are exercised by
// the one fixture.
const encodingSpec = `  /form:
    post:
      operationId: postForm
      requestBody:
        required: true
        content:
          application/x-www-form-urlencoded:
            schema: {type: object, properties: {q: {type: string}}}
            encoding:
              q: {style: form, allowReserved: true, x-vendor: vvv}
      responses: {"200": {description: ok}}
`

// TestEncoding_AllowReservedAndExtensionsKeptOnTheContent pins both halves of
// GitHub #291. Neither allowReserved nor an encoding's x-* reached an IR field,
// an Unmodeled entry or a diagnostic, so two documents differing only in them
// compiled to the same IR. PartEncoding carries no Unmodeled map, so both land
// on the owning Content keyed by the part they govern.
func TestEncoding_AllowReservedAndExtensionsKeptOnTheContent(t *testing.T) {
	t.Parallel()
	_, svc, diags := lowerServiceSpec(t, openapitest.PathsSpecVer("3.1.0", encodingSpec))
	openapitest.RequireNoErrorDiags(t, diags)
	op := openapitest.FirstOp(t, svc)
	require.NotNil(t, op.Request)
	require.Len(t, op.Request.Contents, 1)
	kept := op.Request.Contents[0].Unmodeled

	at := "/paths/~1form/post/requestBody/content/application~1x-www-form-urlencoded/encoding/q"
	entry, ok := kept["openapi:encoding/q/allowReserved"]
	require.True(t, ok, "allowReserved is kept; got %v", kept)
	assert.Equal(t, ir.ReasonNoIRHome, entry.Reason)
	assert.JSONEq(t, "true", string(entry.Value))
	assert.Equal(t, at+"/allowReserved", entry.Provenance.Pointer)
	openapitest.AssertInfoDiagAt(t, diags, at+"/allowReserved")

	ext, ok := kept["openapi:encoding/q/x-vendor"]
	require.True(t, ok, "the encoding's own x-* is kept; got %v", kept)
	assert.Equal(t, ir.ReasonVendorExtension, ext.Reason)
	assert.Equal(t, at+"/x-vendor", ext.Provenance.Pointer)
}

// TestEncoding_AbsentAllowReservedRecordsNothing is the control: preservation
// keys off what the encoding declares, so an entry that omits allowReserved
// records neither an entry nor a diagnostic — rather than recording the OpenAPI
// default the accessor would hand back and calling it a source fact.
func TestEncoding_AbsentAllowReservedRecordsNothing(t *testing.T) {
	t.Parallel()
	_, svc, diags := lowerServiceSpec(t, openapitest.PathsSpecVer("3.1.0", `  /form:
    post:
      operationId: postForm
      requestBody:
        required: true
        content:
          application/x-www-form-urlencoded:
            schema: {type: object, properties: {q: {type: string}}}
            encoding:
              q: {style: form}
      responses: {"200": {description: ok}}
`))
	openapitest.RequireNoErrorDiags(t, diags)
	op := openapitest.FirstOp(t, svc)
	require.NotNil(t, op.Request)
	require.Len(t, op.Request.Contents, 1)
	assert.Empty(t, op.Request.Contents[0].Unmodeled, "nothing was declared, so nothing is kept")
	assert.Empty(t, diags)
}

// TestEncoding_OnlyUnhomedFieldsOutliveTheEmptyPartEncoding pins the ordering
// inside partEncodings. An entry declaring nothing ir.PartEncoding has a field
// for lowers to an empty one, which is dropped from Content.Encoding — so the
// entry has to be read before that check rather than after it, or what it did
// declare goes with it.
//
// Its own case because every other encoding fixture declares a style or a
// contentType, which leaves the PartEncoding non-empty and routes around the
// check entirely: moving the read below it left the whole suite green.
func TestEncoding_OnlyUnhomedFieldsOutliveTheEmptyPartEncoding(t *testing.T) {
	t.Parallel()
	_, svc, diags := lowerServiceSpec(t, openapitest.PathsSpecVer("3.1.0", `  /form:
    post:
      operationId: postForm
      requestBody:
        required: true
        content:
          application/x-www-form-urlencoded:
            schema: {type: object, properties: {q: {type: string}}}
            encoding:
              q: {allowReserved: true, x-vendor: vvv}
      responses: {"200": {description: ok}}
`))
	openapitest.RequireNoErrorDiags(t, diags)
	op := openapitest.FirstOp(t, svc)
	require.NotNil(t, op.Request)
	require.Len(t, op.Request.Contents, 1)
	content := op.Request.Contents[0]
	require.Empty(t, content.Encoding,
		"a string part declaring neither style nor contentType lowers to an empty PartEncoding")

	entry, ok := content.Unmodeled["openapi:encoding/q/allowReserved"]
	require.True(t, ok, "allowReserved outlives the entry it was declared in; got %v", content.Unmodeled)
	assert.Equal(t, ir.ReasonNoIRHome, entry.Reason)
	assert.JSONEq(t, "true", string(entry.Value))
	assert.Contains(t, content.Unmodeled, "openapi:encoding/q/x-vendor", "and so does its own x-*")
}

// TestEncoding_PartNameWithSlashKeepsItsOwnKey pins that a part's name is one
// scope segment however it is spelled. The scope is "encoding/<part>" and the
// part is a schema property name, so a "/" in it used to read as a separator:
// parts "q" and "q/x-a" spelled one key between them, the entry that survived
// followed the order the properties were declared in, and neither order said
// anything about it.
//
// Both halves are needed to state it. The two entries must be distinct, and each
// must hold the value its own part declared — asserting only that two keys exist
// would pass on a lowering that swapped them.
func TestEncoding_PartNameWithSlashKeepsItsOwnKey(t *testing.T) {
	t.Parallel()
	_, svc, diags := lowerServiceSpec(t, openapitest.PathsSpecVer("3.1.0", `  /form:
    post:
      operationId: postForm
      requestBody:
        required: true
        content:
          application/x-www-form-urlencoded:
            schema:
              type: object
              properties:
                q: {type: string}
                "q/x-a": {type: string}
            encoding:
              q: {style: form, x-a/x-b: FROM_Q}
              "q/x-a": {style: form, x-b: FROM_Q_SLASH_XA}
      responses: {"200": {description: ok}}
`))
	openapitest.RequireNoErrorDiags(t, diags)
	kept := openapitest.FirstOp(t, svc).Request.Contents[0].Unmodeled

	plain, ok := kept["openapi:encoding/q/x-a/x-b"]
	require.True(t, ok, `the part named "q" keeps its own x-a/x-b; got %v`, kept)
	assert.JSONEq(t, `"FROM_Q"`, string(plain.Value))

	slashed, ok := kept["openapi:encoding/q~1x-a/x-b"]
	require.True(t, ok, `the part named "q/x-a" keeps its own x-b under an escaped scope; got %v`, kept)
	assert.JSONEq(t, `"FROM_Q_SLASH_XA"`, string(slashed.Value))
}

// TestEncoding_ItemEncodingKeepsItsOwnUnderItsOwnKey is the other position the
// same reader serves. A content can hold both a per-part encoding map and a
// sequential itemEncoding, and both reach one Unmodeled map, so the key has to
// tell them apart.
func TestEncoding_ItemEncodingKeepsItsOwnUnderItsOwnKey(t *testing.T) {
	t.Parallel()
	_, svc, diags := lowerServiceSpec(t, openapitest.PathsSpecVer("3.2.0", `  /stream:
    get:
      operationId: streamEvents
      responses:
        "200":
          description: ok
          content:
            multipart/mixed:
              itemSchema: {type: object, properties: {n: {type: string}}}
              itemEncoding: {contentType: application/json, allowReserved: true, x-vendor: vvv}
`))
	openapitest.RequireNoErrorDiags(t, diags)
	op := openapitest.FirstOp(t, svc)
	require.NotEmpty(t, op.Responses)
	require.NotNil(t, op.Responses[0].Payload)
	require.Len(t, op.Responses[0].Payload.Contents, 1)
	kept := op.Responses[0].Payload.Contents[0].Unmodeled

	assert.Contains(t, kept, "openapi:itemEncoding/allowReserved")
	assert.Contains(t, kept, "openapi:itemEncoding/x-vendor")
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

// opByPath finds an operation by HTTP method and URI template.
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
		op := openapitest.FindOp(t, doc, name)
		require.NotNil(t, op.Request, "%s has a request", name)
		for _, c := range op.Request.Contents {
			assert.Empty(t, c.Encoding, "%s multipart yields no per-part encoding", name)
		}
	}
}

func TestContent_ExampleWithoutValueSkipped(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, multipartVariantsSpec)
	op := openapitest.FindOp(t, doc, "exGet")
	c := op.Responses[0].Payload.Contents[0]
	assert.Empty(t, c.Examples, "an example carrying no example is skipped")

	// Skipped, but not in silence: the entry declares neither a value nor the
	// externalValue that would have given it a home.
	d, ok := openapitest.FirstDegradedWarning(diags)
	require.True(t, ok, "the skipped entry is reported")
	assert.Equal(t, "/paths/~1examples/get/responses/200/content/application~1json/examples/empty",
		d.Provenance.Pointer)
}

func TestContent_UnconvertibleExamplesDiagnosed(t *testing.T) {
	t.Parallel()
	// Covers both the single 3.0-style `example` and one entry of the plural
	// 3.1-style `examples` map — each carries a custom, structurally
	// unconvertible tag, and each conversion failure must be diagnosed rather
	// than discarded silently.
	spec := openapitest.PathsSpec(`  /items:
    get:
      operationId: getItem
      responses:
        "200":
          description: ok
          content:
            application/json:
              schema: {type: string}
              example: !foo bar
              examples:
                one: {value: !foo baz}
`)
	_, svc, diags := lowerServiceSpec(t, spec)
	op := openapitest.FirstOp(t, svc)
	require.Len(t, op.Responses, 1)
	c := op.Responses[0].Payload.Contents[0]
	assert.Empty(t, c.Examples, "both unconvertible examples are skipped, not appended")

	require.Equal(t, 2, openapitest.CountDiagsAt(diags, diag.DegradedConstruct, ir.SeverityWarning))
	pointers := map[string]bool{}
	for _, d := range diags {
		if d.Code == diag.DegradedConstruct && d.Severity == ir.SeverityWarning {
			pointers[d.Provenance.Pointer] = true
			assert.Contains(t, d.Message, "example:")
		}
	}
	const base = "/paths/~1items/get/responses/200/content/application~1json"
	assert.True(t, pointers[base+"/example"], "the single example's pointer")
	assert.True(t, pointers[base+"/examples/one/value"], "the plural example's pointer, keyed by its name")
}

func TestContent_RefdExampleDiagnosedAtReferenceSite(t *testing.T) {
	t.Parallel()
	// An `examples` entry written as a $ref holds no value of its own — the
	// value lives in the referenced component — so `.../examples/<name>/value`
	// would name a location the source never had. The diagnostic belongs at the
	// reference site. The convertible sibling pins that a $ref'd example still
	// lowers normally.
	spec := `openapi: 3.1.0
info: {title: T, version: "1.0.0"}
paths:
  /items:
    get:
      operationId: getItem
      responses:
        "200":
          description: ok
          content:
            application/json:
              schema: {type: string}
              examples:
                good: {$ref: '#/components/examples/Good'}
                bad: {$ref: '#/components/examples/Bad'}
components:
  examples:
    Good: {value: fine}
    Bad: {value: !foo baz}
`
	_, svc, diags := lowerServiceSpec(t, spec)
	op := openapitest.FirstOp(t, svc)
	c := op.Responses[0].Payload.Contents[0]
	require.Len(t, c.Examples, 1, "the convertible $ref'd example still lowers")
	require.NotNil(t, c.Examples[0].Value)
	assert.Equal(t, ir.Value{Kind: ir.ValueString, Str: "fine"}, *c.Examples[0].Value)

	require.Equal(t, 1, openapitest.CountDiagsAt(diags, diag.DegradedConstruct, ir.SeverityWarning))
	d, ok := openapitest.FirstDegradedWarning(diags)
	require.True(t, ok)
	assert.Equal(t, "/paths/~1items/get/responses/200/content/application~1json/examples/bad",
		d.Provenance.Pointer, "the reference site, not a /value the source never had")
	assert.Contains(t, d.Message, "example:")
}

// positionalEncodingSpec declares the 3.2 shape ItemEncoding cannot state:
// prefixEncoding fixes the encoding of the leading items, so the itemEncoding
// beside it governs only the tail after them, not every item.
const positionalEncodingSpec = `openapi: 3.2.0
info: {title: T, version: "1"}
paths:
  /mixed:
    get:
      operationId: mixed
      responses:
        "200":
          description: ok
          content:
            multipart/mixed:
              itemSchema: {type: object, properties: {a: {type: string}}}
              prefixEncoding:
                - contentType: application/json
                - contentType: application/xml
              itemEncoding: {contentType: text/plain}
`

func TestContent_PositionalPrefixEncodingIsPreserved(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, positionalEncodingSpec)
	c := openapitest.FindOp(t, doc, "mixed").Responses[0].Payload.Contents[0]

	assert.Nil(t, c.ItemEncoding, "positional prefixes rule out an every-item encoding")
	prefix, ok := c.Unmodeled["openapi:prefixEncoding"]
	require.True(t, ok, "prefixEncoding kept verbatim; got %v", c.Unmodeled)
	assert.Equal(t, ir.ReasonNoIRHome, prefix.Reason)
	assert.JSONEq(t, `[{"contentType": "application/json"}, {"contentType": "application/xml"}]`,
		string(prefix.Value))
	item, ok := c.Unmodeled["openapi:itemEncoding"]
	require.True(t, ok, "the tail encoding is kept beside the prefixes it follows")
	assert.JSONEq(t, `{"contentType": "text/plain"}`, string(item.Value))
	openapitest.AssertHasCode(t, diags, diag.DegradedConstruct, ir.SeverityInfo)
}

func TestFillSequential_PrefixEncodingWithoutItemEncoding(t *testing.T) {
	t.Parallel()
	spec := strings.ReplaceAll(positionalEncodingSpec, "              itemEncoding: {contentType: text/plain}\n", "")
	doc, diags := parseFull(t, spec)
	c := openapitest.FindOp(t, doc, "mixed").Responses[0].Payload.Contents[0]

	assert.Nil(t, c.ItemEncoding)
	_, ok := c.Unmodeled["openapi:prefixEncoding"]
	assert.True(t, ok, "prefixEncoding alone is still reported rather than dropped")
	_, ok = c.Unmodeled["openapi:itemEncoding"]
	assert.False(t, ok, "no itemEncoding was declared, so none is recorded")
	openapitest.AssertHasCode(t, diags, diag.DegradedConstruct, ir.SeverityInfo)
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

// TestContent_RequestBodyRefSharedAcrossOperationsInternsOnce is the fix's
// core scenario for a referenced requestBody component (issue #107): two
// operations $ref'ing one #/components/requestBodies/Body must intern its
// content schema once, at the component's own declaration pointer.
func TestContent_RequestBodyRefSharedAcrossOperationsInternsOnce(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, componentBodyRefSpec)
	openapitest.RequireNoErrorDiags(t, diags)
	postA := openapitest.FindOp(t, doc, "postA")
	postB := openapitest.FindOp(t, doc, "postB")
	require.NotNil(t, postA.Request)
	require.NotNil(t, postB.Request)

	wantID := ir.TypeID("t/anon/components/requestBodies/Body/content/application~1json/schema")
	assert.Equal(t, wantID, postA.Request.Contents[0].Type.Target)
	assert.Equal(t, wantID, postB.Request.Contents[0].Type.Target,
		"both operations resolve the same shared body schema")

	_, fabricatedA := doc.Types[ir.TypeID("t/anon/paths/~1a/post/requestBody/content/application~1json/schema")]
	_, fabricatedB := doc.Types[ir.TypeID("t/anon/paths/~1b/post/requestBody/content/application~1json/schema")]
	assert.False(t, fabricatedA, "no fabricated per-operation ID for /a")
	assert.False(t, fabricatedB, "no fabricated per-operation ID for /b")
}

const refdResponseNestedHeaderSpec = `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /a:
    get:
      operationId: getA
      responses:
        "200": {$ref: '#/components/responses/OK'}
components:
  responses:
    OK:
      description: ok
      headers:
        X-Rate: {$ref: '#/components/headers/Rate'}
  headers:
    Rate: {schema: {type: string, enum: [a, b]}}
`

// TestContent_RefdResponseHeaderNestedRefInternsSchemaOnce covers the nested
// hop of issue #107: a $ref'd response whose own headers entry is itself a
// $ref to a header component must still hoist the header's schema once, at
// the header component's own declaration pointer.
func TestContent_RefdResponseHeaderNestedRefInternsSchemaOnce(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, refdResponseNestedHeaderSpec)
	openapitest.RequireNoErrorDiags(t, diags)
	op := openapitest.FindOp(t, doc, "getA")
	require.Len(t, op.Responses, 1)
	require.Len(t, op.Responses[0].Headers, 1)

	wantID := ir.TypeID("t/anon/components/headers/Rate/schema")
	assert.Equal(t, wantID, op.Responses[0].Headers[0].Type.Target)
	_, ok := doc.Types[wantID]
	require.True(t, ok, "the header's schema is registered under the header component's own pointer")
}

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

// TestContent_HeaderMapEntriesSharingComponentGetDistinctIDs covers the
// header-identity half of issue #107: two response headers under different
// map keys that both $ref the same header component keep distinct PropIDs —
// the map entry, not the schema, identifies each header — while sharing one
// Type.Target.
func TestContent_HeaderMapEntriesSharingComponentGetDistinctIDs(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, headerIdentitySpec)
	openapitest.RequireNoErrorDiags(t, diags)
	op := openapitest.FindOp(t, doc, "getA")
	require.Len(t, op.Responses[0].Headers, 2)
	byWire := openapitest.IndexBy(op.Responses[0].Headers, func(p ir.Property) string { return p.WireName })

	rate, limit := byWire["X-Rate"], byWire["X-Limit"]
	assert.NotEqual(t, rate.ID, limit.ID, "distinct map keys keep distinct PropIDs")
	assert.Equal(t, ir.PropID("p/openapi/paths/~1a/get/responses/200/headers/X-Rate"), rate.ID)
	assert.Equal(t, ir.PropID("p/openapi/paths/~1a/get/responses/200/headers/X-Limit"), limit.ID)
	assert.Equal(t, rate.Type.Target, limit.Type.Target, "both resolve the same shared header schema")
	assert.Equal(t, ir.TypeID("t/anon/components/headers/Rate/schema"), rate.Type.Target)
}

// TestContent_SharedComponentSchemaTakesItsDeclarationHint pins where the name
// hint of a $ref'd component's schema comes from. The node interns once, at the
// component's declaration, so a use-site hint — the referencing operationId for
// a body, the map key for a header — would name the one shared node after
// whichever reference happened to lower first. Naming.Hint is what emitters
// render from, so "postA_request" on a body two operations share is a wrong
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

const refdEncodingHeaderSpec = `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /u:
    post:
      operationId: upload
      requestBody:
        content:
          multipart/form-data:
            schema:
              type: object
              properties:
                file: {type: string, format: binary}
            encoding:
              file:
                headers:
                  X-Rate: {$ref: '#/components/headers/Rate'}
      responses: {"200": {description: ok}}
components:
  headers:
    Rate: {schema: {type: string, enum: [a, b]}}
`

// TestContent_EncodingHeaderRefInternsAtDeclaration covers lowerHeaders' other
// caller: a multipart part's per-encoding headers. A $ref'd header there must
// split the same way a response header does — schema at the component, identity
// (PropID, provenance) at the encoding entry that binds the name (issue #107).
func TestContent_EncodingHeaderRefInternsAtDeclaration(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, refdEncodingHeaderSpec)
	openapitest.RequireNoErrorDiags(t, diags)
	op := openapitest.FindOp(t, doc, "upload")
	require.NotNil(t, op.Request)
	require.Len(t, op.Request.Contents, 1)
	enc := op.Request.Contents[0].Encoding
	require.Len(t, enc, 1)

	var headers []ir.Property
	for _, pe := range enc {
		headers = pe.Headers
	}
	require.Len(t, headers, 1)
	assert.Equal(t, ir.TypeID("t/anon/components/headers/Rate/schema"), headers[0].Type.Target,
		"the encoding header's schema interns at the header component")
	assert.Equal(t,
		ir.PropID("p/openapi/paths/~1u/post/requestBody/content/multipart~1form-data/encoding/file/headers/X-Rate"),
		headers[0].ID, "the encoding entry that binds the name keeps the header's identity")
	assert.Equal(t, headers[0].Provenance.Pointer, string(headers[0].ID)[len("p/openapi"):],
		"provenance tracks the same use-site pointer as the ID")
}

// TestHeaders_SchemaDetailReachesTheProperty asserts a header's schema is read
// with the same detail a model property's is. lowerHeaders built an ir.Property
// and never filled it, so a header schema dropped its docs, xml, extensions,
// validation-only keywords and value constraints even though ir.Property has a
// field for each (GitHub #116).
func TestHeaders_SchemaDetailReachesTheProperty(t *testing.T) {
	t.Parallel()
	_, svc, diags := lowerServiceSpec(t, openapitest.PathsSpec(
		"  /x:\n    get:\n      operationId: g\n      responses:\n"+
			"        \"200\":\n          description: ok\n          headers:\n"+
			"            X-H: {schema: "+openapitest.InlineProbeBody+"}\n"))
	openapitest.RequireNoErrorDiags(t, diags)

	h := openapitest.FirstOp(t, svc).Responses[0].Headers[0]
	openapitest.AssertProbeDocsKept(t, h.Docs)
	assert.NotNil(t, h.Deprecation)
	openapitest.AssertProbeExample(t, h.Examples)
	require.NotNil(t, h.XML)
	assert.Equal(t, "X", h.XML.Name)
	assert.Contains(t, h.Unmodeled, "openapi:x-vendor")
	assert.Contains(t, h.Unmodeled, "openapi:not")
	require.NotNil(t, h.Constraints)
	require.NotNil(t, h.Constraints.MaxLength)
	assert.Equal(t, int64(3), *h.Constraints.MaxLength)
}

// TestHeaders_OwnAnnotationsOverrideTheSchema checks the precedence between the
// two annotation sources a header has: what the header object writes about
// itself is more specific than what its schema writes about the type, so it
// wins where both are set.
//
// Every annotation applyHeaderAnnotations can override is written on both sides
// here, with a value naming the side it came from. A keyword only one side
// declares proves nothing about precedence: it reaches the header in either
// overlay order, so the assertion over it passes whichever side wins.
func TestHeaders_OwnAnnotationsOverrideTheSchema(t *testing.T) {
	t.Parallel()
	_, svc, diags := lowerServiceSpec(t, openapitest.PathsSpec(
		"  /x:\n    get:\n      operationId: g\n      responses:\n"+
			"        \"200\":\n          description: ok\n          headers:\n"+
			"            X-H:\n              description: HEADER\n              example: HEADER\n"+
			"              x-scope: header\n              schema:\n"+
			"                {type: string, description: SCHEMA, example: SCHEMA, x-scope: schema}\n"))
	openapitest.RequireNoErrorDiags(t, diags)

	h := openapitest.FirstOp(t, svc).Responses[0].Headers[0]
	assert.Equal(t, "HEADER", h.Docs.Description, "the header's own description wins")
	require.Len(t, h.Examples, 1, "and its own example replaces the schema's rather than joining it")
	require.NotNil(t, h.Examples[0].Value)
	assert.Equal(t, "HEADER", h.Examples[0].Value.Str)
	require.Contains(t, h.Unmodeled, "openapi:x-scope")
	assert.JSONEq(t, `"header"`, string(h.Unmodeled["openapi:x-scope"].Value),
		"and its own value for a vendor key the schema also writes")
}

// TestHeaders_DeprecationUnionsWithTheSchema pins the one annotation a header
// does not override. Both overlays only ever set the flag, so `deprecated`
// written on either side deprecates the header and neither side can clear the
// other: a header's own `deprecated: false` does not un-deprecate a type its
// schema marks deprecated. That is a union rather than the precedence the test
// above covers, which is why deprecation is asserted here and not there.
func TestHeaders_DeprecationUnionsWithTheSchema(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, header, schema string }{
		{"declared on the header, denied by the schema", "true", "false"},
		{"declared on the schema, denied by the header", "false", "true"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, svc, diags := lowerServiceSpec(t, openapitest.PathsSpec(
				"  /x:\n    get:\n      operationId: g\n      responses:\n"+
					"        \"200\":\n          description: ok\n          headers:\n"+
					"            X-H:\n              deprecated: "+tc.header+"\n"+
					"              schema: {type: string, deprecated: "+tc.schema+"}\n"))
			openapitest.RequireNoErrorDiags(t, diags)

			h := openapitest.FirstOp(t, svc).Responses[0].Headers[0]
			assert.NotNil(t, h.Deprecation, "either side alone deprecates the header")
		})
	}
}

// TestHeaders_SerializationKeywordsKept covers the two keywords a header object
// writes that ir.Property has no field for. explode decides whether a
// collection-valued header goes on the wire as one repeated field or one joined
// value, so dropping it left the IR unable to say how the header serializes —
// and it was dropped in silence, at both of lowerHeaders' callers.
//
// Both callers are exercised, because the loss belonged to the shared lowering
// rather than to the response position it was noticed at: a multipart part's
// per-encoding headers build the same ir.Property from the same function.
func TestHeaders_SerializationKeywordsKept(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, spec, at string }{
		{
			name: "response header",
			spec: openapitest.PathsSpec("  /x:\n    get:\n      operationId: g\n      responses:\n" +
				"        \"200\":\n          description: ok\n          headers:\n" +
				"            X-H:\n              style: simple\n              explode: true\n" +
				"              schema: {type: array, items: {type: string}}\n"),
			at: "/paths/~1x/get/responses/200/headers/X-H",
		},
		{
			name: "multipart encoding header",
			spec: openapitest.PathsSpec("  /x:\n    post:\n      operationId: g\n      requestBody:\n" +
				"        content:\n          multipart/form-data:\n" +
				"            schema: {type: object, properties: {file: {type: string}}}\n" +
				"            encoding:\n              file:\n                headers:\n" +
				"                  X-H:\n                    style: simple\n" +
				"                    explode: true\n" +
				"                    schema: {type: array, items: {type: string}}\n" +
				"      responses: {\"200\": {description: ok}}\n"),
			at: "/paths/~1x/post/requestBody/content/multipart~1form-data/encoding/file/headers/X-H",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, svc, diags := lowerServiceSpec(t, tc.spec)
			openapitest.RequireNoErrorDiags(t, diags)
			assertHeaderSerializationKept(t, headerAt(t, openapitest.FirstOp(t, svc)), diags, tc.at)
		})
	}
}

// headerAt returns the one header the fixtures above declare, whichever of the
// two positions carries it.
func headerAt(t *testing.T, op ir.Operation) ir.Property {
	t.Helper()
	if len(op.Responses) > 0 && len(op.Responses[0].Headers) > 0 {
		return op.Responses[0].Headers[0]
	}
	require.NotNil(t, op.Request)
	require.Len(t, op.Request.Contents, 1)
	for _, pe := range op.Request.Contents[0].Encoding {
		require.Len(t, pe.Headers, 1)
		return pe.Headers[0]
	}
	t.Fatal("no header at either position")
	return ir.Property{}
}

// assertHeaderSerializationKept requires both keywords to be kept verbatim at
// their own coordinates under the header at `at`, each announced by a diagnostic.
func assertHeaderSerializationKept(t *testing.T, h ir.Property, diags []ir.Diagnostic, at string) {
	t.Helper()
	for key, want := range map[string]string{"style": `"simple"`, "explode": `true`} {
		entry, ok := h.Unmodeled["openapi:"+key]
		require.True(t, ok, "%s is kept", key)
		assert.Equal(t, ir.ReasonNoIRHome, entry.Reason)
		assert.JSONEq(t, want, string(entry.Value))
		assert.Equal(t, at+"/"+key, entry.Provenance.Pointer)
		openapitest.AssertInfoDiagAt(t, diags, entry.Provenance.Pointer)
	}
}

// TestHeaders_SerializationKeywordsAbsentRecordNothing is the control for the
// test above: preservation keys off what the header declares, so a header that
// declares neither keyword records neither — rather than recording the OpenAPI
// defaults the accessors would hand back and calling them source facts.
func TestHeaders_SerializationKeywordsAbsentRecordNothing(t *testing.T) {
	t.Parallel()
	_, svc, diags := lowerServiceSpec(t, openapitest.PathsSpec(
		"  /x:\n    get:\n      operationId: g\n      responses:\n"+
			"        \"200\":\n          description: ok\n          headers:\n"+
			"            X-H: {schema: {type: string}}\n"))
	openapitest.RequireNoErrorDiags(t, diags)

	h := openapitest.FirstOp(t, svc).Responses[0].Headers[0]
	assert.NotContains(t, h.Unmodeled, "openapi:style")
	assert.NotContains(t, h.Unmodeled, "openapi:explode")
	assert.Empty(t, diags, "and nothing is announced about keywords the header never wrote")
}

// TestHeaders_ReservedContentTypeEntryIsReported covers the headers-map half of
// the rule diag.ReservedHeaderName records. OpenAPI states "SHALL be ignored"
// for a reserved header name at three positions, not one: a header parameter
// (§4.8.12), a Content-Type entry in a response's headers map (§4.8.17), and a
// Content-Type entry in an encoding's (§4.8.15). Morphic lowers all three
// anyway, because dropping declared content is a loss and the choice belongs to
// an emitter — but doing that in silence is what leaves an emitter unable to
// tell such a header from any other, and generating one that restates the media
// type the position already owns.
//
// Both headers-map positions are exercised, because the rule belongs to the
// shared lowering rather than the response position: an encoding's per-part
// headers reach lowerHeaders from the other caller.
func TestHeaders_ReservedContentTypeEntryIsReported(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, header, at string
		reported         bool
	}{
		{"response Content-Type", "Content-Type",
			"/paths/~1x/get/responses/200/headers/Content-Type", true},
		{"response lowercase spelling", "content-type",
			"/paths/~1x/get/responses/200/headers/content-type", true},
		{"response ordinary header", "X-Trace",
			"/paths/~1x/get/responses/200/headers/X-Trace", false},
		{"response Accept is not reserved here", "Accept",
			"/paths/~1x/get/responses/200/headers/Accept", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, svc, diags := lowerServiceSpec(t, openapitest.PathsSpec(
				"  /x:\n    get:\n      operationId: g\n      responses:\n"+
					"        \"200\":\n          description: ok\n          headers:\n"+
					"            "+tc.header+": {schema: {type: string}}\n"))
			openapitest.RequireNoErrorDiags(t, diags)

			headers := openapitest.FirstOp(t, svc).Responses[0].Headers
			require.Len(t, headers, 1, "the header lowers either way; nothing is dropped")
			assert.Equal(t, tc.header, headers[0].WireName)
			assert.Equal(t, tc.reported, openapitest.HasDiagCodeAt(diags, diag.ReservedHeaderName, tc.at),
				"reported at the map entry's own pointer")
		})
	}
}

// TestHeaders_ReservedContentTypeInEncodingIsReported is the same rule at the
// other caller of lowerHeaders: an encoding's headers map, which §4.8.15 says
// describes Content-Type separately and SHALL ignore an entry for it.
func TestHeaders_ReservedContentTypeInEncodingIsReported(t *testing.T) {
	t.Parallel()
	_, _, diags := lowerServiceSpec(t, openapitest.PathsSpec(
		"  /x:\n    post:\n      operationId: g\n      requestBody:\n"+
			"        content:\n          multipart/form-data:\n"+
			"            schema: {type: object, properties: {file: {type: string}}}\n"+
			"            encoding:\n              file:\n                headers:\n"+
			"                  Content-Type: {schema: {type: string}}\n"+
			"                  X-Other: {schema: {type: string}}\n"+
			"      responses: {\"200\": {description: ok}}\n"))
	openapitest.RequireNoErrorDiags(t, diags)

	base := "/paths/~1x/post/requestBody/content/multipart~1form-data/encoding/file/headers/"
	assert.True(t, openapitest.HasDiagCodeAt(diags, diag.ReservedHeaderName, base+"Content-Type"))
	assert.False(t, openapitest.HasDiagCodeAt(diags, diag.ReservedHeaderName, base+"X-Other"),
		"the entry beside it is ordinary and says nothing")
	openapitest.AssertHasCode(t, diags, diag.ReservedHeaderName, ir.SeverityWarning)
}

// TestHeaders_ReservedNameIsTheKeyNotTheDeclaration pins which of two pointers
// the report lands on. The reserved thing is the key the header is mapped under,
// not the header object: one component $ref'd from a reserved key and an
// ordinary one is two declarations, and only the reserved key is reported —
// reporting at the shared declaration instead would collapse them into one.
func TestHeaders_ReservedNameIsTheKeyNotTheDeclaration(t *testing.T) {
	t.Parallel()
	_, _, diags := lowerServiceSpec(t, `openapi: 3.1.0
info: {title: T, version: "1"}
components:
  headers:
    Shared: {schema: {type: string}}
paths:
  /x:
    get:
      operationId: g
      responses:
        "200":
          description: ok
          headers:
            Content-Type: {$ref: '#/components/headers/Shared'}
            X-Other: {$ref: '#/components/headers/Shared'}
`)
	openapitest.RequireNoErrorDiags(t, diags)

	base := "/paths/~1x/get/responses/200/headers/"
	assert.True(t, openapitest.HasDiagCodeAt(diags, diag.ReservedHeaderName, base+"Content-Type"),
		"the reserved key is reported at its own use site")
	assert.False(t, openapitest.HasDiagCodeAt(diags, diag.ReservedHeaderName, base+"X-Other"),
		"the other key sharing that declaration is not")
	assert.False(t, openapitest.HasDiagCodeAt(diags, diag.ReservedHeaderName, "/components/headers/Shared"),
		"and nothing is reported at the declaration they share")
}

// TestSingleContentEntry_ReportsExtraMediaTypes covers the invalid document
// OpenAPI forbids: a content-style header or parameter declaring more than one
// media type. Only the first can lower — ir.Property and ir.Parameter each hold
// one type — so the rest were dropped without a word until the position that
// takes the first started naming what it left (GitHub #139).
func TestSingleContentEntry_ReportsExtraMediaTypes(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		spec string
		at   string
	}{
		{
			name: "response header",
			spec: openapitest.PathsSpec("  /x:\n    get:\n      responses:\n" +
				"        \"200\":\n          description: ok\n          headers:\n" +
				"            H:\n              content:\n" +
				"                application/xml: {schema: {type: string}}\n" +
				"                application/json: {schema: {type: integer}}\n"),
			at: "/paths/~1x/get/responses/200/headers/H/content",
		},
		{
			name: "operation parameter",
			spec: openapitest.PathsSpec("  /x:\n    get:\n      parameters:\n" +
				"        - name: p\n          in: query\n          content:\n" +
				"              application/xml: {schema: {type: string}}\n" +
				"              application/json: {schema: {type: integer}}\n" +
				"      responses: {\"200\": {description: ok}}\n"),
			at: "/paths/~1x/get/parameters/0/content",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, diags := parseFull(t, tc.spec)
			assert.True(t, openapitest.HasDiagCodeAt(diags, diag.DegradedConstruct, tc.at),
				"the ignored media types are named at the content map: %+v", diags)
			assert.Contains(t, openapitest.DiagMessageAt(t, diags, diag.DegradedConstruct, ir.SeverityWarning, tc.at),
				"application/json", "the message names what was ignored, not only that something was")
		})
	}
}

// TestSingleContentEntry_OneEntryIsSilent is the control: the legal one-entry
// spelling must not warn, or every content-style header and parameter would.
func TestSingleContentEntry_OneEntryIsSilent(t *testing.T) {
	t.Parallel()
	_, diags := parseFull(t, openapitest.PathsSpec("  /x:\n    get:\n      responses:\n"+
		"        \"200\":\n          description: ok\n          headers:\n"+
		"            H:\n              content:\n"+
		"                application/xml: {schema: {type: string}}\n"))
	assert.Equal(t, 0, openapitest.CountDiagsAt(diags, diag.DegradedConstruct, ir.SeverityWarning),
		"one media type is the legal spelling: %+v", diags)
}

// TestHeaderSchema_NeitherSpelling covers a header that declares no type at all.
// It is legal — a header may carry only a description — and must lower to the top
// type without reporting a loss, since nothing was written to lose.
func TestHeaderSchema_NeitherSpelling(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, openapitest.PathsSpec("  /x:\n    get:\n      operationId: untypedHeader\n      responses:\n"+
		"        \"200\":\n          description: ok\n          headers:\n"+
		"            H: {description: untyped}\n"))
	openapitest.RequireNoErrorDiags(t, diags)

	headers := openapitest.FindOp(t, doc, "untypedHeader").Responses[0].Headers
	require.Len(t, headers, 1)
	assert.Equal(t, ir.TypeID("t/prim/any"), headers[0].Type.Target,
		"a header declaring no type lowers to the top type")
	assert.Nil(t, headers[0].Encoding, "no content map means no media type")
	assert.Equal(t, "untyped", headers[0].Docs.Description)
}

// TestPartEncodings_MixinPartIsKeyedOnTheMixin covers the second composition
// channel. An allOf of two $refs makes the first the Base and the rest Mixins
// (§4.3), so a part contributed by the second is reachable only by searching
// them — and its key must be the ID that mixin declares.
func TestPartEncodings_MixinPartIsKeyedOnTheMixin(t *testing.T) {
	t.Parallel()
	spec := openapitest.PathsSpec(`  /upload:
    post:
      operationId: upload
      requestBody:
        content:
          multipart/form-data:
            schema:
              allOf:
                - {$ref: "#/components/schemas/Core"}
                - {$ref: "#/components/schemas/Extra"}
            encoding:
              note: {contentType: text/plain}
      responses: {"200": {description: ok}}
components:
  schemas:
    Core:
      type: object
      properties: {id: {type: string}}
    Extra:
      type: object
      properties: {note: {type: string}}
`)
	_, svc, diags := lowerServiceSpec(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)
	enc := openapitest.FirstOp(t, svc).Request.Contents[0].Encoding

	want := ir.PropID("p/openapi" + ids.Ptr("components", "schemas", "Extra", "properties", "note"))
	pe, ok := enc[want]
	require.True(t, ok, "a mixin's part is keyed on the mixin that declares it; got %v", enc)
	assert.Equal(t, []string{"text/plain"}, pe.ContentTypes)
}

// TestBodyParts_RedeclaredNameIsOnePart pins that a part named by two allOf
// branches yields one entry rather than depending on which branch was walked
// last. The encoding entry describes one part on the wire either way.
func TestBodyParts_RedeclaredNameIsOnePart(t *testing.T) {
	t.Parallel()
	spec := openapitest.PathsSpec(`  /upload:
    post:
      operationId: upload
      requestBody:
        content:
          multipart/form-data:
            schema:
              allOf:
                - {type: object, properties: {note: {type: string}}}
                - {type: object, properties: {note: {type: string}}}
            encoding:
              note: {contentType: text/plain}
      responses: {"200": {description: ok}}
`)
	_, svc, diags := lowerServiceSpec(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)
	enc := openapitest.FirstOp(t, svc).Request.Contents[0].Encoding
	assert.Len(t, enc, 1, "one wire part is one encoding entry; got %v", enc)
}

// TestPartEncoding_NamesAPropertyInheritedFromTheBase pins how a multipart
// encoding entry finds its part when the body is a composition. The name it
// keys is a wire name, and the property carrying it may be declared on a base
// rather than on the composed model itself — so the lookup descends the
// composition instead of reading only the model's own properties, which would
// silently drop the encoding.
func TestPartEncoding_NamesAPropertyInheritedFromTheBase(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /u:
    post:
      operationId: upload
      requestBody:
        content:
          multipart/form-data:
            schema:
              allOf: [{$ref: '#/components/schemas/Base'}]
            encoding:
              file: {contentType: image/png}
      responses: {"204": {description: ok}}
components:
  schemas:
    Base:
      type: object
      properties: {file: {type: string, format: binary}}
`)
	openapitest.RequireNoErrorDiags(t, diags)

	body := openapitest.FindOp(t, doc, "upload").Request
	require.NotNil(t, body)
	require.Len(t, body.Contents, 1)
	content := body.Contents[0]
	require.Len(t, content.Encoding, 1, "the encoding entry survives: %+v", content.Encoding)

	// Which key it is under is the assertion that matters. A lookup that fails to
	// descend still produces an entry — partPropID derives an ID from the composed
	// node's own pointer — and that ID names a property declared nowhere. The
	// encoding reads as present either way; only the key tells the two apart.
	composed, isModel := doc.Types[content.Type.Target].(*ir.Model)
	require.True(t, isModel, "the multipart body lowers to a model")
	require.NotNil(t, composed.Base, "a sole $ref branch becomes the composed model's base")
	inherited, isModel := doc.Types[composed.Base.Target].(*ir.Model)
	require.True(t, isModel, "and the base is the referenced model")
	want := openapitest.PropsByWire(inherited.Properties)["file"].ID
	require.NotEmpty(t, want, "the base declares the part this encoding names")

	assert.Contains(t, content.Encoding, want,
		"the encoding is keyed by the inherited property's own ID, not one derived for it")
	assert.Equal(t, []string{"image/png"}, content.Encoding[want].ContentTypes)
}

// TestExample_ExternalValueOnlyIsCarried pins the half of an example the IR can
// hold without a value. An example may declare a URL instead of the data, and
// that is a complete example rather than a degraded one — so it is appended
// like any other, and only an example declaring neither is reported.
func TestExample_ExternalValueOnlyIsCarried(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, openapitest.PathsSpec(`  /a:
    get:
      operationId: getA
      responses:
        "200":
          description: ok
          content:
            application/json:
              schema: {type: string}
              examples:
                remote: {externalValue: 'https://e.example/one.json', summary: s}
`))
	openapitest.RequireNoErrorDiags(t, diags)

	resp := openapitest.FindOp(t, doc, "getA").Responses
	require.NotEmpty(t, resp)
	require.NotEmpty(t, resp[0].Payload.Contents)
	examples := resp[0].Payload.Contents[0].Examples
	require.Len(t, examples, 1, "an externalValue-only example is kept: %+v", examples)
	assert.Equal(t, "https://e.example/one.json", examples[0].ExternalURL)
	assert.Nil(t, examples[0].Value, "and carries no inline value")
}

// TestHeaders_RefSiteKeywordsAreKeptOnTheHeader covers the header position of
// the $ref-sibling census (GitHub #283). A header carries its schema's
// declaration the way a model property does, so the keywords an alias over the
// $ref target cannot hold are kept on the header itself.
func TestHeaders_RefSiteKeywordsAreKeptOnTheHeader(t *testing.T) {
	t.Parallel()
	spec := "openapi: 3.1.0\ninfo: {title: T, version: \"1\"}\n" +
		"paths:\n  /x:\n    get:\n      operationId: g\n      responses:\n" +
		"        \"200\":\n          description: ok\n          headers:\n" +
		"            X-H: {schema: {$ref: '#/components/schemas/Base', enum: [a, b]}}\n" +
		"components:\n  schemas:\n    Base: {type: string}\n"
	_, svc, diags := lowerServiceSpec(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)

	h := headerAt(t, openapitest.FirstOp(t, svc))
	entry, ok := h.Unmodeled["openapi:enum"]
	require.True(t, ok, "an alias over the target has no member set, so the enum is kept")
	assert.Equal(t, ir.ReasonDegradedLowering, entry.Reason)
	assert.JSONEq(t, `["a","b"]`, string(entry.Value))
	openapitest.AssertInfoDiagAt(t, diags, "/paths/~1x/get/responses/200/headers/X-H/schema")
}
