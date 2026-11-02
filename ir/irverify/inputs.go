// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package irverify

import (
	"reflect"
	"strconv"

	"github.com/dexpace/morphic/ir"
)

var (
	inputPathType  = reflect.TypeFor[ir.InputPath]()
	paginationType = reflect.TypeFor[ir.Pagination]()
)

// declaredPageStops are the stop conditions ir declares
// (docs/ir/interaction-patterns.md#pagination).
var declaredPageStops = map[ir.PageStop]bool{
	ir.PageStopEmpty: true, ir.PageStopNoNext: true, ir.PageStopShort: true, ir.PageStopTotal: true,
}

// checkInputs asserts every ir.InputPath names a value of the request, and
// every pagination stop is one ir declares. A path with no parameter roots in
// the request body, so one with no segment either names the body whole, which
// no input is: ir/input-path-empty. An emitter switches on a stop to end its
// loop, so one it has no arm for is ir/unknown-page-stop. The walk reaches the
// values rather than their carriers, so a new carrier is held at once.
func checkInputs(*ir.Document) walkCheck {
	var vs []Violation
	return walkCheck{
		visit: func(v reflect.Value, at ir.WalkPath) bool {
			switch v.Type() {
			case inputPathType:
				if in, ok := reflect.TypeAssert[ir.InputPath](v); ok && in.Param == nil && len(in.Segments) == 0 {
					vs = append(vs, Violation{
						Code:    "ir/input-path-empty",
						Message: "input path names neither a parameter nor a member of the request body",
						Path:    at.String(),
					})
				}
			case paginationType:
				if pg, ok := reflect.TypeAssert[ir.Pagination](v); ok {
					vs = appendUnknownStops(vs, pg.Stop, at)
				}
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
