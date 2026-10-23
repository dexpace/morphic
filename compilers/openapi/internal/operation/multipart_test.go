package operation_test

import (
	"encoding/json/jsontext"
	"strconv"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/compilers/openapi/internal/schema"
	"github.com/dexpace/morphic/ir"
)

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

// TestContent_MultipartRedeclaredPartFlagsComeFromTheSettledType holds a part
// the body's allOf redeclares to a $ref, whose target may be declared after the
// body, to the flag of the type the merge settled on, whichever component
// comes first. The operation walk reads the property's type, so it must read it
// after the redeclaration is judged and not at its first declaration.
func TestContent_MultipartRedeclaredPartFlagsComeFromTheSettledType(t *testing.T) {
	t.Parallel()
	const body = `    T:
      allOf:
        - {type: object, properties: {f: {}}}
        - {type: object, properties: {f: {$ref: "#/components/schemas/Q"}}}
`
	cases := []struct {
		name     string
		q        string
		filename bool
		multi    bool
	}{
		{"binary", `{type: string, format: binary}`, true, false},
		{"array", `{type: array, items: {type: string}}`, false, true},
	}
	for _, tc := range cases {
		for _, qFirst := range []bool{false, true} {
			t.Run(tc.name+map[bool]string{false: " declared after", true: " declared before"}[qFirst], func(t *testing.T) {
				t.Parallel()
				q := "    Q: " + tc.q + "\n"
				components := body + q
				if qFirst {
					components = q + body
				}
				doc, svc, diags := lowerServiceSpec(t, openapitest.PathsSpec(`  /u:
    post:
      requestBody:
        content:
          multipart/form-data:
            schema: {$ref: "#/components/schemas/T"}
      responses: {"204": {description: ok}}
components:
  schemas:
`+components))
				openapitest.RequireNoErrorDiags(t, diags)
				enc := multipartEncoding(t, openapitest.FirstOp(t, svc))
				model, ok := typeByName(doc, "T").(*ir.Model)
				require.True(t, ok, "T lowers to a model")
				require.Len(t, model.Properties, 1)
				prop := model.Properties[0]
				assert.Equal(t, ir.TypeID("t/openapi/components/schemas/Q"), prop.Type.Target,
					"the settled type of f is Q")
				pe, ok := enc[prop.ID]
				require.True(t, ok, "the part is entered; got %v", enc)
				assert.Equal(t, tc.filename, pe.Filename, "filename")
				assert.Equal(t, tc.multi, pe.Multi, "multi")
			})
		}
	}
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

// TestPartEncoding_FlagsComeFromTheMergedType holds a part's Multi and Filename
// to the type the allOf merge settled on, not to whichever branch declared the
// name first. The merge keeps the narrower declaration in either order, so a
// flag read from the first declaration differs between the two orders.
func TestPartEncoding_FlagsComeFromTheMergedType(t *testing.T) {
	t.Parallel()
	const binary = `{type: string, format: binary}`
	cases := []struct {
		name     string
		a, b     string
		filename bool
		multi    bool
	}{
		{"string and binary", `{type: string}`, binary, true, false},
		{"untyped and binary", `{description: the file}`, binary, true, false},
		{"untyped and a list of binary", `{description: the files}`,
			`{type: array, items: ` + binary + `}`, false, true},
		{"untyped and byte", `{description: the file}`, `{type: string, format: byte}`, true, false},
		{"untyped and a hoisted binary", `{description: the file}`,
			`{$ref: "#/components/schemas/Blob", description: the blob}`, true, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var got [2]ir.PartEncoding
			for i, branches := range [2][2]string{{tc.a, tc.b}, {tc.b, tc.a}} {
				got[i] = onlyPartEncoding(t, `{allOf: [`+
					`{type: object, properties: {file: `+branches[0]+`}}, `+
					`{type: object, properties: {file: `+branches[1]+`}}]}`)
			}
			assert.Empty(t, cmp.Diff(got[0], got[1]), "the two branch orders agree")
			assert.Equal(t, tc.filename, got[0].Filename, "filename")
			assert.Equal(t, tc.multi, got[0].Multi, "multi")
		})
	}
}

