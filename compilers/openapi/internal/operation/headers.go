package operation

import (
	"encoding/json/jsontext"
	"strings"

	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/speakeasy-api/openapi/sequencedmap"

	"github.com/dexpace/morphic/compilers/compile"
	"github.com/dexpace/morphic/compilers/openapi/internal/annotation"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/resolve"
	"github.com/dexpace/morphic/compilers/openapi/internal/schema"
	"github.com/dexpace/morphic/ir"
)

// lowerHeaders lowers a header map into Headers in source order. Each
// entry's own pointer stays its ID and Provenance (two keys $ref'ing the same
// header must not collide), but its schema — and the name hint that schema is
// hoisted under — follow the ref target's declaration instead (issue #107).
func lowerHeaders(c lowering.Ctx, ts *compile.Types, headers *sequencedmap.Map[string, *soa.ReferencedHeader], basePtr jsontext.Pointer) ([]ir.Header, []ir.Diagnostic) {
	if headers == nil || headers.Len() == 0 {
		return nil, nil
	}
	var diags []ir.Diagnostic
	out := make([]ir.Header, 0, headers.Len())
	for name, rh := range headers.All() {
		hptr := basePtr + ids.Ptr("headers", name)
		h, dc, hdecl := lowering.Declared[soa.Header](c, rh, hptr)
		if h == nil {
			continue
		}
		lowered, headerDiags := lowerHeader(c, dc, ts, h, name, hptr, hdecl)
		diags = append(diags, headerDiags...)
		diags = append(diags, reservedHeaderEntryDiag(c, name, hptr)...)
		// A summary or description beside a $ref of the entry's chain documents
		// this header rather than the declaration it names (GitHub #610).
		lowered.Docs = resolve.UseDocs[soa.Header](rh, lowered.Docs)
		out = append(out, lowered)
	}
	return out, diags
}

// reservedHeaderEntryDiag reports a headers-map entry OpenAPI says SHALL be
// ignored. Both maps this lowering serves, a response's (§4.8.17) and an
// encoding's (§4.8.15), reserve Content-Type and nothing else, because each has
// its own place to state a media type. The comparison is case-insensitive, as
// HTTP field names are.
//
// It reports at the entry's own pointer, not the declaration's: the reserved
// thing is the key the header is mapped under, so of two keys $ref'ing one
// component only the one spelled Content-Type is reserved.
//
// This is the headers-map half of the rule; reservedHeaderParamDiag is the
// parameter half. The header still lowers (see diag.ReservedHeaderName).
func reservedHeaderEntryDiag(c lowering.Ctx, name string, hptr jsontext.Pointer) []ir.Diagnostic {
	if !strings.EqualFold(name, "Content-Type") {
		return nil
	}
	return []ir.Diagnostic{c.DiagAt(ir.SeverityWarning, diag.ReservedHeaderName, hptr,
		"header %q is reserved: OpenAPI says a Content-Type entry in a headers map SHALL be "+
			"ignored, so it is lowered as declared and left for the emitter to suppress", name)}
}

// lowerHeader lowers one header entry into a Header. Its schema goes through
// schema.FillPropertyDetail like a model property's: a header schema declares
// docs, constraints, xml, examples and validation-only keywords the same way,
// and ir.Property has a field for each, so the header path had no reason to drop
// them (GitHub #116). Its style and explode are what Header.Style and Explode
// hold, as declared: undeclared, each stays unset rather than being written as
// the default. Written beside content, where they have no effect, they lower
// unreported, as a parameter's do (GitHub #887).
func lowerHeader(c, dc lowering.Ctx, ts *compile.Types, h *soa.Header, name string, hptr, hdecl jsontext.Pointer) (ir.Header, []ir.Diagnostic) {
	elected, diags := electTypeSpelling(dc, h.GetSchema(), h.GetContent(), h.GetRootNode(), hdecl,
		"header", "ir.Property")
	// The schema is named after the key the header is declared under, never
	// this mount's, and lowered in its declaration's document (dc), so every
	// response mounting one header builds the same nodes beneath it, in
	// whichever order they lower (GitHub #433).
	headerType, headerDiags := schema.CarriedRef(dc, ts, schema.TopLevelDepth, elected.js, elected.pointer)
	diags = append(diags, headerDiags...)
	p := ir.Property{
		ID:         c.IDs().Prop(hptr),
		Name:       compile.NamingFor(name),
		WireName:   name,
		Type:       headerType,
		Required:   h.GetRequired(),
		Provenance: c.ProvenanceAt(hptr),
		Unmodeled:  elected.unmodeled,
	}
	if elected.mediaType != "" {
		// The media type a content-style header serializes its value in, which is
		// what ir.Encoding.MediaType holds. Nothing else on this path writes
		// Property.Encoding, so the content spelling loses nothing the schema
		// spelling keeps.
		p.Encoding = &ir.Encoding{MediaType: elected.mediaType}
	}
	diags = append(diags, schema.FillPropertyDetail(dc, ts, &p, elected.js, elected.pointer)...)
	diags = append(diags, applyHeaderAnnotations(dc, &p, h, hdecl, elected.examples)...)
	lowered := ir.Header{Property: p, Explode: h.Explode}
	if h.Style != nil {
		lowered.Style = string(*h.Style)
	}
	return lowered, diags
}

// applyHeaderAnnotations overlays the annotations the header object writes on
// itself onto p, after its schema's. A header carries both, and the header's
// own are the more specific of the two — they describe this header rather than
// the type it happens to be. media is what its elected content entry wrote.
func applyHeaderAnnotations(c lowering.Ctx, p *ir.Property, h *soa.Header, hdecl jsontext.Pointer, media []ir.Example) []ir.Diagnostic {
	if d := h.GetDescription(); d != "" {
		p.Docs.Description = d
	}
	if h.GetDeprecated() {
		p.Deprecation = &ir.Deprecation{}
	}
	own, diags := exampleList(c, h.GetExample(), h.GetExamples(), hdecl)
	if ex := positionExamples(own, media); len(ex) > 0 {
		p.Examples = ex
	}
	hExt, extDiags := annotation.ExtensionsFrom(h.GetExtensions(), c.ProvenanceAt, hdecl)
	diags = append(diags, extDiags...)
	p.Unmodeled = annotation.MergeUnmodeled(p.Unmodeled, hExt)
	diags = append(diags, annotation.UnknownKeysIn(&p.Unmodeled, h, c.ProvenanceAt, hdecl)...)
	return append(diags, c.PromoteDeprecation(p.Unmodeled, p.Deprecation, &p.Provenance)...)
}
