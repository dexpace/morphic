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
func checkIdempotency(*ir.Document) walkCheck {
	var vs []Violation
	return walkCheck{
		visit: func(v reflect.Value, at ir.WalkPath) bool {
			if v.Type() != idempotencyType {
				return true
			}
			idem, ok := reflect.TypeAssert[ir.Idempotency](v)
			if !ok || (idem.TokenParam != "") == (idem.Kind == ir.IdempotencyToken) {
				return true
			}
			vs = append(vs, Violation{
				Code: "ir/idempotency-token",
				Message: "idempotency kind " + strconv.Quote(string(idem.Kind)) + " with token parameter " +
					strconv.Quote(string(idem.TokenParam)) + ": a token is named exactly when the kind is " +
					strconv.Quote(string(ir.IdempotencyToken)),
				Path: at.String(),
			})
			return true
		},
		report: reporting(&vs),
	}
}