// TestPartEncoding_FlagsOfAnUndecidedMergedTypeComeFromEveryDeclaration holds a
// part whose merged type the type walk cannot answer for to the flags every
// declaration of its name gives, in either branch order, and a redeclared part
// that is not the first to its own entry.
func TestPartEncoding_FlagsOfAnUndecidedMergedTypeComeFromEveryDeclaration(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		a, b     string
		filename bool
		multi    bool
	}{
		{"untyped and a binary enum", `{description: d}`,
			`{type: string, format: binary, enum: [a, b]}`, true, false},
		{"untyped and a union with an array", `{description: d}`,
			`{type: [array, string], items: {type: string}}`, false, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var got [2]ir.PartEncoding
			for i, branches := range [2][2]string{{tc.a, tc.b}, {tc.b, tc.a}} {
				got[i] = onlyPartEncoding(t, `{allOf: [`+
					`{type: object, properties: {file: `+branches[0]+`}}, `+
					`{type: object, properties: {file: `+branches[1]+`}}]}`)
			}
			assert.Empty(t, cmp.Diff(got[0], got[1]), "the two branch orders agree")
			assert.Equal(t, tc.filename, got[0].Filename, "filename")
			assert.Equal(t, tc.multi, got[0].Multi, "multi")
		})
	}

	pe := onlyPartEncoding(t, `{allOf: [{type: object, properties: {name: {type: string}, file: {description: d}}}, `+
		`{type: object, properties: {file: {type: string, format: binary, enum: [a, b]}}}]}`)
	assert.True(t, pe.Filename, "the second part collects its own redeclaration")
}

// TestPartEncoding_ADecidedTypeNeverFallsBack holds a part whose merged type
// decides it is not bytes to no flag, though another declaration is binary.
func TestPartEncoding_ADecidedTypeNeverFallsBack(t *testing.T) {
	t.Parallel()
	spec := openapitest.PathsSpec(`  /upload:
    post:
      operationId: upload
      requestBody:
        content:
          multipart/form-data:
            schema: {allOf: [{type: object, properties: {file: {type: integer}}}, ` +
		`{type: object, properties: {file: {type: string, format: binary}}}]}
      responses: {"200": {description: ok}}
`)
	_, svc, diags := lowerServiceSpec(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)
	assert.Empty(t, openapitest.FirstOp(t, svc).Request.Contents[0].Encoding, "the kept integer decides: not a file")
}

// TestPartEncoding_FlagsOfAnUndecidedTypeComeFromTheSchema holds a part whose
// single declaration lowers to a kind the type walk cannot answer for to the
// flags its schema gives, and a tuple to the type's own answer.
func TestPartEncoding_FlagsOfAnUndecidedTypeComeFromTheSchema(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		part     string
		filename bool
		multi    bool
	}{
		{"union of binary", `{type: [string, integer], format: binary}`, true, false},
		{"union with an array", `{type: [array, string], items: {type: string}}`, false, true},
		{"tuple", `{type: array, prefixItems: [{type: string}]}`, false, true},
		{"enum of binary", `{type: string, format: binary, enum: [a, b]}`, true, false},
		{"literal of binary", `{type: string, format: binary, const: a}`, true, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			pe := onlyPartEncoding(t, `{type: object, properties: {file: `+tc.part+`}}`)
			assert.Equal(t, tc.filename, pe.Filename, "filename")
			assert.Equal(t, tc.multi, pe.Multi, "multi")
		})
	}
}

// TestPartEncoding_FlagsOfABodyWithNoModelPropertyComeFromTheSchema covers the
// schema reading, which stays for a body the IR holds no model for.
func TestPartEncoding_FlagsOfABodyWithNoModelPropertyComeFromTheSchema(t *testing.T) {
	t.Parallel()
	file := onlyPartEncoding(t, `{enum: [x], properties: {file: {type: string, format: binary}}}`)
	assert.True(t, file.Filename, "a binary part is a file")
	assert.False(t, file.Multi)

	files := onlyPartEncoding(t,
		`{enum: [x], properties: {file: {type: array, items: {type: string, format: binary}}}}`)
	assert.True(t, files.Multi, "an array part repeats")
	assert.False(t, files.Filename)
}

