// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package operation

import (
	"encoding/json/jsontext"
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
// context and pointer declaring it: the entry itself, or what the chain of the
// $ref the entry is written as, use, ends on. entryCtx and entryPtr are where
// the entry itself is written.
type mediaDecl struct {
	ctx      lowering.Ctx
	media    *soa.MediaType
	pointer  jsontext.Pointer
	use      *resolve.ReferencedMediaType
	entryCtx lowering.Ctx
	entryPtr jsontext.Pointer
}

// declaredMedia returns what the content entry media, written at mediaPtr in
// c's document, stands for (mediaDecl), and false for one written as a $ref
// resolving to nothing, which lowers nothing, as any reference object does
// (docs/compilers/openapi.md#references). The load phase or the rule table
// reported why, but for a $ref inside an object reached in raw YAML
// (GitHub #1320) or one the model decodes no string from (GitHub #1368). Keys
// beside the $ref but summary and description are not read (GitHub #1321).
//
// One declaration lowers at its own pointer however many entries name it, so
// it interns one type (GitHub #856).
func declaredMedia(c lowering.Ctx, media *soa.MediaType, mediaPtr jsontext.Pointer) (mediaDecl, bool) {
	use, isUse := c.MediaTypeUse(media)
	if !isUse {
		return mediaDecl{ctx: c, media: media, pointer: mediaPtr, entryCtx: c, entryPtr: mediaPtr}, true
	}
	if use == nil {
		return mediaDecl{}, false
	}
	obj, dc, decl := lowering.Declared[soa.MediaType](c, use, mediaPtr)
	if obj == nil {
		return mediaDecl{}, false
	}
	return mediaDecl{ctx: dc, media: obj, pointer: decl, use: use, entryCtx: c, entryPtr: mediaPtr}, true
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
	switch file := bodyFile(c, mt, readFile(c, media.GetSchema())); {
	case file != nil:
		content.File = file
		content.Type = fileBodyType(ts, file, content.Type)
		kept, keptDiags := keepFileBodyEncodings(c, media, mediaPtr, mt)
		diags = append(diags, keptDiags...)
		content.Unmodeled = annotation.MergeUnmodeled(content.Unmodeled, kept)
	case isFormContent(mt):
		enc, encUnmodeled, encDiags := partEncodings(c, ts, media, mt, mediaPtr, content.Type.Target)
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

// keepFileBodyEncodings keeps whole the encoding entries of a file body written
// under a form media type, which declares no part for any of them to name
// (docs/compilers/openapi.md#multipart-encoding). Under any other media type it
// keeps nothing.
func keepFileBodyEncodings(c lowering.Ctx, media *soa.MediaType, mediaPtr jsontext.Pointer, mt string) (ir.Unmodeled, []ir.Diagnostic) {
	if !isFormContent(mt) {
		return nil, nil
	}
	return keepUnkeyedEncodings(c, media, mediaPtr, nil)
}

// fileBodyType is the type of the file body file whose schema lowered to
// declared: the declaration itself, so its docs, bounds and examples stay
// reachable, unless it states nothing and the file is declared, where the
// media type alone says the body is bytes. A file the binary policy read keeps
// the top type, so a consumer dropping the guess finds what the schema said.
// The bytes keep what the reference says of null.
func fileBodyType(ts *compile.Types, file *ir.FileInfo, declared ir.TypeRef) ir.TypeRef {
	if declared.Target != ir.PrimTypeID(ir.PrimAny) || file.Inferred != "" {
		return declared
	}
	declared.Target = ts.PrimID(ir.PrimBytes)
	return declared
}

// isFormContent reports whether a media type is multipart or url-encoded form
// content, whose parts carry per-property encoding.
func isFormContent(mt string) bool {
	return isMultipart(mt) || lowering.NormalizeMediaType(mt) == "application/x-www-form-urlencoded"
}

// isMultipart reports whether a media type is a multipart one, whose parts can
// carry raw bytes, as a url-encoded body's cannot.
func isMultipart(mt string) bool {
	return strings.HasPrefix(lowering.NormalizeMediaType(mt), "multipart/")
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
