// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package operation

import (
	"encoding/json/jsontext"
	"slices"

	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/speakeasy-api/openapi/sequencedmap"

	"github.com/dexpace/morphic/compilers/compile"
	"github.com/dexpace/morphic/compilers/openapi/internal/annotation"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/resolve"
	"github.com/dexpace/morphic/ir"
)

// lowerRequestBody lowers an operation's request body onto op.Request and the
// binding's RequestContentTypes. Payload.Required is always set, because an
// undeclared `required` means false by the specification's own default, and nil
// would report the format as unable to express optionality.
//
// opDeclPtr is the operation's own declaration pointer. A $ref'd body interns
// its content once at its declaration, not once per mount site, under the
// component's name rather than the operationId hint (issue #107). A body $ref'd
// from another operation has no component name, so it is named for the
// operation declaring it, never this mount's (lowering.NameAt, GitHub #433).
func lowerRequestBody(c lowering.Ctx, ts *compile.Types, op *ir.Operation, hb *ir.HTTPBinding, src *soa.Operation, opDeclPtr jsontext.Pointer) []ir.Diagnostic {
	usePtr := opDeclPtr + ids.Ptr("requestBody")
	rb, dc, bodyPtr := lowering.Declared[soa.RequestBody](c, src.GetRequestBody(), usePtr)
	if rb == nil {
		return nil
	}
	payload, diags := lowerPayload(dc, ts, rb.GetContent(), bodyPtr)
	if payload == nil {
		// A body lowering no media type is still the body: whether it must be
		// sent, its description and its x-* stay on a Payload with no Contents.
		payload = &ir.Payload{}
		diags = append(diags, emptyContentDiags(dc, rb, bodyPtr)...)
	}
	// A Request Body Object's description documents the body rather than any
	// one media type in it (GitHub #609), and a summary or description beside a
	// $ref of its chain documents this use of it (GitHub #610).
	payload.Docs = resolve.UseDocs[soa.RequestBody](src.GetRequestBody(), ir.Docs{Description: rb.GetDescription()})
	payload.Required = new(rb.GetRequired())
	// soa.RequestBody exposes no GetExtensions at this library version, so the
	// field is read directly — as annotation.XMLHints already reads its own.
	bodyExt, bodyExtDiags := annotation.ExtensionsFrom(rb.Extensions, dc.ProvenanceAt, bodyPtr)
	payload.Unmodeled = annotation.MergeUnmodeled(payload.Unmodeled, bodyExt)
	diags = append(diags, bodyExtDiags...)
	diags = append(diags, annotation.UnknownKeysIn(&payload.Unmodeled, rb, dc.ProvenanceAt, bodyPtr)...)
	op.Request = payload
	hb.RequestContentTypes = contentTypeKeys(rb.GetContent())
	return diags
}

// emptyContentDiags reports rb's content map written empty, at the map, or
// nothing; the design record is on diag.EmptyRequestContent. The condition is
// the parser's own for reading the map as missing, which the load phase drops.
func emptyContentDiags(c lowering.Ctx, rb *soa.RequestBody, bodyPtr jsontext.Pointer) []ir.Diagnostic {
	if !rb.GetCore().Content.Present || rb.GetContent().Len() != 0 {
		return nil
	}
	return []ir.Diagnostic{c.DiagAt(ir.SeverityWarning, diag.EmptyRequestContent, bodyPtr+ids.Ptr("content"),
		"request body's content map is empty, so it names no media type to send the body as")}
}

// contentTypeKeys returns a content map's media-type keys in source order —
// the request content priority order.
func contentTypeKeys(content *sequencedmap.Map[string, *soa.MediaType]) []string {
	if content == nil || content.Len() == 0 {
		return nil
	}
	return slices.Collect(content.Keys())
}
