package pass

import (
	"fmt"

	"github.com/dexpace/morphic/ir"
)

// checkIdempotency reports an operation whose idempotency token disagrees with
// its kind, which ir.Idempotency pairs: the token kind naming no TokenParam
// leaves an emitter no parameter to carry the token, and a TokenParam on any
// other kind is a token an emitter reading it alone would send unasked. Whether
// the token names the operation's own parameter is checkParamReferences'.
func checkIdempotency(doc *ir.Document) []ir.Diagnostic {
	var diags []ir.Diagnostic
	ir.ForEachOperation(doc, func(op ir.Operation) {
		idem := op.Idempotency
		hasToken := idem.TokenParam != ""
		if hasToken == (idem.Kind == ir.IdempotencyToken) {
			return
		}
		where := fmt.Sprintf("%s/idempotency", op.ID)
		msg := fmt.Sprintf("%s is idempotent by token but names no token parameter", op.ID)
		if hasToken {
			msg = fmt.Sprintf("%s names token parameter %q under idempotency kind %q, which takes none",
				op.ID, idem.TokenParam, idem.Kind)
		}
		diags = append(diags, diag(ir.SeverityError, "pass/idempotency-token-mismatch", msg, where))
	})
	return diags
}