// TestPartEncodings_ReadsTheFlattenedComposition holds bodyParts to the
// composition the schema lowering flattens (docs/compilers/openapi.md#allof-classification),
// in both declaration orders of the parts and of the encoding map. A part
// nested n wrappers deep is keyed on the property the lowering models there, or
// a warning at the body schema names the bound that cut the walk short; one
// that is neither drops its encoding with no word.
func TestPartEncodings_ReadsTheFlattenedComposition(t *testing.T) {
	t.Parallel()
	const multipart, urlencoded = "multipart/form-data", "application/x-www-form-urlencoded"
	cases := []struct {
		name    string
		n       int
		residue bool
		shape   string
		media   string
		capped  bool
	}{
		{"15 wrappers", 15, false, shapeInline, multipart, false},
		{"16 wrappers", 16, false, shapeInline, multipart, false},
		{"17 wrappers", 17, false, shapeInline, multipart, false},
		{"20 wrappers", 20, false, shapeInline, multipart, false},
		{"20 wrappers, urlencoded", 20, false, shapeInline, urlencoded, false},
		{"past the flattening depth", schema.MaxAllOfFlattenDepth + 2, false, shapeInline, multipart, true},
		{"past the flattening depth, urlencoded", schema.MaxAllOfFlattenDepth + 2, false, shapeInline, urlencoded, true},
		{"past the flattening depth behind a $ref", schema.MaxAllOfFlattenDepth + 2, false, shapeViaRef, multipart, true},
		{"a part beside the cut", schema.MaxAllOfFlattenDepth + 2, false, shapeBesideNote, multipart, true},
		{"residue wrapper", 1, true, shapeInline, multipart, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			bodyPtr := ids.Ptr("paths", "/u", "post", "requestBody", "content", tc.media, "schema")
			forward := nestedPartSpec(tc.n, tc.residue, tc.shape, tc.media, []string{"file", "meta"}, []string{"file", "meta"})
			reversed := nestedPartSpec(tc.n, tc.residue, tc.shape, tc.media, []string{"meta", "file"}, []string{"meta", "file"})
			var got [2]map[ir.PropID]ir.PartEncoding
			for i, src := range []string{forward, reversed} {
				_, svc, diags := lowerServiceSpec(t, src)
				openapitest.RequireNoErrorDiags(t, diags)
				got[i] = openapitest.FirstOp(t, svc).Request.Contents[0].Encoding
				if tc.capped {
					msg := openapitest.DiagMessageAt(t, diags, diag.DegradedConstruct, ir.SeverityWarning, string(bodyPtr))
					assert.Equal(t, "form body composition is nested past the flattening depth of 32, "+
						"so a part reached only below it has no encoding entry", msg, "a walk cut short by the bound says so")
					if tc.shape == shapeBesideNote {
						note := ir.PropID("p/openapi" + bodyPtr + ids.Ptr("properties", "note"))
						assert.Contains(t, got[i], note, "the part the body declares is keyed beside the cut")
					}
					continue
				}
				prefix := string(bodyPtr) + strings.Repeat("/allOf/0", tc.n)
				for _, part := range []string{"file", "meta"} {
					id := ir.PropID("p/openapi" + jsontext.Pointer(prefix) + ids.Ptr("properties", part))
					assert.Contains(t, got[i], id, "the %s part is keyed on the property nested %d wrappers down", part, tc.n)
				}
			}
			assert.Empty(t, cmp.Diff(got[0], got[1]), "the declaration order of the parts changes nothing")
		})
	}
}

// TestPartEncodings_RefHopBoundIsReported chains a multipart body through more
// $ref targets than bodyParts follows. The part declared at the end of the chain
// has no entry, and a warning at the body schema says why.
func TestPartEncodings_RefHopBoundIsReported(t *testing.T) {
	t.Parallel()
	var comps strings.Builder
	const links = 20
	for i := range links {
		next := "{type: object, properties: {file: {type: string, format: binary}}}"
		if i < links-1 {
			next = "{allOf: [{$ref: '#/components/schemas/L" + strconv.Itoa(i+1) + "'}]}"
		}
		comps.WriteString("    L" + strconv.Itoa(i) + ": " + next + "\n")
	}
	spec := openapitest.PathsSpec(`  /u:
    post:
      operationId: up
      requestBody:
        content:
          multipart/form-data:
            schema: {$ref: '#/components/schemas/L0'}
            encoding:
              file: {contentType: image/png}
      responses: {"200": {description: ok}}
components:
  schemas:
` + comps.String())
	_, svc, diags := lowerServiceSpec(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)
	msg := openapitest.DiagMessageAt(t, diags, diag.DegradedConstruct, ir.SeverityWarning,
		"/paths/~1u/post/requestBody/content/multipart~1form-data/schema")
	assert.Equal(t, "form body composition follows $refs past 16 hops, "+
		"so a part reached only beyond them has no encoding entry", msg, "the bound is named at the body schema")
	assert.Empty(t, openapitest.FirstOp(t, svc).Request.Contents[0].Encoding, "the part beyond it has no entry")
}

