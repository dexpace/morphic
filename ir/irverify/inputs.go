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

// inputPath holds the types between an operation and each ir.InputPath or
// ir.Pagination below it, the rest of the path checkInputs walks past
// operationPath (TestOperationPath_ListsEveryCarrier).
var inputPath = map[reflect.Type]bool{
	reflect.TypeFor[*ir.Pagination]():      true,
	reflect.TypeFor[*ir.InputPath]():       true,
	reflect.TypeFor[*ir.StreamDetail]():    true,
	reflect.TypeFor[ir.StreamDetail]():     true,
	reflect.TypeFor[*ir.StreamCondition](): true,
	reflect.TypeFor[ir.StreamCondition]():  true,
}

// checkInputs asserts every ir.InputPath names a value of the request, and
// every pagination stop is one ir declares, listed once. A path with no
// parameter roots in the request body, so one with no segment either names the
// body whole, which no input is: ir/input-path-empty. An emitter switches on a
// stop to end its loop, so one it has no arm for is ir/unknown-page-stop.
//
// It prunes all but operationPath and inputPath, for checkStreamingModes's
// reason.
func checkInputs(*ir.Document) walkCheck {
	var vs []Violation
	// Every pagination's stops are read through this one, so a list costs no
	// map of its own.
	var stops reusedMap[ir.PageStop, int]
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
				return false
			case paginationType:
				if pg, ok := reflect.TypeAssert[ir.Pagination](v); ok {
					vs = appendStopViolations(vs, pg.Stop, at, &stops)
				}
				return true
			default:
				return operationPath[v.Type()] || inputPath[v.Type()]
			}
		},
		report: reporting(&vs),
	}
}

// appendStopViolations reports each of stops, the Stop of the pagination at
// owner, that ir does not declare, and each repeat of one at its later index, as
// a set's repeat is (ir/set-entry-duplicate): it ends nothing more.
func appendStopViolations(vs []Violation, stops []ir.PageStop, owner ir.WalkPath,
	entries *reusedMap[ir.PageStop, int],
) []Violation {
	if len(stops) == 0 {
		return vs // an empty Stop states no rule, and costs no map
	}
	first := entries.emptied()
	for i, stop := range stops {
		where := owner.String() + ".Stop[" + strconv.Itoa(i) + "]"
		switch earlier, repeated := first[stop]; {
		case !stop.Valid():
			vs = append(vs, Violation{
				Code:    "ir/unknown-page-stop",
				Message: "pagination stops on undeclared condition " + strconv.Quote(string(stop)),
				Path:    where,
			})
		case repeated:
			vs = append(vs, Violation{
				Code:    "ir/set-entry-duplicate",
				Message: "set entry " + strconv.Quote(string(stop)) + " is listed here and at index " + strconv.Itoa(earlier),
				Path:    where,
			})
		default:
			first[stop] = i
		}
	}
	return vs
}
