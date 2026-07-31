package openapi

import (
	"strings"
	"testing"

	oas3 "github.com/speakeasy-api/openapi/jsonschema/oas3"
	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/speakeasy-api/openapi/sequencedmap"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/ir"
)

func TestContent_AllMediaTypesKeptInOrder(t *testing.T) {
	t.Parallel()
	spec := pathsSpec(`  /docs:
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
	requireNoErrorDiags(t, diags)
	op := firstOp(t, svc)
	require.NotNil(t, op.Request)
	require.Len(t, op.Request.Contents, 2, "no primary-content selection in the IR")
	assert.Equal(t, "application/json", op.Request.Contents[0].MediaType)
	assert.Equal(t, "application/xml", op.Request.Contents[1].MediaType)
	assert.Equal(t, []string{"application/json", "application/xml"},
		op.Bindings.HTTP[0].RequestContentTypes)
}

func TestContent_MultipartPartEncoding(t *testing.T) {
	t.Parallel()
	spec := pathsSpec(`  /upload:
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
	requireNoErrorDiags(t, diags)
	content := firstOp(t, svc).Request.Contents[0]
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
	spec := pathsSpec(`  /raw:
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
	requireNoErrorDiags(t, diags)
	op := firstOp(t, svc)
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
	spec := pathsSpec(`  /raw:
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
	requireNoErrorDiags(t, diags)
	content := firstOp(t, svc).Request.Contents[0]
	require.NotNil(t, content.File, "binary body behind a $ref lowers to a FileInfo")
	assert.False(t, content.File.IsText)
	assert.Equal(t, ir.TypeID("t/prim/bytes"), content.Type.Target)
}

func TestContent_MultipartRefBodyKeepsEncoding(t *testing.T) {
	t.Parallel()
	// A multipart body referenced via $ref must keep its per-part encoding, keyed
	// by the RESOLVED model's property IDs (under the ref target's pointer).
	spec := pathsSpec(`  /upload:
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
	requireNoErrorDiags(t, diags)
	content := firstOp(t, svc).Request.Contents[0]
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
	return pathsSpec(`  /upload:
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
			requireNoErrorDiags(t, diags)
			content := firstOp(t, svc).Request.Contents[0]
			form, isModel := typeByName(doc, "Form").(*ir.Model)
			require.True(t, isModel, "the body model is the component at the end of the chain")

			declared := indexBy(form.Properties, func(p ir.Property) ir.PropID { return p.ID })
			require.NotEmpty(t, content.Encoding, "an aliased multipart body still carries per-part encoding")
			for key := range content.Encoding {
				assert.Contains(t, declared, key,
					"every encoding key addresses a property the aliased model declares")
			}

			byWire := propsByWire(form.Properties)
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
			spec := pathsSpec(`  /upload:
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
			requireNoErrorDiags(t, diags)
			content := firstOp(t, svc).Request.Contents[0]
			require.Contains(t, content.Encoding, tc.want,
				"the key falls back to the schema's own position; got %v", content.Encoding)
		})
	}
}

// TestBodyModelPointer_NoModelBehindBody covers the walk's dead ends on a
// hand-built registry: an ID nothing declares, an opaque scalar, a kind that is
// no model, and an alias chain that closes on itself. The last is what the bound
// is for — a $ref cycle is refused at load, so no document can spell one, and a
// walk that relied on that would loop forever the day one arrived by another
// route.
func TestBodyModelPointer_NoModelBehindBody(t *testing.T) {
	t.Parallel()
	l := newRawLowerer(&soa.OpenAPI{})
	l.types.Register("t/opaque", &ir.Scalar{TypeCommon: ir.TypeCommon{ID: "t/opaque"}})
	l.types.Register("t/cycle/a", &ir.Scalar{
		TypeCommon: ir.TypeCommon{ID: "t/cycle/a"}, Base: &ir.TypeRef{Target: "t/cycle/b"},
	})
	l.types.Register("t/cycle/b", &ir.Scalar{
		TypeCommon: ir.TypeCommon{ID: "t/cycle/b"}, Base: &ir.TypeRef{Target: "t/cycle/a"},
	})
	prim := l.types.PrimID(ir.PrimString)

	cases := []struct {
		name string
		body ir.TypeID
	}{
		{"an ID no registry entry declares", "t/absent"},
		{"an opaque scalar standing for nothing", "t/opaque"},
		{"a kind that declares no properties", prim},
		{"an alias chain that closes on itself", "t/cycle/a"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			pointer, ok := bodyModelPointer(l.types, tc.body)
			assert.False(t, ok, "no model stands behind this body")
			assert.Empty(t, pointer, "and no pointer is invented for one")
		})
	}
}

// TestBodySchemaPointer_LocalRefFragment pins the fallback's own ref hop, which
// the production path reaches only for a body standing for no model.
func TestBodySchemaPointer_LocalRefFragment(t *testing.T) {
	t.Parallel()
	l := newRawLowerer(&soa.OpenAPI{})
	js := oas3.NewJSONSchemaFromReference("#/components/schemas/Form")
	assert.Equal(t, "/components/schemas/Form", bodySchemaPointer(l.ctx, js, "/local"))
}

// TestBodySchemaPointer_ForeignDocumentRefStaysLocal pins the document half of a
// $ref deciding the pointer. Cutting the fragment off blindly turned another
// document's property into an identity in this one, silently naming whichever
// local schema shared the path.
func TestBodySchemaPointer_ForeignDocumentRefStaysLocal(t *testing.T) {
	t.Parallel()
	l := newRawLowerer(&soa.OpenAPI{})
	l.ctx.Source = ir.SourceInfo{Path: "spec.yaml"}
	js := oas3.NewJSONSchemaFromReference("./ext-form.yaml#/components/schemas/Form")
	assert.Equal(t, "/local", bodySchemaPointer(l.ctx, js, "/local"),
		"a fragment from another document must not become a pointer into this one")
}

// TestBodySchemaPointer_SelfNamedRefFollowsFragment pins the other half: a ref
// spelling this document's own filename is internal, so its fragment is a
// pointer here after all.
func TestBodySchemaPointer_SelfNamedRefFollowsFragment(t *testing.T) {
	t.Parallel()
	l := newRawLowerer(&soa.OpenAPI{})
	l.ctx.Source = ir.SourceInfo{Path: "spec.yaml"}
	js := oas3.NewJSONSchemaFromReference("spec.yaml#/components/schemas/Form")
	assert.Equal(t, "/components/schemas/Form", bodySchemaPointer(l.ctx, js, "/local"))
}

func TestContent_NonRequiredRequestBody(t *testing.T) {
	t.Parallel()
	spec := pathsSpec(`  /maybe:
    post:
      operationId: maybe
      requestBody:
        content:
          application/json: {schema: {type: object, properties: {n: {type: string}}}}
      responses: {"200": {description: ok}}
`)
	_, svc, diags := lowerServiceSpec(t, spec)
	requireNoErrorDiags(t, diags)
	op := firstOp(t, svc)
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
	spec := pathsSpec(`  /bulk:
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
	requireNoErrorDiags(t, diags)
	content := firstOp(t, svc).Request.Contents[0]
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

	assert.True(t, hasDiag(diags, diag.DegradedConstruct))
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
	stream := findOp(t, doc, "stream")
	resp := stream.Responses[0]
	require.NotNil(t, resp.Payload)
	c := resp.Payload.Contents[0]
	require.NotNil(t, c.Item, "itemSchema becomes the element type")
	require.NotNil(t, c.ItemEncoding, "itemEncoding lowered structurally")
	assert.Equal(t, []string{"application/json"}, c.ItemEncoding.ContentTypes)
	assert.True(t, c.ItemEncoding.Multi, "itemEncoding governs a repeated tail")

	// Empty request-body content yields no Request payload.
	empty := findOp(t, doc, "emptyBody")
	assert.Nil(t, empty.Request)
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
		op := findOp(t, doc, name)
		require.NotNil(t, op.Request, "%s has a request", name)
		for _, c := range op.Request.Contents {
			assert.Empty(t, c.Encoding, "%s multipart yields no per-part encoding", name)
		}
	}
}

func TestContent_ExampleWithoutValueSkipped(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, multipartVariantsSpec)
	op := findOp(t, doc, "exGet")
	c := op.Responses[0].Payload.Contents[0]
	assert.Empty(t, c.Examples, "an example carrying no example is skipped")

	// Skipped, but not in silence: the entry declares neither a value nor the
	// externalValue that would have given it a home.
	d, ok := firstDegradedWarning(diags)
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
	spec := pathsSpec(`  /items:
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
	op := firstOp(t, svc)
	require.Len(t, op.Responses, 1)
	c := op.Responses[0].Payload.Contents[0]
	assert.Empty(t, c.Examples, "both unconvertible examples are skipped, not appended")

	require.Equal(t, 2, countDiagsAt(diags, diag.DegradedConstruct, ir.SeverityWarning))
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
	op := firstOp(t, svc)
	c := op.Responses[0].Payload.Contents[0]
	require.Len(t, c.Examples, 1, "the convertible $ref'd example still lowers")
	require.NotNil(t, c.Examples[0].Value)
	assert.Equal(t, ir.Value{Kind: ir.ValueString, Str: "fine"}, *c.Examples[0].Value)

	require.Equal(t, 1, countDiagsAt(diags, diag.DegradedConstruct, ir.SeverityWarning))
	d, ok := firstDegradedWarning(diags)
	require.True(t, ok)
	assert.Equal(t, "/paths/~1items/get/responses/200/content/application~1json/examples/bad",
		d.Provenance.Pointer, "the reference site, not a /value the source never had")
	assert.Contains(t, d.Message, "example:")
}

func TestContentTypeKeys_Nil(t *testing.T) {
	t.Parallel()
	assert.Nil(t, contentTypeKeys(nil))
}

func TestFillSequential_EmptyItemEncoding(t *testing.T) {
	t.Parallel()
	l := newRawLowerer(&soa.OpenAPI{})
	c := &ir.Content{}
	media := &soa.MediaType{ItemEncoding: &soa.Encoding{}}
	diags := fillSequential(l.ctx, l.types, &l.anchors, c, media, "/mp", "h")
	assert.Equal(t, &ir.PartEncoding{Multi: true}, c.ItemEncoding,
		"a config-free itemEncoding still records that the tail repeats")
	assert.Nil(t, c.Unmodeled, "nothing is preserved raw")
	assert.Empty(t, diags)
}

func TestEncodingConfig_NilEncoding(t *testing.T) {
	t.Parallel()
	l := newRawLowerer(&soa.OpenAPI{})
	pe, diags := encodingConfig(l.ctx, l.types, &l.anchors, nil, "/mp")
	assert.Equal(t, ir.PartEncoding{}, pe)
	assert.Empty(t, diags)
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
	c := findOp(t, doc, "mixed").Responses[0].Payload.Contents[0]

	assert.Nil(t, c.ItemEncoding, "positional prefixes rule out an every-item encoding")
	prefix, ok := c.Unmodeled["openapi:prefixEncoding"]
	require.True(t, ok, "prefixEncoding kept verbatim; got %v", c.Unmodeled)
	assert.Equal(t, ir.ReasonNoIRHome, prefix.Reason)
	assert.JSONEq(t, `[{"contentType": "application/json"}, {"contentType": "application/xml"}]`,
		string(prefix.Value))
	item, ok := c.Unmodeled["openapi:itemEncoding"]
	require.True(t, ok, "the tail encoding is kept beside the prefixes it follows")
	assert.JSONEq(t, `{"contentType": "text/plain"}`, string(item.Value))
	assertHasCode(t, diags, diag.DegradedConstruct, ir.SeverityInfo)
}

func TestFillSequential_PrefixEncodingWithoutItemEncoding(t *testing.T) {
	t.Parallel()
	spec := strings.ReplaceAll(positionalEncodingSpec, "              itemEncoding: {contentType: text/plain}\n", "")
	doc, diags := parseFull(t, spec)
	c := findOp(t, doc, "mixed").Responses[0].Payload.Contents[0]

	assert.Nil(t, c.ItemEncoding)
	_, ok := c.Unmodeled["openapi:prefixEncoding"]
	assert.True(t, ok, "prefixEncoding alone is still reported rather than dropped")
	_, ok = c.Unmodeled["openapi:itemEncoding"]
	assert.False(t, ok, "no itemEncoding was declared, so none is recorded")
	assertHasCode(t, diags, diag.DegradedConstruct, ir.SeverityInfo)
}

// TestPositionalEncoding_WithoutRootNode covers the media type whose source node
// cannot be read. prefixEncoding is declared — nothing else routes a content
// entry here — so nothing being written is a construct lost rather than one that
// was never there, and the position reports that instead of announcing a
// preservation it did not make (GitHub #144).
func TestPositionalEncoding_WithoutRootNode(t *testing.T) {
	t.Parallel()
	l := newRawLowerer(&soa.OpenAPI{})
	c := &ir.Content{}
	media := &soa.MediaType{PrefixEncoding: []*soa.Encoding{{}}, ItemEncoding: &soa.Encoding{}}
	diags := fillSequential(l.ctx, l.types, &l.anchors, c, media, "/mp", "h")
	assert.Nil(t, c.ItemEncoding, "prefixes still block the every-item lowering")
	assert.Nil(t, c.Unmodeled, "a media type with no source node has nothing verbatim to keep")
	assertHasCode(t, diags, diag.UnpreservableConstruct, ir.SeverityError)
	assert.False(t, hasDiagAt(diags, diag.DegradedConstruct, ir.SeverityInfo),
		"nothing was kept, so nothing announces that it was")
}

func TestBodySchemaPointer_ExternalRefNoFragment(t *testing.T) {
	t.Parallel()
	l := newRawLowerer(&soa.OpenAPI{})
	js := oas3.NewJSONSchemaFromReference("external.yaml")
	assert.Equal(t, "/local", bodySchemaPointer(l.ctx, js, "/local"), "a fragmentless ref falls back to the local pointer")
}

func TestBodySchemaPointer_NilSchema(t *testing.T) {
	t.Parallel()
	l := newRawLowerer(&soa.OpenAPI{})
	assert.Equal(t, "/local", bodySchemaPointer(l.ctx, nil, "/local"))
}

func TestLowerPayload_NilMediaEntriesYieldNil(t *testing.T) {
	t.Parallel()
	l := newRawLowerer(&soa.OpenAPI{})
	content := sequencedmap.New(
		sequencedmap.NewElem("application/json", (*soa.MediaType)(nil)),
	)
	payload, diags := lowerPayload(l.ctx, l.types, &l.anchors, content, "/p", "hint")
	assert.Nil(t, payload, "all-nil media map yields no payload")
	assert.Empty(t, diags)
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
	requireNoErrorDiags(t, diags)
	postA := findOp(t, doc, "postA")
	postB := findOp(t, doc, "postB")
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
	requireNoErrorDiags(t, diags)
	op := findOp(t, doc, "getA")
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
	requireNoErrorDiags(t, diags)
	op := findOp(t, doc, "getA")
	require.Len(t, op.Responses[0].Headers, 2)
	byWire := indexBy(op.Responses[0].Headers, func(p ir.Property) string { return p.WireName })

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
// name, not a cosmetic one.
func TestContent_SharedComponentSchemaTakesItsDeclarationHint(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, componentBodyRefSpec)
	requireNoErrorDiags(t, diags)
	bodyID := ir.TypeID("t/anon/components/requestBodies/Body/content/application~1json/schema")
	body, ok := doc.Types[bodyID]
	require.True(t, ok)
	assert.True(t, body.Common().Anonymous, "a requestBody component is not a named type")
	assert.Equal(t, "Body", body.Common().Name.Hint,
		"the shared body schema is hinted from its component, not from postA or postB")

	hdrDoc, hdrDiags := parseFull(t, headerIdentitySpec)
	requireNoErrorDiags(t, hdrDiags)
	hdr, ok := hdrDoc.Types[ir.TypeID("t/anon/components/headers/Rate/schema")]
	require.True(t, ok)
	assert.Equal(t, "Rate", hdr.Common().Name.Hint,
		"the shared header schema is hinted from its component, not from X-Rate or X-Limit")
}

// TestContent_InlineSchemaKeepsItsUseSiteHint is the other half of the rule
// above: only a component declaration renames the hoisted node. An inline body
// has exactly one use site, so its operationId-derived hint is the best name
// available and must survive.
func TestContent_InlineSchemaKeepsItsUseSiteHint(t *testing.T) {
	t.Parallel()
	spec := pathsSpec(`  /a:
    post:
      operationId: postA
      requestBody:
        content:
          application/json:
            schema: {type: object, properties: {n: {type: string}}}
      responses: {"200": {description: ok}}
`)
	doc, diags := parseFull(t, spec)
	requireNoErrorDiags(t, diags)
	td, ok := doc.Types[ir.TypeID("t/anon/paths/~1a/post/requestBody/content/application~1json/schema")]
	require.True(t, ok)
	assert.Equal(t, "postA_request", td.Common().Name.Hint)
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
	requireNoErrorDiags(t, diags)
	op := findOp(t, doc, "upload")
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
	_, svc, diags := lowerServiceSpec(t, pathsSpec(
		"  /x:\n    get:\n      operationId: g\n      responses:\n"+
			"        \"200\":\n          description: ok\n          headers:\n"+
			"            X-H: {schema: "+inlineProbeBody+"}\n"))
	requireNoErrorDiags(t, diags)

	h := firstOp(t, svc).Responses[0].Headers[0]
	assertProbeDocsKept(t, h.Docs)
	assert.NotNil(t, h.Deprecation)
	assertProbeExample(t, h.Examples)
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
	_, svc, diags := lowerServiceSpec(t, pathsSpec(
		"  /x:\n    get:\n      operationId: g\n      responses:\n"+
			"        \"200\":\n          description: ok\n          headers:\n"+
			"            X-H:\n              description: HEADER\n              example: HEADER\n"+
			"              x-scope: header\n              schema:\n"+
			"                {type: string, description: SCHEMA, example: SCHEMA, x-scope: schema}\n"))
	requireNoErrorDiags(t, diags)

	h := firstOp(t, svc).Responses[0].Headers[0]
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
			_, svc, diags := lowerServiceSpec(t, pathsSpec(
				"  /x:\n    get:\n      operationId: g\n      responses:\n"+
					"        \"200\":\n          description: ok\n          headers:\n"+
					"            X-H:\n              deprecated: "+tc.header+"\n"+
					"              schema: {type: string, deprecated: "+tc.schema+"}\n"))
			requireNoErrorDiags(t, diags)

			h := firstOp(t, svc).Responses[0].Headers[0]
			assert.NotNil(t, h.Deprecation, "either side alone deprecates the header")
		})
	}
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
			spec: pathsSpec("  /x:\n    get:\n      responses:\n" +
				"        \"200\":\n          description: ok\n          headers:\n" +
				"            H:\n              content:\n" +
				"                application/xml: {schema: {type: string}}\n" +
				"                application/json: {schema: {type: integer}}\n"),
			at: "/paths/~1x/get/responses/200/headers/H/content",
		},
		{
			name: "operation parameter",
			spec: pathsSpec("  /x:\n    get:\n      parameters:\n" +
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
			assert.True(t, hasDiagCodeAt(diags, diag.DegradedConstruct, tc.at),
				"the ignored media types are named at the content map: %+v", diags)
			assert.Contains(t, diagMessageAt(t, diags, diag.DegradedConstruct, ir.SeverityWarning, tc.at),
				"application/json", "the message names what was ignored, not only that something was")
		})
	}
}

// TestSingleContentEntry_OneEntryIsSilent is the control: the legal one-entry
// spelling must not warn, or every content-style header and parameter would.
func TestSingleContentEntry_OneEntryIsSilent(t *testing.T) {
	t.Parallel()
	_, diags := parseFull(t, pathsSpec("  /x:\n    get:\n      responses:\n"+
		"        \"200\":\n          description: ok\n          headers:\n"+
		"            H:\n              content:\n"+
		"                application/xml: {schema: {type: string}}\n"))
	assert.Equal(t, 0, countDiagsAt(diags, diag.DegradedConstruct, ir.SeverityWarning),
		"one media type is the legal spelling: %+v", diags)
}

// TestHeaderSchema_NeitherSpelling covers a header that declares no type at all.
// It is legal — a header may carry only a description — and must lower to the top
// type without reporting a loss, since nothing was written to lose.
func TestHeaderSchema_NeitherSpelling(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, pathsSpec("  /x:\n    get:\n      operationId: untypedHeader\n      responses:\n"+
		"        \"200\":\n          description: ok\n          headers:\n"+
		"            H: {description: untyped}\n"))
	requireNoErrorDiags(t, diags)

	headers := findOp(t, doc, "untypedHeader").Responses[0].Headers
	require.Len(t, headers, 1)
	assert.Equal(t, ir.TypeID("t/prim/any"), headers[0].Type.Target,
		"a header declaring no type lowers to the top type")
	assert.Nil(t, headers[0].Encoding, "no content map means no media type")
	assert.Equal(t, "untyped", headers[0].Docs.Description)
}

// TestPropIDByWire_DeadEnds mirrors TestBodyModelPointer_NoModelBehindBody for
// the property walk: the same dead ends, asked for a part name rather than a
// pointer. Neither may invent an ID — a part keyed by a PropID nothing declares
// is a dangling reference the encoding map would carry silently.
func TestPropIDByWire_DeadEnds(t *testing.T) {
	t.Parallel()
	l := newRawLowerer(&soa.OpenAPI{})
	l.types.Register("t/opaque", &ir.Scalar{TypeCommon: ir.TypeCommon{ID: "t/opaque"}})
	l.types.Register("t/cycle/a", &ir.Scalar{
		TypeCommon: ir.TypeCommon{ID: "t/cycle/a"}, Base: &ir.TypeRef{Target: "t/cycle/b"},
	})
	l.types.Register("t/cycle/b", &ir.Scalar{
		TypeCommon: ir.TypeCommon{ID: "t/cycle/b"}, Base: &ir.TypeRef{Target: "t/cycle/a"},
	})
	l.types.Register("t/model/cycle", &ir.Model{
		TypeCommon: ir.TypeCommon{ID: "t/model/cycle"}, Base: &ir.TypeRef{Target: "t/model/cycle"},
	})
	prim := l.types.PrimID(ir.PrimString)

	cases := []struct {
		name string
		body ir.TypeID
	}{
		{"an ID no registry entry declares", "t/absent"},
		{"an opaque scalar standing for nothing", "t/opaque"},
		{"a kind that declares no properties", prim},
		{"an alias chain that closes on itself", "t/cycle/a"},
		{"a model whose base closes on itself", "t/model/cycle"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			id, ok := propIDByWire(l.types, tc.body, "file", 0)
			assert.False(t, ok, "no property with that wire name stands behind this body")
			assert.Empty(t, id, "and no ID is invented for one")
		})
	}
}

// TestPartEncodings_MixinPartIsKeyedOnTheMixin covers the second composition
// channel. An allOf of two $refs makes the first the Base and the rest Mixins
// (§4.3), so a part contributed by the second is reachable only by searching
// them — and its key must be the ID that mixin declares.
func TestPartEncodings_MixinPartIsKeyedOnTheMixin(t *testing.T) {
	t.Parallel()
	spec := pathsSpec(`  /upload:
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
	requireNoErrorDiags(t, diags)
	enc := firstOp(t, svc).Request.Contents[0].Encoding

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
	spec := pathsSpec(`  /upload:
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
	requireNoErrorDiags(t, diags)
	enc := firstOp(t, svc).Request.Contents[0].Encoding
	assert.Len(t, enc, 1, "one wire part is one encoding entry; got %v", enc)
}

// TestBodyParts_DepthBound covers the composition walk's bound. A $ref cycle is
// refused at load, so no document can spell a composition this deep; the bound is
// what keeps the walk terminating without relying on that.
func TestBodyParts_DepthBound(t *testing.T) {
	t.Parallel()
	js := oas3.NewJSONSchemaFromSchema[oas3.Referenceable](&oas3.Schema{})
	assert.Nil(t, bodyParts(js, maxPartCompositionDepth+1),
		"past the bound the walk stops rather than descending further")
}
