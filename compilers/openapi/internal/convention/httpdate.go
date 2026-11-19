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

// httpDate gives header, a response header, the text its field's definition
// gives a date, an HTTP-date, and marks it, under the HTTP-date policy, where
// its field is one httpDateField names, it writes no encoding of its own and
// names one field, and its type reads, through its scalars, as a
// datetime_offset none of them names an encoding for. It reports each such
// header, whose format says RFC 3339's text. The rule is
// docs/compilers/openapi.md#http-date-inference.
func httpDate(policy HTTPDateInference, promoter promotion.Promoter, reg ir.TypeRegistry, header *ir.Header) []ir.Diagnostic {
	if !policy.Honours() || header.Encoding != nil || header.Prefix != nil ||
		!httpDateField(header.EffectiveWireName()) {
		return nil
	}
	if !instant(promoter, reg, header.Type.Target) {
		return nil
	}
	header.Encoding = &ir.Encoding{Name: httpDateEncoding}
	at := header.Provenance
	at.Inferred = ""
	header.Provenance.Inferred = ir.JoinInferred(header.Provenance.Inferred, HTTPDateInferenceHeuristic)
	return []ir.Diagnostic{diag.Newf(ir.SeverityInfo, diag.InferredHTTPDate, at,
		"header %q is a field its RFC defines as an HTTP-date, so its date-time is written and read as one (%s) "+
			"rather than in RFC 3339's text, which its format names", header.EffectiveWireName(), httpDateEncoding)}
}

// httpDateParam gives each header parameter of operation the text httpDate gives a
// response header, under the HTTP-date policy, where the name its binding
// places it under is one httpDateField names, it writes no encoding of its own,
// and its type reads as an instant: on the parameter itself, as its type may be
// one a $ref shares. A parameter in any other location is not read, whatever
// its name. The rule is docs/compilers/openapi.md#http-date-inference.
func httpDateParam(policy HTTPDateInference, promoter promotion.Promoter, compiledDocumentView *compileddoc.CompiledDocumentView,
	operation *ir.Operation,
) []ir.Diagnostic {
	if !policy.Honours() || len(operation.Params) == 0 {
		return nil
	}
	ix := compiledDocumentView.Params(operation)
	var diags []ir.Diagnostic
	for i := range operation.Params {
		parameter := &operation.Params[i]
		name, bound := headerName(ix, parameter)
		if !bound || parameter.Encoding != nil || !httpDateField(name) || !instant(promoter, compiledDocumentView.Types, parameter.Type.Target) {
			continue
		}
		parameter.Encoding = &ir.Encoding{Name: httpDateEncoding}
		at := parameter.Provenance
		at.Inferred = ""
		parameter.Provenance.Inferred = ir.JoinInferred(parameter.Provenance.Inferred, HTTPDateInferenceHeuristic)
		diags = append(diags, diag.Newf(ir.SeverityInfo, diag.InferredHTTPDate, at,
			"header parameter %q is a field its RFC defines as an HTTP-date, so its date-time is written as one (%s) "+
				"rather than in RFC 3339's text, which its format names", name, httpDateEncoding))
	}
	return diags
}

// headerName returns the name the first of the placements ix holds for
// parameter puts it in a header under, and false where none does.
func headerName(ix compileddoc.ParamIndex, parameter *ir.Parameter) (string, bool) {
	for _, at := range ix.Placements(parameter.ID) {
		if at.Location == ir.HTTPLocationHeader {
			return at.Name, true
		}
	}
	return "", false
}

// instant reports whether id reads, through its scalars, as a datetime_offset
// none of them names an encoding for.
func instant(promoter promotion.Promoter, reg ir.TypeRegistry, id ir.TypeID) bool {
	ch, ok := readChain(promoter, reg, id)
	return ok && !ch.encoded && ch.primitive == ir.PrimitiveDatetimeOffset
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
