// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package operation

import (
	"encoding/json/jsontext"

	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/speakeasy-api/openapi/sequencedmap"

	"github.com/dexpace/morphic/compilers/openapi/internal/annotation"
	"github.com/dexpace/morphic/compilers/openapi/internal/componentlookup"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/compilers/openapi/internal/promotion"
	"github.com/dexpace/morphic/compilers/openapi/internal/schema"
	"github.com/dexpace/morphic/compilers/openapi/internal/service"
	"github.com/dexpace/morphic/ir"
)

// lowerHeaders lowers a header map into Headers in source order. Each
// entry's own pointer stays its ID and Provenance (two keys $ref'ing the same
// header must not collide), but its schema — and the name hint that schema is
// hoisted under — follow the ref target's declaration instead (issue #107).
func lowerHeaders(serviceContext service.Ctx, loweringState operationLoweringState, headers *sequencedmap.Map[string, *soa.ReferencedHeader], basePtr jsontext.Pointer) ([]ir.Header, []ir.Diagnostic) {
	if headers == nil || headers.Len() == 0 {
		return nil, nil
	}
	var diags []ir.Diagnostic
	out := make([]ir.Header, 0, headers.Len())
	for name, rh := range headers.All() {
		hptr := basePtr + ids.Ptr("headers", name)
		h, dc, hdecl := service.Declared[soa.Header](serviceContext, rh, hptr)
		if h == nil {
			continue
		}
		lowered, headerDiags := lowerHeader(serviceContext, dc, loweringState, h, name, hptr, hdecl)
		diags = append(diags, headerDiags...)
		// A summary or description beside a $ref of the entry's chain documents
		// this header rather than the declaration it names (GitHub #610).
		lowered.Docs = componentlookup.UseDocs[soa.Header](serviceContext.Bindings(), rh, lowered.Docs)
		beside, besideDiags := service.Beside[soa.Header](serviceContext, rh, hptr, true)
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
func lowerHeader(serviceContext, dc service.Ctx, loweringState operationLoweringState, h *soa.Header, name string, hptr, hdecl jsontext.Pointer) (ir.Header, []ir.Diagnostic) {
	elected, diags := electTypeSpelling(dc, loweringState, h.GetSchema(), h.GetContent(), h.GetRootNode(), hdecl,
		"header", "ir.Property")
	// The schema is named after the key the header is declared under, never
	// this mount's, and lowered in its declaration's document (dc), so every
	// response mounting one header builds the same nodes beneath it, in
	// whichever order they lower (GitHub #433).
	headerType, headerDiags := schema.CarriedRef(elected.ctx.Ctx, loweringState.types, schema.TopLevelDepth, serviceContext.Statements().Of(elected.js), elected.pointer)
	diags = append(diags, headerDiags...)
	property := ir.Property{
		ID:         serviceContext.IDs().Property(hptr),
		Name:       ir.NamingFor(name),
		WireName:   name,
		Type:       headerType,
		Required:   h.GetRequired(),
		Provenance: serviceContext.Place().At(hptr),
	}
	if elected.mediaType != "" {
		// The media type a content-style header serializes its value in, which is
		// what ir.Encoding.MediaType holds. Nothing else on this path writes
		// Property.Encoding, so the content spelling loses nothing the schema
		// spelling keeps.
		property.Encoding = &ir.Encoding{MediaType: elected.mediaType}
	}
	// The property holds what the schema writes beside what the header object
	// writes, so the schema's entries are kept beneath its scope: the two can
	// spell one key.
	diags = append(diags, schema.FillPropertyDetail(elected.ctx.Ctx, loweringState.types, &property, serviceContext.Statements().Of(elected.js), elected.pointer)...)
	promotion.DropContained(dc.Promoter(), &property.Unmodeled)
	property.Unmodeled = annotation.MergeUnmodeled(annotation.Under(elected.schemaScope(), property.Unmodeled), elected.unmodeled)
	diags = append(diags, applyHeaderAnnotations(dc, &property, h, hdecl, elected.examples)...)
	fallbackReferent := dc.Promoter().WithOwnCarriedState(schema.FallbackKeysForCarrier(dc.Ctx, serviceContext.Statements().Of(elected.js), property.Type, elected.pointer), property.Unmodeled,
		elected.carries...)
	own := promotion.Carried(dc.Promoter(), property.Unmodeled, elected.carries...)
	diags = append(diags, schema.PromoteUse(dc.Ctx, fallbackReferent, own, &property.Deprecation, &property.Secret, &property.Provenance)...)
	diags = append(diags, promoteShadowed(dc, property.Unmodeled, elected.carries...)...)
	lowered := ir.Header{Property: property, Explode: h.Explode}
	if h.Style != nil {
		lowered.Style = string(*h.Style)
	}
	return lowered, diags
}

// applyHeaderAnnotations overlays the annotations the header object writes on
// itself onto property, after its schema's. The header's own are the more
// specific of the two: they describe this header rather than the type it
// happens to be. Where the schema hoists a node, that node keeps the notes the
// header writes over (schema.CarriedAnnotations), so neither is lost; where it
// hoists none, the header's replace its schema's description and examples.
// media is what its elected content entry wrote.
func applyHeaderAnnotations(serviceContext service.Ctx, property *ir.Property, h *soa.Header, hdecl jsontext.Pointer, media []ir.Example) []ir.Diagnostic {
	if d := h.GetDescription(); d != "" {
		property.Docs.Description = d
	}
	if h.GetDeprecated() {
		property.Deprecation = &ir.Deprecation{}
	}
	own, diags := exampleList(serviceContext, h.GetExample(), h.GetExamples(), hdecl)
	if ex := positionExamples(own, media); len(ex) > 0 {
		property.Examples = ex
	}
	hExt, extDiags := annotation.ExtensionsFrom(h.GetExtensions(), serviceContext.Place(), hdecl)
	diags = append(diags, extDiags...)
	property.Unmodeled = annotation.MergeUnmodeled(property.Unmodeled, hExt)
	return append(diags, annotation.UnknownKeysIn(&property.Unmodeled, h, serviceContext.Place(), hdecl)...)
}
