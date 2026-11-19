// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package convention

import (
	"strings"

	"github.com/dexpace/morphic/compilers/openapi/internal/compileddoc"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/promotion"
	"github.com/dexpace/morphic/ir"
)

// httpDateEncoding is the encoding the IR names an HTTP-date by
// (docs/ir/constraints.md#encodings).
const httpDateEncoding = "http-date"

// httpDate gives h, a response header, the text its field's definition gives a
// date, an HTTP-date, and marks it, under the HTTP-date policy, where its field
// is one httpDateField names, it writes no encoding of its own and names one
// field, and its type reads, through its scalars, as a datetime_offset none of
// them names an encoding for. It reports each such header, whose format says
// RFC 3339's text. The rule is docs/compilers/openapi.md#http-date-inference.
func httpDate(policy HTTPDateInference, p promotion.Promoter, reg ir.TypeRegistry, h *ir.Header) []ir.Diagnostic {
	if !policy.Honours() || h.Encoding != nil || h.Prefix != nil ||
		!httpDateField(h.EffectiveWireName()) {
		return nil
	}
	if !instant(p, reg, h.Type.Target) {
		return nil
	}
	h.Encoding = &ir.Encoding{Name: httpDateEncoding}
	at := h.Provenance
	at.Inferred = ""
	h.Provenance.Inferred = ir.JoinInferred(h.Provenance.Inferred, HTTPDateInferenceHeuristic)
	return []ir.Diagnostic{diag.Newf(ir.SeverityInfo, diag.InferredHTTPDate, at,
		"header %q is a field its RFC defines as an HTTP-date, so its date-time is written and read as one (%s) "+
			"rather than in RFC 3339's text, which its format names", h.EffectiveWireName(), httpDateEncoding)}
}

// httpDateParam gives each header parameter of op the text httpDate gives a
// response header, under the HTTP-date policy, where the name its binding
// places it under is one httpDateField names, it writes no encoding of its own,
// and its type reads as an instant: on the parameter itself, as its type may be
// one a $ref shares. A parameter in any other location is not read, whatever
// its name. The rule is docs/compilers/openapi.md#http-date-inference.
func httpDateParam(policy HTTPDateInference, promoter promotion.Promoter, w *compileddoc.CompiledDocumentView,
	op *ir.Operation,
) []ir.Diagnostic {
	if !policy.Honours() || len(op.Params) == 0 {
		return nil
	}
	ix := w.Params(op)
	var diags []ir.Diagnostic
	for i := range op.Params {
		p := &op.Params[i]
		name, bound := headerName(ix, p)
		if !bound || p.Encoding != nil || !httpDateField(name) || !instant(promoter, w.Types, p.Type.Target) {
			continue
		}
		p.Encoding = &ir.Encoding{Name: httpDateEncoding}
		at := p.Provenance
		at.Inferred = ""
		p.Provenance.Inferred = ir.JoinInferred(p.Provenance.Inferred, HTTPDateInferenceHeuristic)
		diags = append(diags, diag.Newf(ir.SeverityInfo, diag.InferredHTTPDate, at,
			"header parameter %q is a field its RFC defines as an HTTP-date, so its date-time is written as one (%s) "+
				"rather than in RFC 3339's text, which its format names", name, httpDateEncoding))
	}
	return diags
}

// headerName returns the name the first of the placements ix holds for p puts
// it in a header under, and false where none does.
func headerName(ix compileddoc.ParamIndex, p *ir.Parameter) (string, bool) {
	for _, at := range ix.Placements(p.ID) {
		if at.Location == ir.HTTPLocationHeader {
			return at.Name, true
		}
	}
	return "", false
}

// instant reports whether id reads, through its scalars, as a datetime_offset
// none of them names an encoding for.
func instant(p promotion.Promoter, reg ir.TypeRegistry, id ir.TypeID) bool {
	ch, ok := readChain(p, reg, id)
	return ok && !ch.encoded && ch.prim == ir.PrimitiveDatetimeOffset
}

// httpDateField reports whether name, compared without case as HTTP compares a
// field's, is a field whose value an RFC defines as an HTTP-date, or as one in
// its date form: Date, Last-Modified, If-Modified-Since, If-Unmodified-Since,
// Retry-After and If-Range (RFC 9110 §6.6.1, §8.8.2, §13.1.3, §13.1.4,
// §10.2.3, §13.1.5), Expires (RFC 9111 §5.3) and Sunset (RFC 8594 §3).
func httpDateField(name string) bool {
	switch strings.ToLower(name) {
	case "date", "last-modified", "if-modified-since", "if-unmodified-since", "retry-after", "if-range",
		"expires", "sunset":
		return true
	default:
		return false
	}
}
