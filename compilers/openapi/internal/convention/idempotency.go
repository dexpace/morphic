// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package convention

import (
	"net/http"

	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/ir"
)

// idempotencyKeyHeader is the field a caller's idempotency key travels in, as
// the IETF HTTPAPI working group's Idempotency-Key draft names it.
const idempotencyKeyHeader = "Idempotency-Key"

// idempotency names op's idempotency token, and marks op, under the
// idempotency policy, where op states no idempotency, is bound to HTTP once, by
// a method RFC 9110 does not define as safe, and binds exactly one header
// parameter under idempotencyKeyHeader, compared without case. The rule is
// docs/compilers/openapi.md#idempotency-inference.
func idempotency(c lowering.Ctx, op *ir.Operation) {
	if !c.IdempotencyInference.Honours() || op.Idempotency.Kind != ir.IdempotencyUnknown ||
		len(op.Bindings.HTTP) != 1 || !keyedMethod(op.Bindings.HTTP[0].Method) {
		return
	}
	keys := lowering.IndexParams(op).Bound(ir.HTTPLocationHeader, idempotencyKeyHeader)
	if len(keys) != 1 {
		return
	}
	op.Idempotency = ir.Idempotency{Kind: ir.IdempotencyToken, TokenParam: keys[0]}
	op.Provenance.Inferred = ir.JoinInferred(op.Provenance.Inferred, lowering.IdempotencyInferenceHeuristic)
}

// keyedMethod reports whether method is one of OpenAPI's fixed fields that RFC
// 9110 does not define as safe, so that a key can make a retry of it safe:
// POST and PATCH, which are not idempotent, and PUT and DELETE, whose server
// may still ask for one. A safe method needs no key, and a method an
// additionalOperations entry declares is one whose safety the compiler does not
// know.
func keyedMethod(method string) bool {
	switch method {
	case http.MethodPost, http.MethodPatch, http.MethodPut, http.MethodDelete:
		return true
	default:
		return false
	}
}
