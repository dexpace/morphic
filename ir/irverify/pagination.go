// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package irverify

import (
	"reflect"
	"strconv"

	"github.com/dexpace/morphic/ir"
)

// declaredPageStops are the stop conditions ir declares
// (docs/ir/interaction-patterns.md#pagination).
var declaredPageStops = map[ir.PageStop]bool{
	ir.PageStopEmpty: true, ir.PageStopNoNext: true, ir.PageStopShort: true, ir.PageStopTotal: true,
}

// checkPageStops asserts every pagination stop is one ir declares. An emitter
// switches on a stop to end its loop, so one it has no arm for is
// ir/unknown-page-stop.
func checkPageStops(*ir.Document) walkCheck {
	var vs []Violation
	return walkCheck{
		visit: func(v reflect.Value, at ir.WalkPath) bool {
			if pg, ok := reflect.TypeAssert[ir.Pagination](v); ok {
				vs = appendUnknownStops(vs, pg.Stop, at)
			}
			return true
		},
		report: reporting(&vs),
	}
}

// appendUnknownStops reports each stop in stops ir does not declare.
func appendUnknownStops(vs []Violation, stops []ir.PageStop, at ir.WalkPath) []Violation {
	for i, stop := range stops {
		if !declaredPageStops[stop] {
			vs = append(vs, Violation{
				Code:    "ir/unknown-page-stop",
				Message: "pagination stops on undeclared condition " + strconv.Quote(string(stop)),
				Path:    at.String() + ".Stop[" + strconv.Itoa(i) + "]",
			})
		}
	}
	return vs
}
