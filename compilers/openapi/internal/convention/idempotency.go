// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package convention

import (
	"net/http"

	"github.com/dexpace/morphic/compilers/openapi/internal/compileddoc"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/ir"
)

// idempotencyKeyHeader is the field a caller's idempotency key travels in, as
// the IETF HTTPAPI working group's Idempotency-Key draft names it.
const idempotencyKeyHeader = "Idempotency-Key"

// methodQuery is the method OpenAPI 3.2's query field binds, which net/http
// names no constant for.
const methodQuery = "QUERY"

// idempotency names operation's idempotency token, and marks operation, under the idempotency
// policy, where operation states no idempotency, is bound to HTTP once, by a method
// RFC 9110 does not define as safe, and binds exactly one header parameter
// under idempotencyKeyHeader, compared without case. A safe method states operation's
// idempotency itself, so the header is not read there; a method whose safety
// the compiler does not know, and the header bound twice, are reported as
// undecided. compiledDocumentView is the document holding operation. The rule is
// docs/compilers/openapi.md#idempotency-inference.
func idempotency(policy IdempotencyInference, compiledDocumentView *compileddoc.CompiledDocumentView, operation *ir.Operation) []ir.Diagnostic {
	if !policy.Honours() || operation.Idempotency.Kind != ir.IdempotencyUnknown ||
		len(operation.Bindings.HTTP) != 1 {
		return nil
	}
	method := operation.Bindings.HTTP[0].Method
	if safeMethod(method) {
		return nil
	}
	keys := compiledDocumentView.Params(operation).Bound(ir.HTTPLocationHeader, idempotencyKeyHeader)
	switch {
	case len(keys) == 0:
		return nil
	case !keyedMethod(method):
		return undecidedToken(operation, "%s is no method a path item's fixed fields declare, so whether it is safe is unknown",
			method)
	case len(keys) > 1:
		return undecidedToken(operation, "%d header parameters are named %s, compared without case", len(keys),
			idempotencyKeyHeader)
	}
	operation.Idempotency = ir.Idempotency{Kind: ir.IdempotencyToken, TokenParam: keys[0]}
	operation.Provenance.Inferred = ir.JoinInferred(operation.Provenance.Inferred, IdempotencyInferenceHeuristic)
	return nil
}

// undecidedToken reports why operation takes no token, formatted from format and
// args, at operation: the declaration its Provenance names.
func undecidedToken(operation *ir.Operation, format string, args ...any) []ir.Diagnostic {
	at := operation.Provenance
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
