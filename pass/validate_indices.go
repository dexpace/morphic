package pass

import (
	"fmt"
	"maps"
	"slices"

	"github.com/dexpace/morphic/ir"
)

// checkResponseIndices reports HTTPBinding.SuccessStatus keys that address no
// entry of the operation's Responses.
//
// These are references carried as integer indices, so nothing in their Go type
// marks them as references and the reference walk cannot reach them: they are
// enumerated here instead, and a new one has to be added by hand; irverify's
// integerFields test is what notices it.
func checkResponseIndices(doc *ir.Document) []ir.Diagnostic {
	var diags []ir.Diagnostic
	ir.ForEachOperation(doc, func(op ir.Operation) {
		for i, b := range op.Bindings.HTTP {
			where := fmt.Sprintf("%s/bindings/http/%d", op.ID, i)
			diags = appendSuccessStatusDiags(diags, b.SuccessStatus, len(op.Responses), where)
		}
	})
	return diags
}

// appendSuccessStatusDiags appends to dst a diagnostic per SuccessStatus key that
// addresses none of the declared responses, in ascending key order so map
// iteration cannot reach the output.
func appendSuccessStatusDiags(dst []ir.Diagnostic, status map[int]int, declared int, where string) []ir.Diagnostic {
	for _, index := range slices.Sorted(maps.Keys(status)) {
		if index >= 0 && index < declared {
			continue
		}
		at := fmt.Sprintf("%s/successStatus/%d", where, index)
		dst = append(dst, diag(ir.SeverityError, "ir/response-index-out-of-range",
			fmt.Sprintf("response index %d at %s addresses none of the %d declared responses", index, at, declared),
			at))
	}
	return dst
}