// TestPartEncodings_ShorterPathReentersTarget spells a 16-link allOf chain the
// body reaches twice: from its head and from its last link. Whichever the body
// names first, the target at the end is two hops from the body by the short
// path, so both parts are keyed and no bound is reported.
func TestPartEncodings_ShorterPathReentersTarget(t *testing.T) {
	t.Parallel()
	cases := []struct{ name, allOf string }{
		{"L1, L16", "[{$ref: '#/components/schemas/L1'}, {$ref: '#/components/schemas/L16'}]"},
		{"L16, L1", "[{$ref: '#/components/schemas/L16'}, {$ref: '#/components/schemas/L1'}]"},
	}
	var comps strings.Builder
	for i := 1; i <= 16; i++ {
		comps.WriteString("    L" + strconv.Itoa(i) + ": {allOf: [{$ref: '#/components/schemas/L" + strconv.Itoa(i+1) + "'}]}\n")
	}
	comps.WriteString("    L17: {type: object, properties: {file: {type: string, format: binary}, meta: {type: string}}}\n")
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			spec := openapitest.PathsSpec(`  /u:
    post:
      operationId: up
      requestBody:
        content:
          multipart/form-data:
            schema: {allOf: ` + tc.allOf + `}
            encoding:
              file: {contentType: image/png}
              meta: {contentType: application/json}
      responses: {"200": {description: ok}}
components:
  schemas:
` + comps.String())
			_, svc, diags := lowerServiceSpec(t, spec)
			openapitest.RequireNoErrorDiags(t, diags)
			got := openapitest.FirstOp(t, svc).Request.Contents[0].Encoding
			for _, part := range []string{"file", "meta"} {
				id := ir.PropID("p/openapi/components/schemas/L17" + ids.Ptr("properties", part))
				assert.Contains(t, got, id, "the %s part is keyed at the target both paths reach", part)
			}
			assert.NotContains(t, diagMessages(diags), "follows $refs past",
				"a path within the bound reaches the target, so no bound is reported")
		})
	}
}

// TestPartEncodings_CappedEntryReachedByAnotherPath nests an entry under more
// inline allOf wrappers than the lowering flattens, and reaches what it names a
// second time by another path: an object, by a JSON Pointer $ref to it, and a
// $ref entry of the branch the cap stops at, by the same $ref written at the
// top, whose target the cap's record must follow as the walk does. Whichever
// the body names first, both parts are keyed, so no cap is reported.
func TestPartEncodings_CappedEntryReachedByAnotherPath(t *testing.T) {
	t.Parallel()
	const n = schema.MaxAllOfFlattenDepth + 2
	obj := "{type: object, properties: {file: {type: string, format: binary}, meta: {type: string}}}"
	deep := obj
	for range n {
		deep = "{allOf: [" + deep + "]}"
	}
	leaf := "Deep" + strings.Repeat("/allOf/0", n)
	direct := "{$ref: '#/components/schemas/" + leaf + "'}"
	viaDeep := "{$ref: '#/components/schemas/Deep'}"
	viaForm := "{$ref: '#/components/schemas/Form'}"
	formBelowCap := viaForm
	for range schema.MaxAllOfFlattenDepth + 1 {
		formBelowCap = "{allOf: [" + formBelowCap + "]}"
	}
	cases := []struct{ name, allOf, keyedAt string }{
		{"Deep first", "[" + viaDeep + ", " + direct + "]", leaf},
		{"pointer first", "[" + direct + ", " + viaDeep + "]", leaf},
		{"Form first", "[" + viaForm + ", " + formBelowCap + "]", "Form"},
		{"Form below the cap first", "[" + formBelowCap + ", " + viaForm + "]", "Form"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			spec := openapitest.PathsSpec(`  /u:
    post:
      operationId: up
      requestBody:
        content:
          multipart/form-data:
            schema: {allOf: ` + tc.allOf + `}
            encoding:
              file: {contentType: image/png}
              meta: {contentType: application/json}
      responses: {"200": {description: ok}}
components:
  schemas:
    Deep: ` + deep + `
    Form: ` + obj + `
`)
			_, svc, diags := lowerServiceSpec(t, spec)
			openapitest.RequireNoErrorDiags(t, diags)
			got := openapitest.FirstOp(t, svc).Request.Contents[0].Encoding
			for _, part := range []string{"file", "meta"} {
				id := ir.PropID("p/openapi/components/schemas/" + tc.keyedAt + string(ids.Ptr("properties", part)))
				assert.Contains(t, got, id, "the %s part is keyed at the object both paths reach", part)
			}
			assert.NotContains(t, diagMessages(diags), "form body composition is nested past",
				"a path enters what the capped branch's entry names, so no cap is reported")
		})
	}
}

