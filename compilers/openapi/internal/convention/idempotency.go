// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package convention

import (
	"net/http"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/ir"
)

// idempotencyKeyHeader is the field a caller's idempotency key travels in, as
// the IETF HTTPAPI working group's Idempotency-Key draft names it.
const idempotencyKeyHeader = "Idempotency-Key"

// methodQuery is the method OpenAPI 3.2's query field binds, which net/http
// names no constant for.
const methodQuery = "QUERY"

// idempotency names op's idempotency token, and marks op, under the
// idempotency policy, where op states no idempotency, is bound to HTTP once, by
// a method RFC 9110 does not define as safe, and binds exactly one header
// parameter under idempotencyKeyHeader, compared without case. A safe method
// states op's idempotency itself, so the header is not read there; a method
// whose safety the compiler does not know, and the header bound twice, are
// reported as undecided. w is the document holding op. The rule is
// docs/compilers/openapi.md#idempotency-inference.
func idempotency(c lowering.Ctx, w *lowering.Whole, op *ir.Operation) []ir.Diagnostic {
	if !c.IdempotencyInference.Honours() || op.Idempotency.Kind != ir.IdempotencyUnknown ||
		len(op.Bindings.HTTP) != 1 {
		return nil
	}
	method := op.Bindings.HTTP[0].Method
	if safeMethod(method) {
		return nil
	}
	keys := w.Params(op).Bound(ir.HTTPLocationHeader, idempotencyKeyHeader)
	switch {
	case len(keys) == 0:
		return nil
	case !keyedMethod(method):
		return undecidedToken(op, "%s is no method a path item's fixed fields declare, so whether it is safe is unknown",
			method)
	case len(keys) > 1:
		return undecidedToken(op, "%d header parameters are named %s, compared without case", len(keys),
			idempotencyKeyHeader)
	}
	op.Idempotency = ir.Idempotency{Kind: ir.IdempotencyToken, TokenParam: keys[0]}
	op.Provenance.Inferred = ir.JoinInferred(op.Provenance.Inferred, lowering.IdempotencyInferenceHeuristic)
	return nil
}

// undecidedToken reports why op takes no token, formatted from format and
// args, at op: the declaration its Provenance names.
func undecidedToken(op *ir.Operation, format string, args ...any) []ir.Diagnostic {
	at := op.Provenance
	at.Inferred = ""
	return []ir.Diagnostic{diag.Newf(ir.SeverityInfo, diag.UndecidedIdempotency, at,
		"no idempotency token is inferred: "+format, args...)}
}

// safeMethod reports whether method is one of OpenAPI's fixed fields that RFC
// 9110 defines as safe (§9.2.1), or 3.2's QUERY, which its draft defines so: a
// method stating that the request changes nothing, and so that sending it
// again does no harm, which says more of the operation than a token.
func safeMethod(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions, http.MethodTrace, methodQuery:
		return true
	default:
		return false
	}
}

// keyedMethod reports whether method is one of OpenAPI's fixed fields that RFC
// 9110 does not define as safe, so that a key can make a retry of it safe:
// POST and PATCH, which are not idempotent, and PUT and DELETE, whose server
// may still ask for one.
func keyedMethod(method string) bool {
	switch method {
	case http.MethodPost, http.MethodPatch, http.MethodPut, http.MethodDelete:
		return true
	default:
		return false
	}
}
