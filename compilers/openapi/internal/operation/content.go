// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package operation

import (
	"encoding/json/jsontext"
	"slices"
	"strings"

	"github.com/speakeasy-api/openapi/jsonschema/oas3"
	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/speakeasy-api/openapi/sequencedmap"

	"github.com/dexpace/morphic/compilers/compile"
	"github.com/dexpace/morphic/compilers/openapi/internal/annotation"
	"github.com/dexpace/morphic/compilers/openapi/internal/bind/typed"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/resolve"
	"github.com/dexpace/morphic/compilers/openapi/internal/schema"
	"github.com/dexpace/morphic/ir"
)

// lowerPayload lowers a request/response body's content map into a Payload with
// one Content per media type — all kept, in source order, with no primary-
// content selection (docs/ir/payloads.md). The pointer is the payload owner
// (the response or requestBody); each Content's schema hoists under
// <pointer>/content/<mt>/schema, or under the components/mediaTypes entry an
// entry written as a $ref names (declaredMedia).
func lowerPayload(c lowering.Ctx, ts *compile.Types, content *sequencedmap.Map[string, *soa.MediaType], pointer jsontext.Pointer) (*ir.Payload, []ir.Diagnostic) {
	if content == nil || content.Len() == 0 {
		return nil, nil
	}
	var diags []ir.Diagnostic
	payload := &ir.Payload{}
	for mt, media := range content.All() {
		if media == nil {
			continue
		}
		decl, ok := declaredMedia(c, media, pointer+ids.Ptr("content", mt))
		if !ok {
			continue
		}
		one, contentDiags := lowerContent(decl, ts, mt)
		diags = append(diags, contentDiags...)
		payload.Contents = append(payload.Contents, one)
	}
	if len(payload.Contents) == 0 {
		return nil, diags
	}
	return payload, diags
}

// mediaDecl is the Media Type Object a content entry stands for, with the
// context and pointer it is declared at: the entry itself, or what the chain of
// the $ref it is written as ends on, use, read where the entry is written (at
// in c's document).
type mediaDecl struct {
	ctx     lowering.Ctx
	media   *soa.MediaType
	pointer jsontext.Pointer
	use     *resolve.ReferencedMediaType
	c       lowering.Ctx
	at      jsontext.Pointer
}

// declaredMedia returns what the content entry media, written at mediaPtr in
// c's document, stands for (mediaDecl), and false for one written as a $ref
// that resolves to nothing, which lowers nothing, as any reference object
// resolving to nothing does (docs/compilers/openapi.md#references): the load
// phase reported why, or the rule table did for a $ref holding no value.
//
// One declaration lowers at its own pointer however many entries name it, so
// it interns one type (GitHub #856).
func declaredMedia(c lowering.Ctx, media *soa.MediaType, mediaPtr jsontext.Pointer) (mediaDecl, bool) {
	use, isUse := c.MediaTypeUse(media)
	if !isUse {
		return mediaDecl{ctx: c, media: media, pointer: mediaPtr, c: c, at: mediaPtr}, true
	}
	if use == nil {
		return mediaDecl{}, false
	}
	obj, dc, decl := lowering.Declared[soa.MediaType](c, use, mediaPtr)
	if obj == nil {
		return mediaDecl{}, false
	}
	return mediaDecl{ctx: dc, media: obj, pointer: decl, use: use, c: c, at: mediaPtr}, true
}

// mediaTypeDocs returns the docs of the media type d declares: its own
// description, which OpenAPI 3.2 adds and the parser's model names no field
// for, with the summary and description beside each $ref of d's use folded
// over it (docs/compilers/openapi.md#reference-object-siblings), and the keys
// it read for the census to leave alone. It reads the description at every
// version, as load reports the key below 3.2 (diag.LaterVersionConstruct).
func mediaTypeDocs(d mediaDecl, u *ir.Unmodeled) (ir.Docs, []string, []ir.Diagnostic) {
	description, read, diags := annotation.TakeString(u, d.media.GetRootNode(), "description", nil,
		d.ctx.ProvenanceAt, d.pointer, "")
	docs := ir.Docs{Description: description}
	if d.use != nil {
		docs = resolve.UseDocs[soa.MediaType](d.use, docs)
	}
	if !read {
		return docs, nil, diags
	}
	return docs, []string{"description"}, diags
}