// TestPartEncodings_CutPastReachedPart cuts the walk at a target that no path
// enters, while a shorter path reaches the object below it: a $ref chain past
// the hop bound and inline wrappers past the flattening depth. Whichever the
// body names first, both parts are keyed, and the bound's warning claims a
// loss only for a part reached through the cut alone.
func TestPartEncodings_CutPastReachedPart(t *testing.T) {
	t.Parallel()
	const partRefHops = 16
	obj := "{type: object, properties: {file: {type: string, format: binary}, meta: {type: string}}}"
	var chain strings.Builder
	for i := range partRefHops + 1 {
		chain.WriteString("    L" + strconv.Itoa(i) + ": {allOf: [{$ref: '#/components/schemas/L" + strconv.Itoa(i+1) + "'}]}\n")
	}
	chain.WriteString("    L" + strconv.Itoa(partRefHops+1) + ": " + obj + "\n")
	const n = schema.MaxAllOfFlattenDepth + 3
	deep := obj
	for range n {
		deep = "{allOf: [" + deep + "]}"
	}
	leaf := "Deep" + strings.Repeat("/allOf/0", n)
	cases := []struct{ name, first, second, comps, keyedAt, bound string }{
		{"hops, chain first", "L0", "L17", chain.String(), "L17", "past 16 hops, so a part reached only beyond them"},
		{"hops, target first", "L17", "L0", chain.String(), "L17", "past 16 hops, so a part reached only beyond them"},
		{"depth, wrappers first", "Deep", leaf, "    Deep: " + deep + "\n", leaf, "depth of 32, so a part reached only below it"},
		{"depth, pointer first", leaf, "Deep", "    Deep: " + deep + "\n", leaf, "depth of 32, so a part reached only below it"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			spec := openapitest.PathsSpec(`  /u:
    post:
      operationId: up
      requestBody:
        content:
          multipart/form-data:
            schema: {allOf: [{$ref: '#/components/schemas/` + tc.first + `'}, {$ref: '#/components/schemas/` + tc.second + `'}]}
            encoding:
              file: {contentType: image/png}
              meta: {contentType: application/json}
      responses: {"200": {description: ok}}
components:
  schemas:
` + tc.comps)
			_, svc, diags := lowerServiceSpec(t, spec)
			openapitest.RequireNoErrorDiags(t, diags)
			got := openapitest.FirstOp(t, svc).Request.Contents[0].Encoding
			for _, part := range []string{"file", "meta"} {
				id := ir.PropID("p/openapi/components/schemas/" + tc.keyedAt + string(ids.Ptr("properties", part)))
				assert.Contains(t, got, id, "the %s part is keyed at the object the shorter path reaches", part)
			}
			msg := openapitest.DiagMessageAt(t, diags, diag.DegradedConstruct, ir.SeverityWarning,
				"/paths/~1u/post/requestBody/content/multipart~1form-data/schema")
			assert.Contains(t, msg, tc.bound, "the warning at the body schema claims a loss only for a part the cut alone reaches")
		})
	}
}

