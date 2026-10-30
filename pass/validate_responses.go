// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package pass

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/dexpace/morphic/ir"
)

// checkOneWay reports one-way operations that nonetheless declare responses,
// a Default among them: it is a response, whichever class the plan gives it.
func checkOneWay(doc *ir.Document) []ir.Diagnostic {
	var diags []ir.Diagnostic
	ir.ForEachOperation(doc, func(op ir.Operation) {
		declared := len(op.Responses)
		if op.Default != nil {
			declared++
		}
		if op.OneWay && declared > 0 {
			diags = append(diags, diag(ir.SeverityError, "pass/oneway-with-responses",
				fmt.Sprintf("operation %s is one-way but declares %d response(s)", op.ID, declared),
				string(op.ID)))
		}
	})
	return diags
}

// checkDefaultResponses reports an operation whose Default answers anything but
// the catch-all, the one condition the field holds: a status the source names
// is a Response or an ErrorCase (ir-design §7.2).
func checkDefaultResponses(doc *ir.Document) []ir.Diagnostic {
	var diags []ir.Diagnostic
	ir.ForEachOperation(doc, func(op ir.Operation) {
		if op.Default == nil || answersCatchAllOnly(op.Default.Conditions) {
			return
		}
		where := string(op.ID) + "/default"
		diags = append(diags, diag(ir.SeverityError, "ir/default-response-conditions",
			fmt.Sprintf("default response at %s answers %s, where a default answers the catch-all alone",
				where, statusNames(op.Default.Conditions.StatusCodes)), where))
	})
	return diags
}

// statusNames names the statuses rs answers, for a message: a code as itself, a
// range by its bounds, the zero range as the catch-all, and an empty list as no
// status.
func statusNames(rs []ir.StatusRange) string {
	if len(rs) == 0 {
		return "no status"
	}
	names := make([]string, 0, len(rs))
	for _, r := range rs {
		switch {
		case r == (ir.StatusRange{}):
			names = append(names, "the catch-all")
		case r.From == r.To:
			names = append(names, strconv.Itoa(r.From))
		default:
			names = append(names, strconv.Itoa(r.From)+"-"+strconv.Itoa(r.To))
		}
	}
	return strings.Join(names, " and ")
}

// answersCatchAllOnly reports whether c is the catch-all condition alone: one
// range, the zero one (ir-design §7.2).
func answersCatchAllOnly(c ir.ResponseConditions) bool {
	return len(c.StatusCodes) == 1 && c.StatusCodes[0] == ir.StatusRange{}
}
