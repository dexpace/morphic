// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package irverify

import (
	"reflect"
	"strconv"

	"github.com/dexpace/morphic/ir"
)

var idempotencyType = reflect.TypeFor[ir.Idempotency]()

// checkIdempotency asserts every Idempotency pairs its token with its kind:
// TokenParam set exactly when Kind is ir.IdempotencyToken, as ir.Idempotency
// documents. pass.Validate reports the same disagreement in a document from any
// producer; this holds a compiler's own output to it.
//
// It prunes all but operationPath, for checkStreamingModes's reason.
func checkIdempotency(*ir.Document) walkCheck {
	var vs []Violation
	return walkCheck{
		visit: func(v reflect.Value, at ir.WalkPath) bool {
			if v.Type() != idempotencyType {
				return operationPath[v.Type()]
			}
			idem, ok := reflect.TypeAssert[ir.Idempotency](v)
			if !ok || (idem.TokenParam != "") == (idem.Kind == ir.IdempotencyToken) {
				return false
			}
			vs = append(vs, Violation{
				Code: "ir/idempotency-token",
				Message: "idempotency kind " + strconv.Quote(string(idem.Kind)) + " with token parameter " +
					strconv.Quote(string(idem.TokenParam)) + ": a token is named exactly when the kind is " +
					strconv.Quote(string(ir.IdempotencyToken)),
				Path: at.String(),
			})
			return false
		},
		report: reporting(&vs),
	}
}