// TestPartEncodings_CycleEntersEachTargetOnce spells an allOf cycle back to the
// body's own schema. Every part is declared once, so every part is keyed at the
// target's properties and no bound is reported: a cycle costs one visit.
func TestPartEncodings_CycleEntersEachTargetOnce(t *testing.T) {
	t.Parallel()
	cases := []struct{ name, allOf string }{
		{"direct", "[{$ref: '#/components/schemas/A'}]"},
		{"wrapper", "[{allOf: [{$ref: '#/components/schemas/A'}]}]"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			spec := openapitest.PathsSpec(`  /u:
    post:
      operationId: up
      requestBody:
        content:
          multipart/form-data:
            schema: {$ref: '#/components/schemas/A'}
            encoding:
              file: {contentType: image/png}
              meta: {contentType: application/json}
      responses: {"200": {description: ok}}
components:
  schemas:
    A:
      type: object
      properties: {file: {type: string, format: binary}, meta: {type: string}}
      allOf: ` + tc.allOf + "\n")
			_, svc, diags := lowerServiceSpec(t, spec)
			openapitest.RequireNoErrorDiags(t, diags)
			got := openapitest.FirstOp(t, svc).Request.Contents[0].Encoding
			for _, part := range []string{"file", "meta"} {
				id := ir.PropID("p/openapi/components/schemas/A" + ids.Ptr("properties", part))
				assert.Contains(t, got, id, "the %s part is keyed at the target's own property", part)
			}
			assert.NotContains(t, diagMessages(diags), "form body composition",
				"a cycle spends no bound, so no cut is reported anywhere")
		})
	}
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

// onlyPartEncoding lowers a multipart body with the given schema and returns
// the one part encoding it keeps. The part is named file.
func onlyPartEncoding(t *testing.T, mediaSchema string) ir.PartEncoding {
	t.Helper()
	spec := openapitest.PathsSpec(`  /upload:
    post:
      operationId: upload
      requestBody:
        content:
          multipart/form-data:
            schema: ` + mediaSchema + `
      responses: {"200": {description: ok}}
components:
  schemas:
    Blob: {type: string, format: binary}
`)
	_, svc, diags := lowerServiceSpec(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)
	enc := openapitest.FirstOp(t, svc).Request.Contents[0].Encoding
	require.Len(t, enc, 1, "one part carries a flag: %v", enc)
	for _, pe := range enc {
		return pe
	}
	return ir.PartEncoding{}
}

// nestedPartSpec spells a form body of media type media whose parts sit wrapped
// deep inside n inline allOf wrappers, each optionally carrying a description,
// with the encoding map and the two parts written in the order given.
func nestedPartSpec(n int, residue bool, shape, media string, parts, encoding []string) string {
	partLine := map[string]string{
		"file": "file: {type: string, format: binary}",
		"meta": "meta: {type: string}",
	}
	encLine := map[string]string{
		"file": "file: {contentType: image/png}",
		"meta": "meta: {contentType: application/json}",
	}
	props := make([]string, 0, len(parts))
	encs := make([]string, 0, len(encoding))
	for _, p := range parts {
		props = append(props, partLine[p])
	}
	for _, e := range encoding {
		encs = append(encs, encLine[e])
	}
	schemaSrc := "{type: object, properties: {" + strings.Join(props, ", ") + "}}"
	for range n {
		wrapper := "{allOf: [" + schemaSrc + "]}"
		if residue {
			wrapper = "{description: grouped, allOf: [" + schemaSrc + "]}"
		}
		schemaSrc = wrapper
	}
	var components string
	switch shape {
	case shapeViaRef:
		components = "components:\n  schemas:\n    Deep: " + schemaSrc + "\n"
		schemaSrc = "{allOf: [{$ref: '#/components/schemas/Deep'}]}"
	case shapeBesideNote:
		schemaSrc = "{type: object, properties: {note: {type: string}}, allOf: [" + schemaSrc + "]}"
		encs = append(encs, "note: {contentType: text/plain}")
	}
	return openapitest.PathsSpec(`  /u:
    post:
      operationId: up
      requestBody:
        content:
          ` + media + `:
            schema: ` + schemaSrc + `
            encoding: {` + strings.Join(encs, ", ") + `}
      responses: {"200": {description: ok}}
` + components)
}

// How nestedPartSpec places the wrappers: inline at the body, behind an allOf
// $ref branch, or beside a part the body declares itself.
const (
	shapeInline     = ""
	shapeViaRef     = "ref"
	shapeBesideNote = "note"
)

// diagMessages joins the messages of diags.
func diagMessages(diags []ir.Diagnostic) string {
	out := make([]string, 0, len(diags))
	for _, d := range diags {
		out = append(out, d.Message)
	}
	return strings.Join(out, "\n")
}
