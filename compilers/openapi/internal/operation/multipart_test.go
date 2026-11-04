// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package operation_test

import (
	"encoding/json/jsontext"
	"fmt"
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
			_, svc, diags := compileService(t, spec)
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
	_, svc, diags := compileService(t, spec)
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
// schema reading, which stays for a body the IR holds no model for, a union's
// branches among it.
func TestPartEncoding_FlagsOfABodyWithNoModelPropertyComeFromTheSchema(t *testing.T) {
	t.Parallel()
	file := onlyPartEncoding(t, `{enum: [x], properties: {file: {type: string, format: binary}}}`)
	assert.True(t, file.Filename, "a binary part is a file")
	assert.False(t, file.Multi)

	encoded := onlyPartEncoding(t, `{enum: [x], properties: {file: {type: string, format: byte}}}`)
	assert.True(t, encoded.Filename, "a base64 part carries a filename, as a typed one does (GitHub #1214)")

	files := onlyPartEncoding(t,
		`{enum: [x], properties: {file: {type: array, items: {type: string, format: binary}}}}`)
	assert.True(t, files.Multi, "an array part repeats")
	assert.True(t, files.Filename, "and an array of files is files")

	texts := onlyPartEncoding(t,
		`{enum: [x], properties: {file: {oneOf: [{type: string}, {type: array, items: {type: string}}]}}}`)
	assert.True(t, texts.Multi, "a union with an array branch repeats")
	assert.False(t, texts.Filename)

	either := onlyPartEncoding(t, `{enum: [x], properties: {file: {oneOf: [{$ref: '#/components/schemas/Blob'}, `+
		`{type: array, items: {$ref: '#/components/schemas/Blob'}}]}}}`)
	assert.True(t, either.Multi, "a file or a list of files repeats")
	assert.True(t, either.Filename, "and is files")
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
						"so a part reached only below it has no encoding entry", schema.MaxAllOfFlattenDepth), msg,
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
	assert.Equal(t, fmt.Sprintf("form body composition follows $refs past %d hops, "+
		"so a part reached only beyond them has no encoding entry", operation.MaxPartRefHops), msg,
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
	_, svc, diags := compileService(t, spec)
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

// unkeyedMessage is the message announcing the entry named name, spelled out
// here and not derived from the code under test.
func unkeyedMessage(name string) string {
	return fmt.Sprintf("encoding entry %q names no part the form body declares on the schema its $ref chain ends on "+
		"or on an allOf branch the part walk reaches, so Content.Encoding keys nothing for it; "+
		"kept verbatim under Unmodeled", name)
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
// at the entry and no error; the parts the walk does find stay keyed in
// Content.Encoding with no record of their own; and entries are announced in
// source order. The rows are the body shapes and the positions of a form content.
func TestPartEncodings_AnEntryNamingNoPartIsKeptWhole(t *testing.T) {
	t.Parallel()
	mfd := requestMedia(multipartForm)
	cases := []struct {
		name     string
		doc      string
		media    string
		response bool
		file     bool
		keyed    map[ir.PropID][]string
		kept     []unkeyedEntry
		warns    []string
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
			media: mfd, keyed: map[ir.PropID][]string{partProp(mfd, "file"): nil},
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
			media: mfd, keyed: map[ir.PropID][]string{},
			kept: []unkeyedEntry{{name: "ghost", value: pngEntry}},
		},
		{
			name:  "the name of a nested property",
			doc:   formDoc("", multipartForm, "{type: object, properties: {a: {type: object, properties: {b: {type: string}}}}}", "{b: "+pngSource+"}", ""),
			media: mfd, keyed: map[ir.PropID][]string{},
			kept: []unkeyedEntry{{name: "b", value: pngEntry}},
		},
		{
			name:  "the golden spec's map body",
			doc:   formDoc("", multipartForm, "{type: object, additionalProperties: {type: string, description: d}}", `{"*": {contentType: "*/*"}}`, ""),
			media: mfd, keyed: map[ir.PropID][]string{},
			kept: []unkeyedEntry{{name: "*", value: `{"contentType":"*/*"}`}},
		},
		{
			name:  "properties beside a catch-all",
			doc:   formDoc("", multipartForm, "{type: object, properties: {a: {type: string}}, additionalProperties: {type: string, format: binary}}", "{other: "+pngSource+"}", ""),
			media: mfd, keyed: map[ir.PropID][]string{},
			kept: []unkeyedEntry{{name: "other", value: pngEntry}},
		},
		{
			name:  "patternProperties alone",
			doc:   formDoc("", multipartForm, "{type: object, patternProperties: {'^f': {type: string}}}", "{foo: "+pngSource+"}", ""),
			media: mfd, keyed: map[ir.PropID][]string{},
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
			kept: []unkeyedEntry{{name: "file", value: pngEntry}},
		},
		{
			name:  "a oneOf body",
			doc:   formDoc("", multipartForm, unionSource, "{file: "+pngSource+"}", ""),
			media: mfd, keyed: map[ir.PropID][]string{},
			kept: []unkeyedEntry{{name: "file", value: pngEntry}},
		},
		{
			name:  "an anyOf body",
			doc:   formDoc("", multipartForm, "{anyOf: "+strings.TrimPrefix(unionSource, "{oneOf: "), "{file: "+pngSource+"}", ""),
			media: mfd, keyed: map[ir.PropID][]string{},
			kept: []unkeyedEntry{{name: "file", value: pngEntry}},
		},
		{
			name:  "a $ref to a oneOf",
			doc:   formDoc("", multipartForm, "{$ref: '#/components/schemas/U'}", "{file: "+pngSource+"}", "components:\n  schemas:\n    U: "+unionSource+"\n"),
			media: mfd, keyed: map[ir.PropID][]string{},
			kept: []unkeyedEntry{{name: "file", value: pngEntry}},
		},
		{
			name: "a oneOf with a $ref branch",
			doc: formDoc("", multipartForm, "{oneOf: [{$ref: '#/components/schemas/F'}, {type: object, properties: {url: {type: string}}}]}",
				"{file: "+pngSource+"}", "components:\n  schemas:\n    F: "+fileForm+"\n"),
			media: mfd, keyed: map[ir.PropID][]string{},
			kept: []unkeyedEntry{{name: "file", value: pngEntry}},
		},
		{
			name:  "a oneOf inside an allOf",
			doc:   formDoc("", multipartForm, "{allOf: ["+unionSource+"]}", "{file: "+pngSource+"}", ""),
			media: mfd, keyed: map[ir.PropID][]string{},
			kept: []unkeyedEntry{{name: "file", value: pngEntry}},
		},
		{
			name: "a oneOf beside properties",
			doc: formDoc("", multipartForm, "{type: object, properties: {meta: {type: string}}, oneOf: [{properties: {file: {type: string, format: binary}}}, {properties: {url: {type: string}}}]}",
				"{file: "+pngSource+", meta: {contentType: text/plain}}", ""),
			media: mfd, keyed: map[ir.PropID][]string{partProp(mfd, "meta"): {"text/plain"}},
			kept: []unkeyedEntry{{name: "file", value: pngEntry}},
		},
		{
			name: "a $ref beside properties",
			doc: formDoc("", multipartForm, "{$ref: '#/components/schemas/F', properties: {extra: {type: string, format: binary}}}",
				"{extra: "+pngSource+", file: {contentType: image/gif}}", "components:\n  schemas:\n    F: "+fileForm+"\n"),
			media: mfd, keyed: map[ir.PropID][]string{"p/openapi/components/schemas/F/properties/file": {"image/gif"}},
			kept: []unkeyedEntry{{name: "extra", value: pngEntry}},
		},
		{
			name:  "a url-encoded body",
			doc:   formDoc("", urlEncoded, "{type: object, properties: {a: {type: string}}}", "{ghost: {style: form}}", ""),
			media: requestMedia(urlEncoded), keyed: map[ir.PropID][]string{},
			kept: []unkeyedEntry{{name: "ghost", value: `{"style":"form"}`}},
		},
		{
			name:  "a media type written in mixed case with parameters",
			doc:   formDoc("", "Multipart/Form-Data; boundary=x", "{type: object, properties: {a: {type: string}}}", "{ghost: "+pngSource+"}", ""),
			media: requestMedia("Multipart/Form-Data; boundary=x"), keyed: map[ir.PropID][]string{},
			kept: []unkeyedEntry{{name: "ghost", value: pngEntry}},
		},
		{
			name:  "a 3.1 response, which load already warns about",
			doc:   responseFormDoc("3.1.0", "multipart/mixed", "{type: object, properties: {a: {type: string}}}", "{ghost: "+pngSource+"}"),
			media: "/paths/~1u/get/responses/200/content/multipart~1mixed", response: true, keyed: map[ir.PropID][]string{},
			kept:  []unkeyedEntry{{name: "ghost", value: pngEntry}},
			warns: []string{"openapi/validation/validation-no-effect"},
		},
		{
			name:  "a 3.2 response",
			doc:   responseFormDoc("3.2.0", "multipart/mixed", "{type: object, properties: {a: {type: string}}}", "{ghost: "+pngSource+"}"),
			media: "/paths/~1u/get/responses/200/content/multipart~1mixed", response: true, keyed: map[ir.PropID][]string{},
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
			media: "/components/requestBodies/Form/content/multipart~1form-data", keyed: map[ir.PropID][]string{},
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
			media: "/components/mediaTypes/Form", keyed: map[ir.PropID][]string{},
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
			media: mfd, keyed: map[ir.PropID][]string{},
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
				assert.Equal(t, unkeyedMessage(e.name), msg, "%s: the one announcement", e.name)
			}
			slices.Sort(wantNames)
			assert.Equal(t, wantNames, keptEncodingNames(content.Unmodeled),
				"the only records under openapi:encoding/ are the entries kept whole")
			var gotPointers []jsontext.Pointer
			for _, d := range diags {
				if strings.HasPrefix(string(d.Provenance.Pointer), tc.media+"/encoding/") {
					gotPointers = append(gotPointers, d.Provenance.Pointer)
				}
			}
			assert.Equal(t, wantPointers, gotPointers, "one finding per entry kept, in source order, and nothing else beneath the entries")
		})
	}
}

// TestPartEncodings_AnUnkeyedEntryLowersNothingItDeclares holds an entry kept
// whole to being one record and one finding: its header schema registers no
// type, and its unknown key, extension and nested encoding draw no finding and
// no record of their own, which the same entry under a part's name does draw.
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
	assert.Equal(t, []jsontext.Pointer{jsontext.Pointer(media + "/encoding/ghost")}, diagPointersUnder(diags, media+"/encoding/"),
		"one finding beneath the entry")
}

// TestPartEncodings_AFileBodyOutsideAFormKeepsNoEncodingEntry holds a binary
// body under a media type that is no form's to keeping no encoding entry
// whole, as the rule leaves an encoding under a media type it does not apply
// to in no form (docs/compilers/openapi.md#multipart-encoding).
func TestPartEncodings_AFileBodyOutsideAFormKeepsNoEncodingEntry(t *testing.T) {
	t.Parallel()
	_, svc, _ := compileService(t, formDoc("", "application/octet-stream", "{type: string, format: binary}", "{file: "+pngSource+"}", ""))
	content := formContent(t, svc, false)
	require.NotNil(t, content.File, "the body is a file body")
	assert.Empty(t, keptEncodingNames(content.Unmodeled), "no entry is kept whole")
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
