// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package operation_test

import (
	"encoding/json/jsontext"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/compilers/openapi/internal/operation"
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
	_, svc, diags := compileService(t, spec)
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
	_, svc, diags := compileService(t, spec)
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
				doc, svc, diags := compileService(t, openapitest.PathsSpec(`  /u:
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
			doc, svc, diags := compileService(t, aliasedMultipartSpec(tc.mediaSchema))
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

// TestPartEncodings_APartTheBodysTypeHoldsNoPropertyForKeysNothing holds a
// part the walk finds that no property of the body's lowered type holds, as
// one declared beside a keyword lowering the body to no model, or on a union
// branch lowering to none, to no Content.Encoding key, never a PropID derived
// from its pointer that no node holds: its flags key nothing, and an entry
// written for it is kept whole at openapi:encoding/<key>, with one info at the
// entry, while a part a model holds stays keyed.
func TestPartEncodings_APartTheBodysTypeHoldsNoPropertyForKeysNothing(t *testing.T) {
	t.Parallel()
	const component = "components:\n  schemas:\n    NotAModel: {enum: [x], properties: {file: {type: string, format: binary}}}\n"
	mfd := requestMedia(multipartForm)
	cases := []struct {
		name, schema, encoding string
		keyed                  map[ir.PropID][]string
	}{
		{"beside an enum", "{enum: [x], properties: {file: {type: string, format: binary}}}", "{}", nil},
		{"beside a const", "{const: x, properties: {file: {type: string, format: binary}}}", "{}", nil},
		{"beside a scalar type", "{type: string, properties: {file: {type: array, items: {type: string}}}}", "{}", nil},
		{"in a $ref'd component", "{$ref: '#/components/schemas/NotAModel'}", "{}", nil},
		{"beside an enum, with an entry", "{enum: [x], properties: {file: {type: string, format: binary}}}", "{file: " + pngSource + "}", nil},
		{"in a $ref'd component, with an entry", "{$ref: '#/components/schemas/NotAModel'}", "{file: " + pngSource + "}", nil},
		{
			"on a union branch lowering to no model, with an entry",
			"{oneOf: [{type: string, properties: {file: {type: string, format: binary}}}, {type: object, properties: {url: {type: string}}}]}",
			"{file: " + pngSource + "}",
			map[ir.PropID][]string{ir.PropID("p/openapi" + mfd + "/schema/oneOf/1/properties/url"): {"text/plain"}},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, svc, diags := compileService(t, formDoc("", multipartForm, tc.schema, tc.encoding, component))
			openapitest.RequireNoErrorDiags(t, diags)
			content := formContent(t, svc, false)
			want := tc.keyed
			if want == nil {
				want = map[ir.PropID][]string{}
			}
			assert.Equal(t, want, encodingTypes(content.Encoding), "a part no property holds keys nothing")
			if tc.encoding == "{}" {
				assert.Empty(t, keptEncodingNames(content.Unmodeled), "a part with no entry keeps nothing")
				assert.Empty(t, diagPointersUnder(diags, mfd+"/encoding/"))
				return
			}
			at := mfd + "/encoding/file"
			assert.Equal(t, []string{"file"}, keptEncodingNames(content.Unmodeled), "the entry is kept whole, and nothing else")
			rec := content.Unmodeled["openapi:encoding/file"]
			assert.Equal(t, ir.ReasonNoIRHome, rec.Reason)
			assert.JSONEq(t, pngEntry, string(rec.Value))
			assert.Equal(t, jsontext.Pointer(at), rec.Provenance.Pointer)
			assert.Equal(t, unkeyedPartMessage("file"), openapitest.DiagMessageAt(t, diags, diag.DegradedConstruct, ir.SeverityInfo, at))
			assert.Equal(t, []jsontext.Pointer{jsontext.Pointer(at)}, diagPointersUnder(diags, mfd+"/encoding/"),
				"one finding, and no warning that the key names nothing: the schema declares it")
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
	_, svc, diags := compileService(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)
	content := openapitest.FirstOp(t, svc).Request.Contents[0]
	tagsProp := ir.PropID("p/openapi" + ids.Ptr("paths", "/bulk", "post", "requestBody", "content", "multipart/form-data", "schema", "properties", "tags"))
	enc, ok := content.Encoding[tagsProp]
	require.True(t, ok, "array part gets a synthesized PartEncoding; got keys %v", content.Encoding)
	assert.True(t, enc.Multi, "array-typed part repeats per item")
}

// TestContent_MultipartEncodingVariants holds a body declaring no part to no
// part encoding, and one declaring plain parts and no encoding to each part's
// default content type alone.
func TestContent_MultipartEncodingVariants(t *testing.T) {
	t.Parallel()
	doc, _ := parseFull(t, multipartVariantsSpec)
	for _, name := range []string{"noSchema", "noProps"} {
		op := openapitest.FindOp(t, doc, name)
		require.NotNil(t, op.Request, "%s has a request", name)
		for _, c := range op.Request.Contents {
			assert.Empty(t, c.Encoding, "%s multipart yields no per-part encoding", name)
		}
	}
	op := openapitest.FindOp(t, doc, "plainProps")
	require.NotNil(t, op.Request)
	media := string(ids.Ptr("paths", "/plainprops", "post", "requestBody", "content", "multipart/form-data"))
	assert.Equal(t, map[ir.PropID]ir.PartEncoding{
		partProp(media, "a"): {ContentTypes: []string{"text/plain"}},
		partProp(media, "b"): {ContentTypes: []string{"text/plain"}},
	}, op.Request.Contents[0].Encoding, "plain parts carry their default content type and nothing else")
}

// TestPartEncodings_MixinPartIsKeyedOnTheMixin covers the Mixins channel,
// beside TestPartEncoding_NamesAPropertyInheritedFromTheBase's Base. Two $ref
// branches that join no discriminator hierarchy both become Mixins
// (docs/compilers/openapi.md#allof-classification), so a part either
// contributes is reachable only by searching them — and its key must be the ID
// that mixin declares.
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
	_, svc, diags := compileService(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)
	enc := openapitest.FirstOp(t, svc).Request.Contents[0].Encoding

	want := ir.PropID("p/openapi" + ids.Ptr("components", "schemas", "Extra", "properties", "note"))
	pe, ok := enc[want]
	require.True(t, ok, "a mixin's part is keyed on the mixin that declares it; got %v", enc)
	assert.Equal(t, []string{"text/plain"}, pe.ContentTypes)
}

// TestPartEncodings_KeyedOnTheDeclarationTheFlatteningTakes holds a part two
// composed models declare to the declaration the body's flattening takes
// (docs/ir/models.md#the-flattening): the later conjunct's, whichever the allOf
// lists last, with the flags read off that declaration's type. The declarations
// differ, a file and a list of files, so a key or a flag read off the other one
// shows. Both are files, so Filename shows nothing either way.
func TestPartEncodings_KeyedOnTheDeclarationTheFlatteningTakes(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name          string
		first, second string
		multi         bool
	}{
		{"the list last", "One", "Many", true},
		{"the list first", "Many", "One", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			spec := openapitest.PathsSpec(fmt.Sprintf(`  /upload:
    post:
      operationId: upload
      requestBody:
        content:
          multipart/form-data:
            schema:
              allOf:
                - {$ref: "#/components/schemas/%s"}
                - {$ref: "#/components/schemas/%s"}
            encoding:
              file: {contentType: image/png}
      responses: {"200": {description: ok}}
components:
  schemas:
    One:
      type: object
      properties: {file: {type: string, format: binary}}
    Many:
      type: object
      properties: {file: {type: array, items: {type: string, format: binary}}}
`, tc.first, tc.second))
			_, svc, diags := compileService(t, spec)
			openapitest.RequireNoErrorDiags(t, diags)
			enc := openapitest.FirstOp(t, svc).Request.Contents[0].Encoding

			want := ir.PropID("p/openapi" + ids.Ptr("components", "schemas", tc.second, "properties", "file"))
			require.Len(t, enc, 1, "one part is one entry: %v", enc)
			pe, ok := enc[want]
			require.True(t, ok, "the part is keyed on the later conjunct's declaration; got %v", enc)
			assert.Equal(t, []string{"image/png"}, pe.ContentTypes)
			assert.Equal(t, tc.multi, pe.Multi, "the flags are read off the declaration the key names")
			assert.True(t, pe.Filename, "a file and a list of files are both files")
		})
	}
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
	_, svc, diags := compileService(t, spec)
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
			`{type: array, items: ` + binary + `}`, true, true},
		{"untyped and byte, which is base64 text (GitHub #1214)", `{description: the file}`, `{type: string, format: byte}`, false, false},
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

// TestPartEncoding_AMergedUnionReadsThroughItsVariants holds a part whose
// merged type is a union to the flags its variants give, in either branch
// order: it repeats where a variant is a list, and is files where every value
// it admits is a file.
func TestPartEncoding_AMergedUnionReadsThroughItsVariants(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		a, b     string
		filename bool
		multi    bool
	}{
		{"untyped and a type array with an array", `{description: d}`,
			`{type: [array, string], items: {type: string}}`, false, true},
		{"untyped and a file or a list of files", `{description: d}`,
			`{oneOf: [{$ref: '#/components/schemas/Blob'}, {type: array, items: {$ref: '#/components/schemas/Blob'}}]}`,
			true, true},
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
	_, svc, diags := compileService(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)
	enc := openapitest.FirstOp(t, svc).Request.Contents[0].Encoding
	require.Len(t, enc, 1)
	for _, pe := range enc {
		assert.Equal(t, ir.PartEncoding{ContentTypes: []string{"text/plain"}}, pe,
			"the kept integer decides: not a file, and an integer's content type")
	}
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

// TestPartEncodings_ReadsTheFlattenedComposition holds bodyParts to the
// composition the schema lowering flattens (docs/compilers/openapi.md#allof-classification),
// in both declaration orders of the parts and of the encoding map. A part
// nested n wrappers deep is keyed on the property the lowering models there, or
// a warning at the body schema names the bound that cut the walk short and the
// entries for the parts beyond it are kept whole.
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
			mediaPtr := requestMedia(tc.media)
			bodyPtr := ids.Ptr("paths", "/u", "post", "requestBody", "content", tc.media, "schema")
			forward := nestedPartSpec(tc.n, tc.residue, tc.shape, tc.media, []string{"file", "meta"}, []string{"file", "meta"})
			reversed := nestedPartSpec(tc.n, tc.residue, tc.shape, tc.media, []string{"meta", "file"}, []string{"meta", "file"})
			var got [2]map[ir.PropID]ir.PartEncoding
			for i, src := range []string{forward, reversed} {
				_, svc, diags := compileService(t, src)
				openapitest.RequireNoErrorDiags(t, diags)
				content := openapitest.FirstOp(t, svc).Request.Contents[0]
				got[i] = content.Encoding
				if tc.capped {
					assert.Equal(t, []string{"file", "meta"}, keptEncodingNames(content.Unmodeled),
						"the entries for the parts beyond the cut are kept whole")
					for _, part := range []string{"file", "meta"} {
						assert.Equal(t, unkeyedMessage(part), openapitest.DiagMessageAt(t, diags,
							diag.DegradedConstruct, ir.SeverityInfo, mediaPtr+"/encoding/"+part), "%s is announced", part)
					}
					msg := openapitest.DiagMessageAt(t, diags, diag.DegradedConstruct, ir.SeverityWarning, string(bodyPtr))
					assert.Equal(t, fmt.Sprintf("form body composition is nested past the flattening depth of %d, "+
						"so a part reached only below it has no Content.Encoding key", schema.MaxAllOfFlattenDepth), msg,
						"a walk cut short by the bound says so")
					if tc.shape == shapeBesideNote {
						note := ir.PropID("p/openapi" + bodyPtr + ids.Ptr("properties", "note"))
						assert.Contains(t, got[i], note, "the part the body declares is keyed beside the cut")
					}
					continue
				}
				assert.Empty(t, keptEncodingNames(content.Unmodeled), "every entry names a part the walk reached")
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
// has no entry, a warning at the body schema says why, and the entry written
// for it is kept whole.
func TestPartEncodings_RefHopBoundIsReported(t *testing.T) {
	t.Parallel()
	var comps strings.Builder
	const links = operation.MaxPartRefHops + 4
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
	_, svc, diags := compileService(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)
	msg := openapitest.DiagMessageAt(t, diags, diag.DegradedConstruct, ir.SeverityWarning,
		"/paths/~1u/post/requestBody/content/multipart~1form-data/schema")
	assert.Equal(t, fmt.Sprintf("form body composition follows $refs and union branches past %d hops, "+
		"so a part reached only beyond them has no Content.Encoding key", operation.MaxPartRefHops), msg,
		"the bound is named at the body schema")
	content := openapitest.FirstOp(t, svc).Request.Contents[0]
	assert.Empty(t, content.Encoding, "the part beyond it has no entry")
	assert.Equal(t, []string{"file"}, keptEncodingNames(content.Unmodeled), "the entry written for it is kept whole")
	assert.JSONEq(t, pngEntry, string(content.Unmodeled["openapi:encoding/file"].Value))
	assert.Equal(t, unkeyedMessage("file"), openapitest.DiagMessageAt(t, diags, diag.DegradedConstruct, ir.SeverityInfo,
		requestMedia(multipartForm)+"/encoding/file"), "and announced")
}

// TestPartEncodings_ShorterPathReentersTarget spells an allOf chain as long as
// the hop bound, which the body reaches twice: from its head and from its last
// link. Whichever the body names first, the target at the end is two hops from
// the body by the short path, so both parts are keyed and no bound is reported.
func TestPartEncodings_ShorterPathReentersTarget(t *testing.T) {
	t.Parallel()
	head, last := "L1", "L"+strconv.Itoa(operation.MaxPartRefHops)
	target := "L" + strconv.Itoa(operation.MaxPartRefHops+1)
	ref := func(name string) string { return "{$ref: '#/components/schemas/" + name + "'}" }
	cases := []struct{ name, allOf string }{
		{head + ", " + last, "[" + ref(head) + ", " + ref(last) + "]"},
		{last + ", " + head, "[" + ref(last) + ", " + ref(head) + "]"},
	}
	var comps strings.Builder
	for i := 1; i <= operation.MaxPartRefHops; i++ {
		comps.WriteString("    L" + strconv.Itoa(i) + ": {allOf: [{$ref: '#/components/schemas/L" + strconv.Itoa(i+1) + "'}]}\n")
	}
	comps.WriteString("    " + target + ": {type: object, properties: {file: {type: string, format: binary}, meta: {type: string}}}\n")
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
			_, svc, diags := compileService(t, spec)
			openapitest.RequireNoErrorDiags(t, diags)
			got := openapitest.FirstOp(t, svc).Request.Contents[0].Encoding
			for _, part := range []string{"file", "meta"} {
				id := ir.PropID("p/openapi/components/schemas/" + target + string(ids.Ptr("properties", part)))
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
			_, svc, diags := compileService(t, spec)
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
	const partRefHops = operation.MaxPartRefHops
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
	target := "L" + strconv.Itoa(partRefHops+1)
	pastHops := fmt.Sprintf("past %d hops, so a part reached only beyond them", partRefHops)
	pastDepth := fmt.Sprintf("depth of %d, so a part reached only below it", schema.MaxAllOfFlattenDepth)
	cases := []struct{ name, first, second, comps, keyedAt, bound string }{
		{"hops, chain first", "L0", target, chain.String(), target, pastHops},
		{"hops, target first", target, "L0", chain.String(), target, pastHops},
		{"depth, wrappers first", "Deep", leaf, "    Deep: " + deep + "\n", leaf, pastDepth},
		{"depth, pointer first", leaf, "Deep", "    Deep: " + deep + "\n", leaf, pastDepth},
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
			_, svc, diags := compileService(t, spec)
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
			_, svc, diags := compileService(t, spec)
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
// the encoding of the part named file, which it must keep.
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
	_, svc, diags := compileService(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)
	enc := openapitest.FirstOp(t, svc).Request.Contents[0].Encoding
	var found []ir.PartEncoding
	for id, pe := range enc {
		if strings.HasSuffix(string(id), "/properties/file") {
			found = append(found, pe)
		}
	}
	require.Len(t, found, 1, "the part file has one entry: %v", enc)
	return found[0]
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

// unkeyedEntry is one encoding entry a form body's part walk finds no part for:
// the key as written, the key as a JSON Pointer token and Unmodeled scope
// spell it (esc, the key itself when empty), and the canonical JSON the IR
// holds for the entry's value.
type unkeyedEntry struct{ name, esc, value string }

func (e unkeyedEntry) token() string {
	if e.esc != "" {
		return e.esc
	}
	return e.name
}

// unkeyedMessage is the message announcing the entry named name on a form
// body, spelled out here and not derived from the code under test.
func unkeyedMessage(name string) string {
	return fmt.Sprintf("encoding entry %q matches no property read from the form body's schema, "+
		"so Content.Encoding has no key for it; kept verbatim under Unmodeled", name)
}

// unknownKeyMessage is the warning that the entry named name names no property
// of its body's schema, spelled out here and not derived from the code under
// test.
func unknownKeyMessage(name string) string {
	return fmt.Sprintf("encoding entry %q names no property the body's schema declares, which OpenAPI requires of an "+
		"encoding key, so the entry has no effect", name)
}

// unkeyedPartMessage is the message announcing the entry named name for a part
// the body declares and its lowered type holds no property for.
func unkeyedPartMessage(name string) string {
	return fmt.Sprintf("encoding entry %q names a part the form body's schema declares, but the type its schema "+
		"lowered to holds no property of that name, so Content.Encoding has no key for it; kept verbatim under Unmodeled", name)
}

// unkeyedFileMessage is the message announcing the entry named name on a file
// body, spelled out here and not derived from the code under test.
func unkeyedFileMessage(name string) string {
	return fmt.Sprintf("encoding entry %q belongs to a body that is file content, which has no parts, "+
		"so Content.Encoding has no key for it; kept verbatim under Unmodeled", name)
}

// formDoc spells a document of the given version, "" being 3.1.0, whose POST /u
// takes a request body holding one content of media type mt, with the schema
// and encoding given as flow YAML (no schema line where schemaSrc is empty) and
// the rest of the document after the paths.
func formDoc(version, mt, schemaSrc, encodingSrc, rest string) string {
	if version == "" {
		version = "3.1.0"
	}
	schemaLine := ""
	if schemaSrc != "" {
		schemaLine = "            schema: " + schemaSrc + "\n"
	}
	return openapitest.PathsSpecVer(version, `  /u:
    post:
      operationId: up
      requestBody:
        content:
          "`+mt+`":
`+schemaLine+`            encoding: `+encodingSrc+`
      responses: {"200": {description: ok}}
`+rest)
}

// responseFormDoc spells, at version, a document whose GET /u 200 response
// holds one content of media type mt with the schema and encoding given.
func responseFormDoc(version, mt, schemaSrc, encodingSrc string) string {
	return openapitest.PathsSpecVer(version, `  /u:
    get:
      operationId: down
      responses:
        "200":
          description: ok
          content:
            "`+mt+`":
              schema: `+schemaSrc+`
              encoding: `+encodingSrc+"\n")
}

// requestMedia is the pointer of the media type object formDoc writes.
func requestMedia(mt string) string {
	return string(ids.Ptr("paths", "/u", "post", "requestBody", "content", mt))
}

// partProp is the PropID of the property name of the schema at mediaPtr.
func partProp(mediaPtr, name string) ir.PropID {
	return ir.PropID("p/openapi" + mediaPtr + string(ids.Ptr("schema", "properties", name)))
}

const (
	multipartForm = "multipart/form-data"
	urlEncoded    = "application/x-www-form-urlencoded"
	pngEntry      = `{"contentType":"image/png"}`
	pngSource     = "{contentType: image/png}"
	unionSource   = "{oneOf: [{type: object, properties: {file: {type: string, format: binary}}}, " +
		"{type: object, properties: {url: {type: string}}}]}"
	fileForm = "{type: object, properties: {file: {type: string, format: binary}}}"
)

// formContent returns the single content of the operation's request body or,
// where response is set, of its first response.
func formContent(t *testing.T, svc ir.Service, response bool) ir.Content {
	t.Helper()
	op := openapitest.FirstOp(t, svc)
	if response {
		require.NotNil(t, op.Responses[0].Payload)
		require.Len(t, op.Responses[0].Payload.Contents, 1)
		return op.Responses[0].Payload.Contents[0]
	}
	require.Len(t, op.Request.Contents, 1)
	return op.Request.Contents[0]
}

// keptEncodingNames returns, sorted, what follows openapi:encoding/ in each
// Unmodeled key under it: a kept entry's escaped key, or a part's own record
// such as a/x-k.
func keptEncodingNames(u ir.Unmodeled) []string {
	var out []string
	for key := range u {
		if rest, ok := strings.CutPrefix(key, "openapi:encoding/"); ok {
			out = append(out, rest)
		}
	}
	slices.Sort(out)
	return out
}

// encodingTypes projects a content's Encoding onto the content types of each
// part, which is what the rows below say about a part that is keyed.
func encodingTypes(enc map[ir.PropID]ir.PartEncoding) map[ir.PropID][]string {
	out := make(map[ir.PropID][]string, len(enc))
	for id, pe := range enc {
		out[id] = pe.ContentTypes
	}
	return out
}

// TestPartEncodings_AnEntryNamingNoPartIsKeptWhole holds every encoding entry
// whose key the part walk finds no part for to one outcome, whatever the body
// is: the entry is kept whole, as written, under the content's Unmodeled at
// openapi:encoding/<key>, reason no_ir_home, located at the entry, with one info
// at the entry and no error, and, where the body's schema leaves no key
// undecided, a warning that it names no property; the parts the walk does find
// stay keyed with no record of their own; and entries are announced in source
// order. The rows are the body shapes and the positions of a form content.
func TestPartEncodings_AnEntryNamingNoPartIsKeptWhole(t *testing.T) {
	t.Parallel()
	mfd := requestMedia(multipartForm)
	const mixedResponse = "/paths/~1u/get/responses/200/content/multipart~1mixed"
	cases := []struct {
		name     string
		doc      string
		media    string
		response bool
		file     bool
		keyed    map[ir.PropID][]string
		kept     []unkeyedEntry
		warns    []string
		// open marks a body whose schema leaves a key undecided, a catch-all
		// admitting it or a position the walk does not read declaring it, so a
		// key naming none of its parts draws no warning that it names nothing.
		open bool
	}{
		{
			name:  "a key beside a keyed part",
			doc:   formDoc("", multipartForm, "{type: object, properties: {a: {type: string}}}", "{a: {contentType: text/plain}, ghost: "+pngSource+"}", ""),
			media: mfd, keyed: map[ir.PropID][]string{partProp(mfd, "a"): {"text/plain"}},
			kept: []unkeyedEntry{{name: "ghost", value: pngEntry}},
		},
		{
			name:  "a key differing from a part's name only in case",
			doc:   formDoc("", multipartForm, fileForm, "{File: "+pngSource+"}", ""),
			media: mfd, keyed: map[ir.PropID][]string{partProp(mfd, "file"): {"application/octet-stream"}},
			kept: []unkeyedEntry{{name: "File", value: pngEntry}},
		},
		{
			name:  "a binary string body",
			doc:   formDoc("", multipartForm, "{type: string, format: binary}", "{file: "+pngSource+"}", ""),
			media: mfd, file: true, keyed: map[ir.PropID][]string{},
			kept: []unkeyedEntry{{name: "file", value: pngEntry}},
		},
		{
			name:  "a $ref to a binary string body",
			doc:   formDoc("", multipartForm, "{$ref: '#/components/schemas/Bin'}", "{file: "+pngSource+"}", "components:\n  schemas:\n    Bin: {type: string, format: binary}\n"),
			media: mfd, file: true, keyed: map[ir.PropID][]string{},
			kept: []unkeyedEntry{{name: "file", value: pngEntry}},
		},
		{
			name:  "a url-encoded binary string body",
			doc:   formDoc("", urlEncoded, "{type: string, format: binary}", "{file: "+pngSource+"}", ""),
			media: requestMedia(urlEncoded), file: true, keyed: map[ir.PropID][]string{},
			kept: []unkeyedEntry{{name: "file", value: pngEntry}},
		},
		{
			name:  "a binary string body under a media type in mixed case with parameters",
			doc:   formDoc("", "Multipart/Form-Data; boundary=x", "{type: string, format: binary}", "{file: "+pngSource+"}", ""),
			media: requestMedia("Multipart/Form-Data; boundary=x"), file: true, keyed: map[ir.PropID][]string{},
			kept: []unkeyedEntry{{name: "file", value: pngEntry}},
		},
		{
			name:  "a closed body",
			doc:   formDoc("", multipartForm, "{type: object, properties: {a: {type: string}}, additionalProperties: false}", "{ghost: "+pngSource+"}", ""),
			media: mfd, keyed: map[ir.PropID][]string{partProp(mfd, "a"): {"text/plain"}},
			kept: []unkeyedEntry{{name: "ghost", value: pngEntry}},
		},
		{
			name:  "the name of a nested property",
			doc:   formDoc("", multipartForm, "{type: object, properties: {a: {type: object, properties: {b: {type: string}}}}}", "{b: "+pngSource+"}", ""),
			media: mfd, keyed: map[ir.PropID][]string{partProp(mfd, "a"): {"application/json"}},
			kept: []unkeyedEntry{{name: "b", value: pngEntry}},
		},
		{
			name:  "the golden spec's map body",
			doc:   formDoc("", multipartForm, "{type: object, additionalProperties: {type: string, description: d}}", `{"*": {contentType: "*/*"}}`, ""),
			media: mfd, keyed: map[ir.PropID][]string{},
			open: true,
			kept: []unkeyedEntry{{name: "*", value: `{"contentType":"*/*"}`}},
		},
		{
			name:  "properties beside a catch-all",
			doc:   formDoc("", multipartForm, "{type: object, properties: {a: {type: string}}, additionalProperties: {type: string, format: binary}}", "{other: "+pngSource+"}", ""),
			media: mfd, keyed: map[ir.PropID][]string{partProp(mfd, "a"): {"text/plain"}},
			open: true,
			kept: []unkeyedEntry{{name: "other", value: pngEntry}},
		},
		{
			name:  "patternProperties alone",
			doc:   formDoc("", multipartForm, "{type: object, patternProperties: {'^f': {type: string}}}", "{foo: "+pngSource+"}", ""),
			media: mfd, keyed: map[ir.PropID][]string{},
			open: true,
			kept: []unkeyedEntry{{name: "foo", value: pngEntry}},
		},
		{
			name:  "an object declaring nothing",
			doc:   formDoc("", multipartForm, "{type: object}", "{file: "+pngSource+"}", ""),
			media: mfd, keyed: map[ir.PropID][]string{},
			kept: []unkeyedEntry{{name: "file", value: pngEntry}},
		},
		{
			name:  "a schema declaring nothing",
			doc:   formDoc("", multipartForm, "{}", "{file: "+pngSource+"}", ""),
			media: mfd, keyed: map[ir.PropID][]string{},
			kept: []unkeyedEntry{{name: "file", value: pngEntry}},
		},
		{
			name:  "no schema",
			doc:   formDoc("", multipartForm, "", "{file: "+pngSource+"}", ""),
			media: mfd, keyed: map[ir.PropID][]string{},
			kept: []unkeyedEntry{{name: "file", value: pngEntry}},
		},
		{
			name:  "a $dynamicRef body",
			doc:   formDoc("", multipartForm, "{$dynamicRef: '#/components/schemas/F'}", "{file: "+pngSource+"}", "components:\n  schemas:\n    F: "+fileForm+"\n"),
			media: mfd, keyed: map[ir.PropID][]string{},
			open: true,
			kept: []unkeyedEntry{{name: "file", value: pngEntry}},
		},
		{
			name: "a $ref beside properties",
			doc: formDoc("", multipartForm, "{$ref: '#/components/schemas/F', properties: {extra: {type: string, format: binary}}}",
				"{extra: "+pngSource+", file: {contentType: image/gif}}", "components:\n  schemas:\n    F: "+fileForm+"\n"),
			media: mfd, keyed: map[ir.PropID][]string{"p/openapi/components/schemas/F/properties/file": {"image/gif"}},
			open: true,
			kept: []unkeyedEntry{{name: "extra", value: pngEntry}},
		},
		{
			name:  "a url-encoded body",
			doc:   formDoc("", urlEncoded, "{type: object, properties: {a: {type: string}}}", "{ghost: {style: form}}", ""),
			media: requestMedia(urlEncoded), keyed: map[ir.PropID][]string{partProp(requestMedia(urlEncoded), "a"): {"text/plain"}},
			kept: []unkeyedEntry{{name: "ghost", value: `{"style":"form"}`}},
		},
		{
			name:  "a media type written in mixed case with parameters",
			doc:   formDoc("", "Multipart/Form-Data; boundary=x", "{type: object, properties: {a: {type: string}}}", "{ghost: "+pngSource+"}", ""),
			media: requestMedia("Multipart/Form-Data; boundary=x"), keyed: map[ir.PropID][]string{partProp(requestMedia("Multipart/Form-Data; boundary=x"), "a"): {"text/plain"}},
			kept: []unkeyedEntry{{name: "ghost", value: pngEntry}},
		},
		{
			name:  "a 3.1 response, which load already warns about",
			doc:   responseFormDoc("3.1.0", "multipart/mixed", "{type: object, properties: {a: {type: string}}}", "{ghost: "+pngSource+"}"),
			media: mixedResponse, response: true, keyed: map[ir.PropID][]string{partProp(mixedResponse, "a"): {"text/plain"}},
			kept:  []unkeyedEntry{{name: "ghost", value: pngEntry}},
			warns: []string{"openapi/validation/validation-no-effect"},
		},
		{
			name:  "a 3.2 response",
			doc:   responseFormDoc("3.2.0", "multipart/mixed", "{type: object, properties: {a: {type: string}}}", "{ghost: "+pngSource+"}"),
			media: mixedResponse, response: true, keyed: map[ir.PropID][]string{partProp(mixedResponse, "a"): {"text/plain"}},
			kept: []unkeyedEntry{{name: "ghost", value: pngEntry}},
		},
		{
			name: "a request body component",
			doc: openapitest.PathsSpec(`  /u:
    post:
      operationId: up
      requestBody: {$ref: '#/components/requestBodies/Form'}
      responses: {"200": {description: ok}}
components:
  requestBodies:
    Form:
      content:
        multipart/form-data:
          schema: {type: object, properties: {a: {type: string}}}
          encoding: {ghost: ` + pngSource + `}
`),
			media: "/components/requestBodies/Form/content/multipart~1form-data", keyed: map[ir.PropID][]string{partProp("/components/requestBodies/Form/content/multipart~1form-data", "a"): {"text/plain"}},
			kept: []unkeyedEntry{{name: "ghost", value: pngEntry}},
		},
		{
			name: "a 3.2 media type component",
			doc: openapitest.PathsSpecVer("3.2.0", `  /u:
    post:
      operationId: up
      requestBody:
        content:
          multipart/form-data: {$ref: '#/components/mediaTypes/Form'}
      responses: {"200": {description: ok}}
components:
  mediaTypes:
    Form:
      schema: {type: object, properties: {a: {type: string}}}
      encoding: {ghost: `+pngSource+`}
`),
			media: "/components/mediaTypes/Form", keyed: map[ir.PropID][]string{partProp("/components/mediaTypes/Form", "a"): {"text/plain"}},
			kept: []unkeyedEntry{{name: "ghost", value: pngEntry}},
		},
		{
			name:  "keys holding a slash and a tilde, in an order that is not sorted",
			doc:   formDoc("", multipartForm, "{type: object}", "{m~n: "+pngSource+", a/b: {contentType: text/plain}}", ""),
			media: mfd, keyed: map[ir.PropID][]string{},
			kept: []unkeyedEntry{{name: "m~n", esc: "m~0n", value: pngEntry}, {name: "a/b", esc: "a~1b", value: `{"contentType":"text/plain"}`}},
		},
		{
			name:  "an entry declaring nothing",
			doc:   formDoc("", multipartForm, "{type: object, properties: {a: {type: string}}}", "{ghost: {}}", ""),
			media: mfd, keyed: map[ir.PropID][]string{partProp(mfd, "a"): {"text/plain"}},
			kept: []unkeyedEntry{{name: "ghost", value: `{}`}},
		},
		{
			name:  "an entry merged in with <<",
			doc:   formDoc("", multipartForm, "{type: object, properties: {a: {type: string}}}", "{<<: {ghost: "+pngSource+"}, a: {contentType: text/plain}}", ""),
			media: mfd, keyed: map[ir.PropID][]string{partProp(mfd, "a"): {"text/plain"}},
			kept: []unkeyedEntry{{name: "ghost", value: pngEntry}},
		},
		{
			name:  "an entry that is an alias",
			doc:   formDoc("", multipartForm, "{type: object}", "{first: &e "+pngSource+", second: *e}", ""),
			media: mfd, keyed: map[ir.PropID][]string{},
			kept: []unkeyedEntry{{name: "first", value: pngEntry}, {name: "second", value: pngEntry}},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, svc, diags := compileService(t, tc.doc)
			openapitest.RequireNoErrorDiags(t, diags)
			for _, code := range tc.warns {
				openapitest.AssertHasCode(t, diags, code, ir.SeverityWarning)
			}
			content := formContent(t, svc, tc.response)

			assert.Equal(t, tc.file, content.File != nil, "whether the body lowers as a file body")
			assert.Equal(t, tc.keyed, encodingTypes(content.Encoding),
				"the parts the walk finds are keyed, and an entry for no part keys nothing")
			var wantNames []string
			var wantPointers []jsontext.Pointer
			for _, e := range tc.kept {
				key := "openapi:encoding/" + e.token()
				at := tc.media + "/encoding/" + e.token()
				wantNames = append(wantNames, e.token())
				wantPointers = append(wantPointers, jsontext.Pointer(at))
				rec, ok := content.Unmodeled[key]
				if !assert.True(t, ok, "%s is kept at %s; got %v", e.name, key, keptEncodingNames(content.Unmodeled)) {
					continue
				}
				assert.Equal(t, ir.ReasonNoIRHome, rec.Reason, "%s: reason", e.name)
				assert.JSONEq(t, e.value, string(rec.Value), "%s: kept as written", e.name)
				assert.Equal(t, jsontext.Pointer(at), rec.Provenance.Pointer, "%s: located at the entry", e.name)
				msg := openapitest.DiagMessageAt(t, diags, diag.DegradedConstruct, ir.SeverityInfo, at)
				want := unkeyedMessage(e.name)
				if tc.file {
					want = unkeyedFileMessage(e.name)
				}
				assert.Equal(t, want, msg, "%s: the one announcement, naming the body's kind", e.name)
				if !tc.open {
					wantPointers = append(wantPointers, jsontext.Pointer(at))
					assert.Equal(t, unknownKeyMessage(e.name),
						openapitest.DiagMessageAt(t, diags, diag.UnknownEncodingKey, ir.SeverityWarning, at), "%s: names no property", e.name)
				}
			}
			slices.Sort(wantNames)
			assert.Equal(t, wantNames, keptEncodingNames(content.Unmodeled),
				"the only records under openapi:encoding/ are the entries kept whole")
			assert.Equal(t, wantPointers, diagPointersUnder(diags, tc.media+"/encoding/"),
				"one info per entry kept, and a warning beside each naming no property, in source order, and nothing else beneath the entries")
		})
	}
}

// TestPartEncodings_AUnionBranchsPartIsKeyed holds a part only a oneOf or an
// anyOf branch declares to a Content.Encoding key naming the first variant
// declaring it, with its entry's fields and its own flags, and no record or
// finding for the entry, in each spelling: a union body, an anyOf, a $ref to
// one and a $ref branch, a union on an allOf branch or beside properties, and
// a union nested in a branch. A name two branches declare is one part, keyed
// by the first: it repeats where one declaration does, and is a file only
// where each is.
func TestPartEncodings_AUnionBranchsPartIsKeyed(t *testing.T) {
	t.Parallel()
	mfd := requestMedia(multipartForm)
	at := func(rest string) ir.PropID { return ir.PropID("p/openapi" + mfd + "/schema/" + rest) }
	const comps = "components:\n  schemas:\n    F: " + fileForm + "\n    U: " + unionSource + "\n"
	png := ir.PartEncoding{ContentTypes: []string{"image/png"}, Filename: true}
	url := ir.PartEncoding{ContentTypes: []string{"text/plain"}}
	cases := []struct {
		name, schema, encoding string
		want                   map[ir.PropID]ir.PartEncoding
	}{
		{"a oneOf body", unionSource, "{file: " + pngSource + "}",
			map[ir.PropID]ir.PartEncoding{at("oneOf/0/properties/file"): png, at("oneOf/1/properties/url"): url}},
		{"an anyOf body", "{anyOf: " + strings.TrimPrefix(unionSource, "{oneOf: "), "{file: " + pngSource + "}",
			map[ir.PropID]ir.PartEncoding{at("anyOf/0/properties/file"): png, at("anyOf/1/properties/url"): url}},
		{"a $ref to a oneOf", "{$ref: '#/components/schemas/U'}", "{file: " + pngSource + "}",
			map[ir.PropID]ir.PartEncoding{
				"p/openapi/components/schemas/U/oneOf/0/properties/file": png,
				"p/openapi/components/schemas/U/oneOf/1/properties/url":  url,
			}},
		{"a oneOf with a $ref branch", "{oneOf: [{$ref: '#/components/schemas/F'}, {type: object, properties: {url: {type: string}}}]}",
			"{file: " + pngSource + "}",
			map[ir.PropID]ir.PartEncoding{"p/openapi/components/schemas/F/properties/file": png, at("oneOf/1/properties/url"): url}},
		{"a oneOf inside an allOf", "{allOf: [" + unionSource + "]}", "{file: " + pngSource + "}",
			map[ir.PropID]ir.PartEncoding{at("allOf/0/oneOf/0/properties/file"): png, at("allOf/0/oneOf/1/properties/url"): url}},
		{"a oneOf beside properties",
			"{type: object, properties: {meta: {type: string}}, oneOf: [{properties: {file: {type: string, format: binary}}}, {properties: {url: {type: string}}}]}",
			"{file: " + pngSource + ", meta: {contentType: text/plain}}",
			map[ir.PropID]ir.PartEncoding{
				at("oneOf/0/properties/file"): png, at("oneOf/1/properties/url"): url, partProp(mfd, "meta"): url,
			}},
		{"a union nested in a branch",
			"{oneOf: [{oneOf: [" + fileForm + ", {type: object, properties: {b: {type: integer}}}]}, {type: object, properties: {url: {type: string}}}]}",
			"{file: " + pngSource + "}",
			map[ir.PropID]ir.PartEncoding{
				at("oneOf/0/oneOf/0/properties/file"): png, at("oneOf/0/oneOf/1/properties/b"): url, at("oneOf/1/properties/url"): url,
			}},
		{"a name two branches declare, a file in one and a list in the other",
			"{oneOf: [" + fileForm + ", {type: object, properties: {file: {type: array, items: {type: string, format: binary}}}}]}", "{}",
			map[ir.PropID]ir.PartEncoding{at("oneOf/0/properties/file"): {
				ContentTypes: []string{"application/octet-stream"}, Multi: true, Filename: true,
			}}},
		{"a name two branches declare, a file in one and text in the other",
			"{oneOf: [" + fileForm + ", {type: object, properties: {file: {type: string}}}]}", "{}",
			map[ir.PropID]ir.PartEncoding{at("oneOf/0/properties/file"): {}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, svc, diags := compileService(t, formDoc("", multipartForm, tc.schema, tc.encoding, comps))
			openapitest.RequireNoErrorDiags(t, diags)
			content := formContent(t, svc, false)
			want := maps.Clone(tc.want)
			for id, pe := range want {
				if partEncodingIsEmpty(pe) {
					delete(want, id)
				}
			}
			assert.Equal(t, want, content.Encoding)
			assert.Empty(t, keptEncodingNames(content.Unmodeled), "no entry is kept whole")
			assert.Empty(t, diagPointersUnder(diags, mfd+"/encoding/"), "and none draws a finding")
		})
	}
}

// partEncodingIsEmpty reports whether pe states nothing, which the lowering
// leaves out of Content.Encoding: a row expecting it states that the part
// keys nothing.
func partEncodingIsEmpty(pe ir.PartEncoding) bool {
	return len(pe.ContentTypes) == 0 && !pe.Multi && !pe.Filename
}

// TestPartEncodings_ANestedKeyNamingNoPropertyIsWarnedOf holds each key of the
// nested encoding a 3.2 Encoding Object writes to the properties of the value
// it applies to: a key no property names draws the warning at its own entry,
// the items of an array part are that value, and a catch-all beside them
// leaves every key undecided, so none is warned of.
func TestPartEncodings_ANestedKeyNamingNoPropertyIsWarnedOf(t *testing.T) {
	t.Parallel()
	mfd := requestMedia(multipartForm)
	for _, tc := range []struct {
		name, part string
		warned     []string
	}{
		{"an object part", "{type: object, properties: {a: {type: string}}}", []string{"ghost"}},
		{"an array part", "{type: array, items: {type: object, properties: {a: {type: string}}}}", []string{"ghost"}},
		{"a part with a catch-all", "{type: object, properties: {a: {type: string}}, additionalProperties: true}", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, _, diags := compileService(t, formDoc("3.2.0", multipartForm, "{type: object, properties: {meta: "+tc.part+"}}",
				"{meta: {contentType: multipart/mixed, encoding: {a: {contentType: text/plain}, ghost: {}}}}", ""))
			openapitest.RequireNoErrorDiags(t, diags)
			var got []string
			for _, d := range diags {
				if d.Code == diag.UnknownEncodingKey {
					got = append(got, string(d.Provenance.Pointer))
					assert.Equal(t, ir.SeverityWarning, d.Severity)
					assert.Contains(t, d.Message, "names no property the part's schema declares")
				}
			}
			var want []string
			for _, key := range tc.warned {
				want = append(want, mfd+"/encoding/meta/encoding/"+key)
			}
			assert.Equal(t, want, got)
		})
	}
}

// TestPartEncodings_AnUnkeyedEntryLowersNothingItDeclares holds an entry kept
// whole to being one record, with the findings at the entry and none beneath
// it: its header schema registers no type, and its unknown key, extension and
// nested encoding draw no finding and no record of their own, which the same
// entry under a part's name does draw. The rule records why it is not judged
// (#1662).
func TestPartEncodings_AnUnkeyedEntryLowersNothingItDeclares(t *testing.T) {
	t.Parallel()
	const entry = "{contentType: image/png, headers: {X-A: {schema: {type: object, properties: {k: {type: string}}}}}, " +
		"x-k: 1, bogus: 2, encoding: {inner: {contentType: text/plain}}}"
	const written = `{"bogus":2,"contentType":"image/png","encoding":{"inner":{"contentType":"text/plain"}},` +
		`"headers":{"X-A":{"schema":{"properties":{"k":{"type":"string"}},"type":"object"}}},"x-k":1}`
	media := requestMedia(multipartForm)
	compileEntry := func(t *testing.T, name string) (*ir.Document, ir.Content, []ir.Diagnostic) {
		t.Helper()
		doc, svc, diags := compileService(t, formDoc("3.2.0", multipartForm,
			"{type: object, properties: {a: {type: string}}}", "{"+name+": "+entry+"}", ""))
		openapitest.RequireNoErrorDiags(t, diags)
		return doc, formContent(t, svc, false), diags
	}
	typesUnder := func(doc *ir.Document, name string) []ir.TypeID {
		var out []ir.TypeID
		for id, td := range doc.Types {
			if strings.HasPrefix(string(td.Common().Provenance.Pointer), media+"/encoding/"+name+"/") {
				out = append(out, id)
			}
		}
		return out
	}

	walked, walkedContent, walkedDiags := compileEntry(t, "a")
	require.NotEmpty(t, typesUnder(walked, "a"), "a part's header schema registers a type, so the scan below can see one")
	require.True(t, openapitest.HasDiag(walkedDiags, diag.UnknownObjectKey), "a part's unknown key is reported")
	require.Contains(t, walkedContent.Unmodeled, "openapi:encoding/a/x-k", "a part's extension has a record of its own")
	require.Contains(t, walkedContent.Unmodeled, "openapi:encoding/a/encoding", "a part's nested encoding has a record of its own")

	doc, content, diags := compileEntry(t, "ghost")
	assert.Empty(t, typesUnder(doc, "ghost"), "no type is registered for the header schema of an entry kept whole")
	assert.Equal(t, []string{"ghost"}, keptEncodingNames(content.Unmodeled), "one record, and none for what the entry declares")
	assert.JSONEq(t, written, string(content.Unmodeled["openapi:encoding/ghost"].Value))
	assert.False(t, openapitest.HasDiag(diags, diag.UnknownObjectKey), "the entry's unknown key is not judged")
	ghost := jsontext.Pointer(media + "/encoding/ghost")
	assert.Equal(t, []jsontext.Pointer{ghost, ghost}, diagPointersUnder(diags, media+"/encoding/"),
		"the info keeping the entry and the warning that it names no property, and nothing beneath the entry")
}

// TestPartEncodings_AnEncodingOutsideAFormIsKeptWhole holds each entry of an
// encoding written under a media type that is neither multipart nor form, a
// file body's or not, to being kept whole at openapi:encoding/<key>, with one
// info at the entry beside load's warning at the field, and no warning that
// the key names nothing: the field has no effect there, whatever it names.
func TestPartEncodings_AnEncodingOutsideAFormIsKeptWhole(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, mt, schema string }{
		{"a JSON body", "application/json", "{type: object, properties: {file: {type: string}}}"},
		{"a file body", "application/octet-stream", "{type: string, format: binary}"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, svc, diags := compileService(t, formDoc("", tc.mt, tc.schema, "{file: "+pngSource+", x: {}}", ""))
			content := formContent(t, svc, false)
			assert.Empty(t, content.Encoding)
			assert.Equal(t, []string{"file", "x"}, keptEncodingNames(content.Unmodeled), "each entry is kept whole")
			media := requestMedia(tc.mt)
			for _, name := range []string{"file", "x"} {
				at := media + "/encoding/" + name
				assert.Equal(t, ir.ReasonNoIRHome, content.Unmodeled["openapi:encoding/"+name].Reason)
				assert.Equal(t, fmt.Sprintf("encoding entry %q belongs to a media type that is neither multipart nor form, "+
					"where it has no effect, so Content.Encoding has no key for it; kept verbatim under Unmodeled", name),
					openapitest.DiagMessageAt(t, diags, diag.DegradedConstruct, ir.SeverityInfo, at))
			}
			assert.Equal(t, []jsontext.Pointer{jsontext.Pointer(media + "/encoding/file"), jsontext.Pointer(media + "/encoding/x")},
				diagPointersUnder(diags, media+"/encoding/"), "an info per entry, and no warning that a key names nothing")
			openapitest.AssertHasCode(t, diags, "openapi/validation/validation-no-effect", ir.SeverityWarning)
		})
	}
}

// diagPointersUnder returns the pointer of each of diags beneath prefix, in order.
func diagPointersUnder(diags []ir.Diagnostic, prefix string) []jsontext.Pointer {
	var out []jsontext.Pointer
	for _, d := range diags {
		if strings.HasPrefix(string(d.Provenance.Pointer), prefix) {
			out = append(out, d.Provenance.Pointer)
		}
	}
	return out
}

// TestPartEncodings_AnUnkeyedEntryThatIsNoObjectAddsNothingToLoadsError holds
// an entry whose value is not a mapping, null, a scalar or a list, to the one
// error load reports for it, under a key naming no part and under a part's name
// alike: the lowering adds no record and no second finding.
func TestPartEncodings_AnUnkeyedEntryThatIsNoObjectAddsNothingToLoadsError(t *testing.T) {
	t.Parallel()
	const mismatch = "openapi/validation/validation-type-mismatch"
	media := requestMedia(multipartForm)
	cases := []struct {
		name     string
		encoding string
		// reported names the entry load reports for key: the one written, or for
		// an alias the anchor, since load reads a node once.
		reported func(key string) string
	}{
		{"null", "{%s: null}", func(key string) string { return key }},
		{"a number", "{%s: 5}", func(key string) string { return key }},
		{"a string", "{%s: hello}", func(key string) string { return key }},
		{"a list", "{%s: [a]}", func(key string) string { return key }},
		{"an alias of null", "{anchor: &n null, %s: *n}", func(string) string { return "anchor" }},
	}
	for _, tc := range cases {
		for _, key := range []string{"ghost", "a"} {
			t.Run(tc.name+" under "+key, func(t *testing.T) {
				t.Parallel()
				_, svc, diags := compileService(t, formDoc("", multipartForm,
					"{type: object, properties: {a: {type: string}}}", fmt.Sprintf(tc.encoding, key), ""))
				want := []jsontext.Pointer{jsontext.Pointer(media + "/encoding/" + tc.reported(key))}
				var got []jsontext.Pointer
				for _, d := range diags {
					if d.Severity == ir.SeverityError {
						assert.Equal(t, mismatch, d.Code, "the only error is load's")
						got = append(got, d.Provenance.Pointer)
					}
				}
				assert.Equal(t, want, got, "one error for the entry that is no object")
				assert.Empty(t, diagPointersUnder(slices.DeleteFunc(slices.Clone(diags),
					func(d ir.Diagnostic) bool { return d.Code == mismatch }), media+"/encoding/"),
					"the lowering adds no finding of its own")
				assert.Empty(t, keptEncodingNames(formContent(t, svc, false).Unmodeled), "nothing is kept")
			})
		}
	}
}

// TestPartEncodings_AnEntryThatCannotBeKeptIsReportedNotAnnounced writes an
// entry naming no part with a key twice, which cannot be rendered as JSON. The
// construct is reported as reaching the IR in no form, and no info claims it
// was kept.
func TestPartEncodings_AnEntryThatCannotBeKeptIsReportedNotAnnounced(t *testing.T) {
	t.Parallel()
	media := requestMedia(multipartForm)
	at := media + "/encoding/ghost"
	_, svc, diags := compileService(t, formDoc("", multipartForm,
		"{type: object, properties: {a: {type: string}}}", "{ghost: {contentType: image/png, contentType: text/plain}}", ""))
	assert.Empty(t, keptEncodingNames(formContent(t, svc, false).Unmodeled), "nothing was written")
	openapitest.AssertHasCode(t, diags, diag.UnpreservableConstruct, ir.SeverityError)
	assert.True(t, openapitest.HasDiagCodeAt(diags, diag.UnpreservableConstruct, at), "reported at the entry")
	assert.Empty(t, openapitest.PreservationClaims(diags), "no announcement says the entry was kept")
}

// TestPartEncodings_AContentTypeWrittenTakesNoDefault holds a part whose entry
// writes a contentType naming no media type, which load reports, to no
// default content type: the entry wrote one, and a default would read as a
// valid value standing for a malformed one.
func TestPartEncodings_AContentTypeWrittenTakesNoDefault(t *testing.T) {
	t.Parallel()
	_, svc, diags := compileService(t, formDoc("", multipartForm,
		"{type: object, properties: {a: {type: string}}}", `{a: {contentType: "", headers: {X-A: {schema: {type: string}}}}}`, ""))
	openapitest.AssertHasCode(t, diags, "openapi/validation/validation-empty-value", ir.SeverityError)
	pe, ok := formContent(t, svc, false).Encoding[partProp(requestMedia(multipartForm), "a")]
	require.True(t, ok, "the part keeps its header")
	assert.Empty(t, pe.ContentTypes)
}

// TestPartEncodings_AnEntryKeptAtSettlementThatCannotBeKeptKeepsItsRecords
// holds an entry for a part no property holds, which cannot be kept whole
// since it writes a key twice, to the records its lowering kept of it, its
// extension among them, and to the refusal alone, with no announcement that
// it was kept whole.
func TestPartEncodings_AnEntryKeptAtSettlementThatCannotBeKeptKeepsItsRecords(t *testing.T) {
	t.Parallel()
	media := requestMedia(multipartForm)
	_, svc, diags := compileService(t, formDoc("", multipartForm,
		"{const: x, properties: {file: {type: string}}}", "{file: {x-k: 1, contentType: image/png, contentType: text/plain}}", ""))
	content := formContent(t, svc, false)
	assert.Empty(t, content.Encoding)
	assert.Equal(t, []string{"file/x-k"}, keptEncodingNames(content.Unmodeled), "the entry's own records stand")
	assert.True(t, openapitest.HasDiagCodeAt(diags, diag.UnpreservableConstruct, media+"/encoding/file"), "the refusal, at the entry")
	assert.False(t, openapitest.HasDiagCodeAt(diags, diag.DegradedConstruct, media+"/encoding/file"),
		"no announcement says the entry was kept whole")
}

// TestPartEncodings_AUnionPastItsBoundIsReported holds a body whose type nests
// more unions than the keying reads to a warning at the body schema, its
// parts the first unions declare keyed, and those past them keying nothing.
func TestPartEncodings_AUnionPastItsBoundIsReported(t *testing.T) {
	t.Parallel()
	var branches []string
	for i := range operation.MaxPartUnions + 6 {
		branches = append(branches, fmt.Sprintf("{oneOf: [{type: object, properties: {p%d: {type: string}}}, {type: string}]}", i))
	}
	_, svc, diags := compileService(t, formDoc("", multipartForm, "{oneOf: ["+strings.Join(branches, ", ")+"]}", "{}", ""))
	openapitest.RequireNoErrorDiags(t, diags)
	msg := openapitest.DiagMessageAt(t, diags, diag.DegradedConstruct, ir.SeverityWarning, requestMedia(multipartForm)+"/schema")
	assert.Equal(t, fmt.Sprintf("form body's type nests past %d unions, so a part only a union below them declares "+
		"has no Content.Encoding key", operation.MaxPartUnions), msg)
	enc := formContent(t, svc, false).Encoding
	assert.NotEmpty(t, enc, "the parts of the unions read are keyed")
	assert.Less(t, len(enc), operation.MaxPartUnions+6, "and those past them are not")
}

// TestPartEncodings_KeptEntriesDoNotDependOnDeclarationOrder compiles one form
// content with its encoding entries in two orders, and one document with its
// two paths in two orders, each writing an entry of the same name. The Contents
// come out equal. The order oracle (internal/harness diffOrderInvariants) reads
// no Content.Unmodeled, so this unit test holds the kept entries instead.
func TestPartEncodings_KeptEntriesDoNotDependOnDeclarationOrder(t *testing.T) {
	t.Parallel()
	contents := func(t *testing.T, src string) map[string]ir.Content {
		t.Helper()
		doc, diags := parseFull(t, src)
		openapitest.RequireNoErrorDiags(t, diags)
		out := map[string]ir.Content{}
		for _, g := range doc.Services[0].Groups {
			for _, op := range g.Operations {
				out[string(op.ID)] = op.Request.Contents[0]
			}
		}
		return out
	}
	schemaSrc := "{type: object, properties: {a: {type: string}}}"
	t.Run("entries", func(t *testing.T) {
		t.Parallel()
		forward := contents(t, formDoc("", multipartForm, schemaSrc,
			"{one: "+pngSource+", a: {contentType: text/plain}, two: {style: form}, three: {}}", ""))
		reversed := contents(t, formDoc("", multipartForm, schemaSrc,
			"{three: {}, two: {style: form}, a: {contentType: text/plain}, one: "+pngSource+"}", ""))
		require.Len(t, forward, 1)
		assert.Equal(t, []string{"one", "three", "two"}, keptEncodingNames(forward["op/openapi/paths/~1u/post"].Unmodeled))
		assert.Empty(t, cmp.Diff(forward, reversed), "the order of the entries changes nothing (-forward +reversed)")
	})
	t.Run("paths", func(t *testing.T) {
		t.Parallel()
		path := func(name string) string {
			return "  /" + name + `:
    post:
      operationId: ` + name + `
      requestBody:
        content:
          multipart/form-data:
            schema: ` + schemaSrc + `
            encoding: {ghost: ` + pngSource + `}
      responses: {"200": {description: ok}}
`
		}
		forward := contents(t, openapitest.PathsSpec(path("p")+path("q")))
		reversed := contents(t, openapitest.PathsSpec(path("q")+path("p")))
		require.Len(t, forward, 2)
		assert.Empty(t, cmp.Diff(forward, reversed), "the order of the paths changes nothing (-forward +reversed)")
	})
}

// TestPartEncodings_AKeptEntryNeverTakesAPartsRecord writes an entry whose key
// reads like a part's name, a slash and one of that part's keywords, beside the
// part. The key is escaped, so the part's extension and the entry each keep a
// record.
func TestPartEncodings_AKeptEntryNeverTakesAPartsRecord(t *testing.T) {
	t.Parallel()
	_, svc, diags := compileService(t, formDoc("", multipartForm, "{type: object, properties: {a: {type: string}}}",
		"{a: {contentType: text/plain, x-k: 1}, a/x-k: "+pngSource+"}", ""))
	openapitest.RequireNoErrorDiags(t, diags)
	got := map[string]string{}
	for key, rec := range formContent(t, svc, false).Unmodeled {
		got[key] = string(rec.Value)
	}
	assert.Equal(t, map[string]string{
		"openapi:encoding/a/x-k":  "1",
		"openapi:encoding/a~1x-k": pngEntry,
	}, got, "the part's extension and the entry kept whole each keep a record")
}

// TestPartEncodings_AnEntryNamingNoPartIsKeptWholeInCallbacksAndWebhooks holds
// the operations a callback and a webhook declare, which lower their request
// bodies as a path's operations do, to the one outcome: the entry is kept whole
// with one info at it.
func TestPartEncodings_AnEntryNamingNoPartIsKeptWholeInCallbacksAndWebhooks(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, openapitest.PathsSpec(`  /u:
    post:
      operationId: up
      callbacks:
        done:
          '{$request.body#/url}':
            post:
              operationId: callback
              requestBody:
                content:
                  multipart/form-data:
                    schema: {type: object, properties: {a: {type: string}}}
                    encoding: {ghost: `+pngSource+`}
              responses: {"200": {description: ok}}
      responses: {"204": {description: ok}}
webhooks:
  hook:
    post:
      operationId: webhook
      requestBody:
        content:
          multipart/form-data:
            schema: {type: object, properties: {a: {type: string}}}
            encoding: {ghost: `+pngSource+`}
      responses: {"200": {description: ok}}
`))
	openapitest.RequireNoErrorDiags(t, diags)
	cases := []struct{ op, media string }{
		{"callback", "/paths/~1u/post/callbacks/done/{$request.body#~1url}/post/requestBody/content/multipart~1form-data"},
		{"webhook", "/webhooks/hook/post/requestBody/content/multipart~1form-data"},
	}
	for _, tc := range cases {
		content := openapitest.FindOp(t, doc, tc.op).Request.Contents[0]
		assert.Equal(t, []string{"ghost"}, keptEncodingNames(content.Unmodeled), tc.op)
		assert.Equal(t, unkeyedMessage("ghost"), openapitest.DiagMessageAt(t, diags,
			diag.DegradedConstruct, ir.SeverityInfo, tc.media+"/encoding/ghost"), tc.op)
	}
}
