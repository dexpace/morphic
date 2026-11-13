// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package convention

import (
	"strings"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
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
func httpDate(c lowering.Ctx, reg ir.TypeRegistry, h *ir.Header) []ir.Diagnostic {
	if !c.HTTPDateInference.Honours() || h.Encoding != nil || h.Prefix != nil ||
		!httpDateField(h.EffectiveWireName()) {
		return nil
	}
	ch, ok := readChain(c.Promoter(), reg, h.Type.Target)
	if !ok || ch.encoded || ch.prim != ir.PrimDatetimeOffset {
		return nil
	}
	h.Encoding = &ir.Encoding{Name: httpDateEncoding}
	at := h.Provenance
	at.Inferred = ""
	h.Provenance.Inferred = ir.JoinInferred(h.Provenance.Inferred, lowering.HTTPDateInferenceHeuristic)
	return []ir.Diagnostic{diag.Newf(ir.SeverityInfo, diag.InferredHTTPDate, at,
		"header %q is a field its RFC defines as an HTTP-date, so its date-time is written and read as one (%s) "+
			"rather than in RFC 3339's text, which its format names", h.EffectiveWireName(), httpDateEncoding)}
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
