package pass

import (
	"fmt"
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
			diags = appendSuccessStatusDiags(diags, b.SuccessStatus, len(op.Responses), op.ID, i)
		}
	})
	return diags
}

// appendSuccessStatusDiags appends to dst a diagnostic per SuccessStatus key of
// HTTP binding i of operation op that addresses none of the declared responses,
// in ascending key order so map iteration cannot reach the output. Only the
// keys reported are sorted, and only their location spelled.
func appendSuccessStatusDiags(dst []ir.Diagnostic, status map[int]int, declared int, op ir.OpID, i int) []ir.Diagnostic {
	var outside []int
	for index := range status {
		if index < 0 || index >= declared {
			outside = append(outside, index)
		}
	}
	if len(outside) == 0 {
		return dst
	}
	slices.Sort(outside)
	where := fmt.Sprintf("%s/bindings/http/%d", op, i)
	for _, index := range outside {
		at := fmt.Sprintf("%s/successStatus/%d", where, index)
		dst = append(dst, diag(ir.SeverityError, "ir/response-index-out-of-range",
			fmt.Sprintf("response index %d at %s addresses none of the %d declared responses", index, at, declared),
			at))
	}
	return dst
}
