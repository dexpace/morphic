// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package operation

import (
	"encoding/json/jsontext"

	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/speakeasy-api/openapi/sequencedmap"

	"github.com/dexpace/morphic/compilers/openapi/internal/annotation"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/compilers/openapi/internal/promotion"
	"github.com/dexpace/morphic/compilers/openapi/internal/resolve"
	"github.com/dexpace/morphic/compilers/openapi/internal/schema"
	"github.com/dexpace/morphic/compilers/openapi/internal/service"
	"github.com/dexpace/morphic/ir"
)

// lowerHeaders lowers a header map into Headers in source order. Each
// entry's own pointer stays its ID and Provenance (two keys $ref'ing the same
// header must not collide), but its schema — and the name hint that schema is
// hoisted under — follow the ref target's declaration instead (issue #107).
func lowerHeaders(c service.Ctx, w walk, headers *sequencedmap.Map[string, *soa.ReferencedHeader], basePtr jsontext.Pointer) ([]ir.Header, []ir.Diagnostic) {
	if headers == nil || headers.Len() == 0 {
		return nil, nil
	}
	var diags []ir.Diagnostic
	out := make([]ir.Header, 0, headers.Len())
	for name, rh := range headers.All() {
		hptr := basePtr + ids.Ptr("headers", name)
		h, dc, hdecl := service.Declared[soa.Header](c, rh, hptr)
		if h == nil {
			continue
		}
		lowered, headerDiags := lowerHeader(c, dc, w, h, name, hptr, hdecl)
		diags = append(diags, headerDiags...)
		// A summary or description beside a $ref of the entry's chain documents
		// this header rather than the declaration it names (GitHub #610).
		lowered.Docs = resolve.UseDocs[soa.Header](c.Bindings(), rh, lowered.Docs)
		beside, besideDiags := service.Beside[soa.Header](c, rh, hptr, true)
		lowered.Unmodeled = annotation.MergeUnmodeled(lowered.Unmodeled, beside)
		diags = append(diags, besideDiags...)
		out = append(out, lowered)
	}
	return out, diags
}

// lowerHeader lowers one header entry into a Header. Its schema goes through
// schema.FillPropertyDetail like a model property's: a header schema declares
// docs, constraints, xml, examples and validation-only keywords the same way,
// and ir.Property has a field for each, so the header path had no reason to drop
// them (GitHub #116). Its style and explode are what Header.Style and Explode
// hold, as declared: undeclared, each stays unset rather than being written as
// the default. Written beside content, where they have no effect, they lower
// as declared, and load's rule table reports them, as a parameter's.
func lowerHeader(c, dc service.Ctx, w walk, h *soa.Header, name string, hptr, hdecl jsontext.Pointer) (ir.Header, []ir.Diagnostic) {
	elected, diags := electTypeSpelling(dc, w, h.GetSchema(), h.GetContent(), h.GetRootNode(), hdecl,
		"header", "ir.Property")
	// The schema is named after the key the header is declared under, never
	// this mount's, and lowered in its declaration's document (dc), so every
	// response mounting one header builds the same nodes beneath it, in
	// whichever order they lower (GitHub #433).
	headerType, headerDiags := schema.CarriedRef(elected.ctx.Ctx, w.types, schema.TopLevelDepth, elected.js, elected.pointer)
	diags = append(diags, headerDiags...)
	p := ir.Property{
		ID:         c.IDs().Prop(hptr),
		Name:       ir.NamingFor(name),
		WireName:   name,
		Type:       headerType,
		Required:   h.GetRequired(),
		Provenance: c.Place().At(hptr),
	}
	if elected.mediaType != "" {
		// The media type a content-style header serializes its value in, which is
		// what ir.Encoding.MediaType holds. Nothing else on this path writes
		// Property.Encoding, so the content spelling loses nothing the schema
		// spelling keeps.
		p.Encoding = &ir.Encoding{MediaType: elected.mediaType}
	}
	// The property holds what the schema writes beside what the header object
	// writes, so the schema's entries are kept beneath its scope: the two can
	// spell one key.
	diags = append(diags, schema.FillPropertyDetail(elected.ctx.Ctx, w.types, &p, elected.js, elected.pointer)...)
	promotion.DropContained(dc.Promoter(), &p.Unmodeled)
	p.Unmodeled = annotation.MergeUnmodeled(annotation.Under(elected.schemaScope(), p.Unmodeled), elected.unmodeled)
	diags = append(diags, applyHeaderAnnotations(dc, &p, h, hdecl, elected.examples)...)
	lent := dc.Promoter().Lent(schema.KeysLentTo(dc.Ctx, w.types, elected.js, p.Type, elected.pointer), p.Unmodeled,
		elected.carries...)
	own := promotion.Carried(dc.Promoter(), p.Unmodeled, elected.carries...)
	diags = append(diags, schema.PromoteUse(dc.Ctx, lent, own, &p.Deprecation, &p.Secret, &p.Provenance)...)
	diags = append(diags, promoteShadowed(dc, p.Unmodeled, elected.carries...)...)
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
func applyHeaderAnnotations(c service.Ctx, p *ir.Property, h *soa.Header, hdecl jsontext.Pointer, media []ir.Example) []ir.Diagnostic {
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
	hExt, extDiags := annotation.ExtensionsFrom(h.GetExtensions(), c.Place(), hdecl)
	diags = append(diags, extDiags...)
	p.Unmodeled = annotation.MergeUnmodeled(p.Unmodeled, hExt)
	return append(diags, annotation.UnknownKeysIn(&p.Unmodeled, h, c.Place(), hdecl)...)
}
