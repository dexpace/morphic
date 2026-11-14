// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package operation

import (
	"encoding/json/jsontext"
	"strings"

	soa "github.com/speakeasy-api/openapi/openapi"

	"github.com/dexpace/morphic/compilers/openapi/internal/annotation"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
	"github.com/dexpace/morphic/compilers/openapi/internal/oas"
	"github.com/dexpace/morphic/ir"
)

// encodingConfig lowers one Encoding object's declared wire config under mt, a
// normalized media type: content types, per-part headers, and the arrangement
// a query parameter's value takes (arrangement). A field the table says has no
// effect under mt is kept verbatim instead (keepIgnoredFields). The structural
// flags (Multi, Filename) come from the part, not from here.
//
// It returns what the object declared that PartEncoding has no field for as a
// second value rather than writing it, because PartEncoding carries no
// Unmodeled map of its own; scope is where that belongs on the owning Content
// (see encodingUnmodeled).
func encodingConfig(c lowering.Ctx, w walk, enc *soa.Encoding, encPtr jsontext.Pointer, scope, mt string) (ir.PartEncoding, ir.Unmodeled, []ir.Diagnostic) {
	pe := ir.PartEncoding{}
	if enc == nil {
		return pe, nil, nil
	}
	rules := lowering.EncodingRules(c)
	unmodeled, diags := keepIgnoredFields(c, enc, encPtr, scope, mt, rules)
	pe.ContentTypes = splitContentTypes(enc.GetContentTypeValue())
	if !rules.Ignores("headers", mt) {
		headers, headerDiags := lowerHeaders(c, w, enc.GetHeaders(), encPtr)
		pe.Headers = headers
		diags = append(diags, headerDiags...)
	}
	if arranges(rules, enc, mt) {
		pe.Style, pe.Explode, pe.AllowReserved = arrangement(enc)
	}
	encUnmodeled, encDiags := encodingUnmodeled(c, enc, encPtr, scope)
	return pe, annotation.MergeUnmodeled(unmodeled, encUnmodeled), append(diags, encDiags...)
}

// arrangingFields are the Encoding Object's RFC6570-style fields: where one is
// written and takes effect, the part's value is arranged as a query
// parameter's is, and its contentType, implicit or explicit, is not read
// (docs/compilers/openapi.md#multipart-encoding).
var arrangingFields = []string{"style", "explode", "allowReserved"}

// arranges reports whether enc writes an RFC6570-style field the table does
// not say has no effect under mt, which arranges the part's value.
func arranges(rules oas.EncodingRules, enc *soa.Encoding, mt string) bool {
	return enc.Style != nil && !rules.Ignores("style", mt) ||
		enc.Explode != nil && !rules.Ignores("explode", mt) ||
		enc.AllowReserved != nil && !rules.Ignores("allowReserved", mt)
}

// arrangement returns the arrangement enc writes: its style, or a query
// parameter's default, which every minor gives one writing only explode or
// allowReserved; explode as written, nil reading as the style's default; and
// allowReserved.
func arrangement(enc *soa.Encoding) (style string, explode *bool, allowReserved bool) {
	style = defaultParamStyle(soa.ParameterInQuery)
	if enc.Style != nil {
		style = string(*enc.Style)
	}
	return style, enc.Explode, enc.GetAllowReserved()
}

