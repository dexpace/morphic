// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package operation

import (
	"encoding/json/jsontext"
	"slices"

	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/speakeasy-api/openapi/sequencedmap"

	"github.com/dexpace/morphic/compilers/openapi/internal/annotation"
	"github.com/dexpace/morphic/compilers/openapi/internal/componentlookup"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/compilers/openapi/internal/service"
	"github.com/dexpace/morphic/ir"
)

// lowerRequestBody lowers an operation's request body onto operation.Request
// and the binding's RequestContentTypes. Payload.Required is always set,
// because an undeclared `required` means false by the specification's own
// default, and nil would report the format as unable to express optionality.
//
// operationDeclPtr is the operation's own declaration pointer. A $ref'd body
// interns its content once at its declaration, not once per mount site, under
// the component's name rather than the operationId hint (issue #107). A body
// $ref'd from another operation has no component name, so it is named for the
// operation declaring it, never this mount's (lowering.NameAt, GitHub #433).
func lowerRequestBody(serviceContext service.Ctx, loweringState operationLoweringState, operation *ir.Operation, hb *ir.HTTPBinding, src *soa.Operation, operationDeclPtr jsontext.Pointer) []ir.Diagnostic {
	usePtr := operationDeclPtr + ids.Ptr("requestBody")
	rb, dc, bodyPtr := service.Declared[soa.RequestBody](serviceContext, src.GetRequestBody(), usePtr)
	if rb == nil {
		return nil
	}
	payload, diags := lowerPayload(dc, loweringState, rb.GetContent(), bodyPtr)
	diags = append(diags, bodyMethodDiags(serviceContext, hb.Method, usePtr)...)
	if payload == nil {
		// A body lowering no media type is still the body: whether it must be
		// sent, its description and its x-* stay on a Payload with no Contents.
		payload = &ir.Payload{}
		diags = append(diags, emptyContentDiags(dc, rb, bodyPtr)...)
	}
	// A Request Body Object's description documents the body rather than any
	// one media type in it (GitHub #609), and a summary or description beside a
	// $ref of its chain documents this use of it (GitHub #610).
	payload.Docs = componentlookup.UseDocs[soa.RequestBody](serviceContext.Bindings(), src.GetRequestBody(), ir.Docs{Description: rb.GetDescription()})
	payload.Required = new(rb.GetRequired())
	// soa.RequestBody exposes no GetExtensions at this library version, so the
	// field is read directly — as annotation.XMLHints already reads its own.
	bodyExt, bodyExtDiags := annotation.ExtensionsFrom(rb.Extensions, dc.Place(), bodyPtr)
	payload.Unmodeled = annotation.MergeUnmodeled(payload.Unmodeled, bodyExt)
	diags = append(diags, bodyExtDiags...)
	diags = append(diags, annotation.UnknownKeysIn(&payload.Unmodeled, rb, dc.Place(), bodyPtr)...)
	beside, besideDiags := service.Beside[soa.RequestBody](serviceContext, src.GetRequestBody(), usePtr, true)
	payload.Unmodeled = annotation.MergeUnmodeled(payload.Unmodeled, beside)
	operation.Request = payload
	diags = append(diags, besideDiags...)
	hb.RequestContentTypes = contentTypeKeys(rb.GetContent())
	return diags
}

// bodyMethodDiags reports a request body, written at at, on method, as sent on
// the wire, where HTTP defines a body no semantics, an info, or forbids one, a
// warning (docs/compilers/openapi.md#request-body). A method is compared as
// written, as HTTP compares one (RFC 9110 §9.1). The body lowers as declared.
func bodyMethodDiags(serviceContext service.Ctx, method string, at jsontext.Pointer) []ir.Diagnostic {
	switch method {
	case "GET", "HEAD", "DELETE":
		return []ir.Diagnostic{serviceContext.DiagAt(ir.SeverityInfo, diag.BodyWithoutSemantics, at,
			"%s request declares a body, which HTTP defines no semantics for (RFC 9110 §9.3); OpenAPI 3.0 has a "+
				"consumer ignore it and later versions advise against it; lowered as declared", method)}
	case "TRACE", "CONNECT":
		section := map[string]string{"TRACE": "§9.3.8", "CONNECT": "§9.3.6"}[method]
		return []ir.Diagnostic{serviceContext.DiagAt(ir.SeverityWarning, diag.BodyForbiddenByMethod, at,
			"%s request declares a body, which HTTP forbids (RFC 9110 %s); lowered as declared", method, section)}
	default:
		return nil
	}
}

// emptyContentDiags reports rb's content map written empty, at the map, or
// nothing; the design record is on diag.EmptyRequestContent. The condition is
// the parser's own for reading the map as missing, which the load phase drops.
func emptyContentDiags(serviceContext service.Ctx, rb *soa.RequestBody, bodyPtr jsontext.Pointer) []ir.Diagnostic {
	if !rb.GetCore().Content.Present || rb.GetContent().Len() != 0 {
		return nil
	}
	return []ir.Diagnostic{serviceContext.DiagAt(ir.SeverityWarning, diag.EmptyRequestContent, bodyPtr+ids.Ptr("content"),
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
