package operation

import (
	"encoding/json/jsontext"
	"slices"
	"strings"

	oas3 "github.com/speakeasy-api/openapi/jsonschema/oas3"
	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/speakeasy-api/openapi/sequencedmap"
	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/compile"
	"github.com/dexpace/morphic/compilers/openapi/internal/annotation"
	"github.com/dexpace/morphic/compilers/openapi/internal/bind/typed"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
	"github.com/dexpace/morphic/compilers/openapi/internal/schema"
	"github.com/dexpace/morphic/ir"
)

// lowerPayload lowers a request/response body's content map into a Payload with
// one Content per media type — all kept, in source order, with no primary-
// content selection (ir-design §7.2). The pointer is the payload owner (the
// response or requestBody); each Content's schema hoists under
// <pointer>/content/<mt>/schema.
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
		one, contentDiags := lowerContent(c, ts, mt, media, pointer)
		diags = append(diags, contentDiags...)
		payload.Contents = append(payload.Contents, one)
	}
	if len(payload.Contents) == 0 {
		return nil, diags
	}
	return payload, diags
}

// keepMediaTypeFields keeps what OpenAPI 3.2 lets a Media Type Object write
// and the parser's model names no field for, at the census's own entries, so
// the census does not call it undefined: a `$ref` (keepMediaTypeRef), or else a
// description (keepMediaTypeDescription). Both are read at every version, as
// load reports them below 3.2 (diag.LaterVersionConstruct).
func keepMediaTypeFields(c lowering.Ctx, u *ir.Unmodeled, media *soa.MediaType, mediaPtr jsontext.Pointer, scope string) []ir.Diagnostic {
	root := media.GetRootNode()
	if ref := nodeview.Child(root, "$ref"); ref != nil {
		return keepMediaTypeRef(c, u, root, ref, mediaPtr, scope)
	}
	return keepMediaTypeDescription(c, u, root, mediaPtr, scope)
}

// keepMediaTypeDescription keeps a Media Type Object's description verbatim,
// with an info. ir.Content has no docs field, hence ReasonNoIRHome.
func keepMediaTypeDescription(c lowering.Ctx, u *ir.Unmodeled, root *yaml.Node, mediaPtr jsontext.Pointer, scope string) []ir.Diagnostic {
	at := mediaPtr + ids.Ptr("description")
	kept, diags := annotation.PreserveNodeInto(u, annotation.EntryKey(scope, "description"),
		nodeview.Child(root, "description"), ir.ReasonNoIRHome, c.ProvenanceAt(at))
	if !kept {
		return diags
	}
	return append(diags, c.DiagAt(ir.SeverityInfo, diag.DegradedConstruct, at,
		"a media type's description has no IR home; kept verbatim under Unmodeled"))
}

// keepMediaTypeRef keeps a content entry written as a Reference Object, which
// this compiler does not follow yet (GitHub #856): ref, the `$ref` written on
// root, verbatim at the census's own entry, with a warning that the entry
// lowers as written, and the summary and description a Reference Object may
// write beside it verbatim too.
func keepMediaTypeRef(c lowering.Ctx, u *ir.Unmodeled, root, ref *yaml.Node, mediaPtr jsontext.Pointer, scope string) []ir.Diagnostic {
	var diags []ir.Diagnostic
	for _, sibling := range []string{"summary", "description"} {
		_, siblingDiags := annotation.PreserveNodeInto(u, annotation.EntryKey(scope, sibling),
			nodeview.Child(root, sibling), ir.ReasonNoIRHome, c.ProvenanceAt(mediaPtr+ids.Ptr(sibling)))
		diags = append(diags, siblingDiags...)
	}
	at := mediaPtr + ids.Ptr("$ref")
	kept, refDiags := annotation.PreserveNodeInto(u, annotation.EntryKey(scope, "$ref"), ref,
		ir.ReasonDegradedLowering, c.ProvenanceAt(at))
	diags = append(diags, refDiags...)
	if !kept {
		return diags
	}
	return append(diags, c.DiagAt(ir.SeverityWarning, diag.DegradedConstruct, at,
		"a media type written as a $ref is not followed by this compiler yet (GitHub #856); the entry "+
			"lowers as written, and the reference is kept verbatim under Unmodeled"))
}

// lowerContent lowers one media-type view: its type graph, examples, binary/
// form specialization, sequential-media shape, and extensions.
func lowerContent(c lowering.Ctx, ts *compile.Types, mt string, media *soa.MediaType, pointer jsontext.Pointer) (ir.Content, []ir.Diagnostic) {
	mediaPtr := pointer + ids.Ptr("content", mt)
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
		content.Type = ts.PrimRef(ir.PrimBytes)
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
	diags = append(diags, keepMediaTypeFields(c, &content.Unmodeled, media, mediaPtr, "")...)
	return content, append(diags,
		annotation.UnknownKeysIn(&content.Unmodeled, media, c.ProvenanceAt, mediaPtr)...)
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