// lowerContent lowers one media-type view: its type graph, examples, binary/
// form specialization, sequential-media shape, docs and extensions, as d
// declares them, under mt, the key it is written under.
func lowerContent(d mediaDecl, ts *compile.Types, mt string) (ir.Content, []ir.Diagnostic) {
	c, media, mediaPtr := d.ctx, d.media, d.pointer
	mediaType, diags := schema.Ref(c, ts, schema.TopLevelDepth, media.GetSchema(), mediaPtr+ids.Ptr("schema"))
	content := ir.Content{
		MediaType: mt,
		Type:      mediaType,
	}
	ex, exDiags := exampleList(c, media.GetExample(), media.GetExamples(), mediaPtr)
	diags = append(diags, exDiags...)
	if len(ex) > 0 {
		content.Examples = ex
	}
	switch {
	case isBinaryBody(c.Bindings(), mt, media.GetSchema()):
		content.File = &ir.FileInfo{IsText: false, ContentTypes: []string{mt}}
		content.Type = fileBodyType(ts, content.Type)
	case isFormContent(mt):
		enc, encUnmodeled, encDiags := partEncodings(c, ts, media, mediaPtr, content.Type.Target)
		diags = append(diags, encDiags...)
		if enc != nil {
			content.Encoding = enc
		}
		content.Unmodeled = annotation.MergeUnmodeled(content.Unmodeled, encUnmodeled)
	}
	diags = append(diags, fillSequential(c, ts, &content, media, mediaPtr)...)
	ext, extDiags := annotation.ExtensionsFrom(media.GetExtensions(), c.ProvenanceAt, mediaPtr)
	diags = append(diags, extDiags...)
	if len(ext) > 0 {
		content.Unmodeled = annotation.MergeUnmodeled(content.Unmodeled, ext)
	}
	docs, taken, docDiags := mediaTypeDocs(d, &content.Unmodeled)
	content.Docs = docs
	diags = append(diags, docDiags...)
	return content, append(diags,
		annotation.UnknownKeysIn(&content.Unmodeled, media, c.ProvenanceAt, mediaPtr, taken...)...)
}

// fileBodyType is the type of a file body that lowered to declared: the
// declaration itself, so its docs, bounds and examples stay reachable, unless
// it states nothing, where the media type alone says the body is bytes.
func fileBodyType(ts *compile.Types, declared ir.TypeRef) ir.TypeRef {
	if declared.Target == ir.PrimTypeID(ir.PrimAny) {
		return ts.PrimRef(ir.PrimBytes)
	}
	return declared
}

// isFormContent reports whether a media type is multipart or url-encoded form
// content, whose parts carry per-property encoding.
func isFormContent(mt string) bool {
	mt = lowering.NormalizeMediaType(mt)
	return strings.HasPrefix(mt, "multipart/") || mt == "application/x-www-form-urlencoded"
}

// isBinaryBody reports whether a content entry is a binary file body: a
// string+binary schema, or an absent or boolean schema on
// application/octet-stream.
func isBinaryBody(refs *typed.Bindings, mt string, js *oas3.JSONSchema[oas3.Referenceable]) bool {
	s := schemaOf(refs, js)
	if s == nil {
		return lowering.NormalizeMediaType(mt) == "application/octet-stream"
	}
	return schemaIsBinary(s)
}

// schemaIsBinary reports whether a schema is a string+binary body.
func schemaIsBinary(s *oas3.Schema) bool {
	return s.GetFormat() == "binary" && slices.Contains(s.GetType(), oas3.SchemaTypeString)
}

// schemaOf returns the concrete Schema of a schema-or-ref-or-bool position,
// following a $ref to the schema its chain ends on, as refs resolved it, so
// binary/file detection and part enumeration see the referent's type and
// properties rather than the bare ref (which carries none). It returns nil for
// a boolean schema or an absent one.
func schemaOf(refs *typed.Bindings, js *oas3.JSONSchema[oas3.Referenceable]) *oas3.Schema {
	if js == nil || !js.IsSchema() {
		return nil
	}
	s := js.GetSchema()
	if s != nil && s.Ref != nil {
		if end := refs.End(js); end != nil {
			return end.GetSchema()
		}
	}
	return s
}