// keepIgnoredFields keeps verbatim, under the scope the part's own entries ride
// on, each field of enc the table says has no effect under mt: an
// RFC6570-style field outside the media types it applies to, and headers
// outside multipart. Each is reported where it is written as the check reports
// a field written where the specification gives it no effect.
func keepIgnoredFields(c lowering.Ctx, enc *soa.Encoding, encPtr jsontext.Pointer, scope, mt string, rules oas.EncodingRules) (ir.Unmodeled, []ir.Diagnostic) {
	var kept ir.Unmodeled
	var diags []ir.Diagnostic
	for _, field := range append([]string{"headers"}, arrangingFields...) {
		if !rules.Ignores(field, mt) {
			continue
		}
		at := encPtr + ids.Ptr(field)
		written, keepDiags := annotation.PreserveNodeInto(&kept, annotation.EntryKey(scope, field),
			nodeview.Child(enc.GetRootNode(), field), ir.ReasonNoIRHome, c.Place().At(at))
		diags = append(diags, keepDiags...)
		if written {
			diags = append(diags, c.DiagAt(ir.SeverityWarning, oas.NoEffect, at,
				"Encoding Object field %q has no effect under %q, where the specification ignores it; kept verbatim under Unmodeled",
				field, mt))
		}
	}
	return kept, diags
}

// encodingUnmodeled keeps what an Encoding Object declares that nothing in the
// IR holds: its nested encodings, the object's own x-*, and the keys the
// specification defines for no encoding at all. Otherwise documents differing
// only in those compile to one IR (GitHub #291).
//
// Entries ride on the owning ir.Content, since PartEncoding carries no
// Unmodeled map, keyed under scope: "encoding/<part>" or "itemEncoding". One
// content can hold several such entries in the same map, so the part tells them
// apart.
func encodingUnmodeled(c lowering.Ctx, enc *soa.Encoding, encPtr jsontext.Pointer, scope string) (ir.Unmodeled, []ir.Diagnostic) {
	var out ir.Unmodeled
	diags := nestedEncodings(c, &out, enc, encPtr, scope)
	ext, extDiags := annotation.ExtensionsUnder(enc.GetExtensions(), c.Place(), encPtr, scope)
	out = annotation.MergeUnmodeled(out, ext)
	diags = append(diags, extDiags...)
	return out, append(diags, annotation.UnknownKeysUnder(&out, enc, c.Place(), encPtr, scope)...)
}

// nestedEncodingFields are the OpenAPI 3.2 Encoding Object fields that carry a
// nested Encoding Object: the object's own encoding map, the positional prefix
// encodings, and the item encoding governing what follows them. This compiler
// lowers an Encoding Object to ir.PartEncoding, which has no encoding fields of
// its own, so each reaches the IR in no modelled form.
var nestedEncodingFields = []string{"encoding", "prefixEncoding", "itemEncoding"}

// nestedEncodings keeps the nested Encoding Objects OpenAPI 3.2 lets an
// encoding write, verbatim under the scope the part's own entries ride on, one
// info each (GitHub #615). Recording them at the census's own key and pointer
// keeps the census from calling them undefined, at every version, as load
// reports them below 3.2 (diag.LaterVersionConstruct).
//
// ReasonNoIRHome, since PartEncoding could grow the fields. Nothing is lowered:
// a part inside a part has no shape here, and keeping the source is lossless.
func nestedEncodings(c lowering.Ctx, out *ir.Unmodeled, enc *soa.Encoding, encPtr jsontext.Pointer, scope string) []ir.Diagnostic {
	var diags []ir.Diagnostic
	for _, keyword := range nestedEncodingFields {
		at := encPtr + ids.Ptr(keyword)
		kept, keptDiags := annotation.PreserveNodeInto(out, annotation.EntryKey(scope, keyword),
			nodeview.Child(enc.GetRootNode(), keyword), ir.ReasonNoIRHome, c.Place().At(at))
		diags = append(diags, keptDiags...)
		if !kept {
			continue
		}
		diags = append(diags, c.DiagAt(ir.SeverityInfo, diag.DegradedConstruct, at,
			"encoding %s has no ir.PartEncoding home; kept verbatim under Unmodeled", keyword))
	}
	return diags
}

// splitContentTypes splits an encoding contentType value on "," into trimmed,
// non-empty media types.
func splitContentTypes(raw string) []string {
	if raw == "" {
		return nil
	}
	var out []string
	for part := range strings.SplitSeq(raw, ",") {
		if s := strings.TrimSpace(part); s != "" {
			out = append(out, s)
		}
	}
	return out
}
