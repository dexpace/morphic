// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package irverify

import (
	"reflect"
	"strconv"

	"github.com/dexpace/morphic/ir"
)

var (
	inputPathType    = reflect.TypeFor[ir.InputPath]()
	paginationType   = reflect.TypeFor[ir.Pagination]()
	streamDetailType = reflect.TypeFor[ir.StreamDetail]()
)

// interactionPath holds the types between an operation and each ir.InputPath,
// ir.Pagination or ir.StreamDetail below it, past operationPath, that
// checkInteractions has no case of its own for
// (TestOperationPath_ListsEveryCarrier).
var interactionPath = map[reflect.Type]bool{
	reflect.TypeFor[*ir.Pagination]():      true,
	reflect.TypeFor[*ir.InputPath]():       true,
	reflect.TypeFor[*ir.StreamDetail]():    true,
	reflect.TypeFor[*ir.StreamCondition](): true,
	reflect.TypeFor[ir.StreamCondition]():  true,
	// A link's request value reads the request of the operation answered.
	reflect.TypeFor[[]ir.Response]():      true,
	reflect.TypeFor[ir.Response]():        true,
	reflect.TypeFor[*ir.Response]():       true,
	reflect.TypeFor[[]ir.ErrorCase]():     true,
	reflect.TypeFor[ir.ErrorCase]():       true,
	reflect.TypeFor[[]ir.OperationLink](): true,
	reflect.TypeFor[ir.OperationLink]():   true,
	reflect.TypeFor[[]ir.LinkParam]():     true,
	reflect.TypeFor[ir.LinkParam]():       true,
	reflect.TypeFor[ir.LinkValue]():       true,
	reflect.TypeFor[*ir.LinkValue]():      true,
}

// checkInteractions asserts what starts and ends a page walk or a stream
// (docs/ir/interaction-patterns.md): every ir.InputPath names a value its
// operation's request carries, every pagination stop is one ir declares, listed
// once, and no stream ends on empty data. An emitter switches on a stop to end
// its loop, so one it has no arm for is ir/unknown-page-stop.
//
// It prunes all but operationPath and interactionPath, for
// checkStreamingModes's reason. Operations never nest, so the one the walk last
// entered holds every input path it meets.
func checkInteractions(*ir.Document) walkCheck {
	var vs []Violation
	// Every pagination's stops are read through this one, so a list costs no
	// map of its own.
	var stops reusedMap[ir.PageStop, int]
	bodyless := false // the operation the walk is in sends no request body
	return walkCheck{
		visit: func(v reflect.Value, at ir.WalkPath) bool {
			switch v.Type() {
			case operationType:
				op, ok := reflect.TypeAssert[ir.Operation](v)
				bodyless = ok && op.Request == nil
				return true
			case inputPathType:
				if in, ok := reflect.TypeAssert[ir.InputPath](v); ok {
					vs = appendInputViolation(vs, in, bodyless, at)
				}
				return false
			case paginationType:
				if pg, ok := reflect.TypeAssert[ir.Pagination](v); ok {
					vs = appendStopViolations(vs, pg.Stop, at, &stops)
				}
				return true
			case streamDetailType:
				if sd, ok := reflect.TypeAssert[ir.StreamDetail](v); ok {
					vs = appendTerminatorViolation(vs, sd.Terminator, at)
				}
				return true
			default:
				return operationPath[v.Type()] || interactionPath[v.Type()]
			}
		},
		report: reporting(&vs),
	}
}

// appendInputViolation reports in, where the walk reached it, when it names no
// value of the request. With no parameter it roots in the request body, the
// body whole when it names no member either, so it needs an operation that
// sends one (ir/input-body-absent).
func appendInputViolation(vs []Violation, in ir.InputPath, bodyless bool, at ir.WalkPath) []Violation {
	if in.Param != nil || !bodyless {
		return vs
	}
	return append(vs, Violation{
		Code:    "ir/input-body-absent",
		Message: "input path reads the request body, and the operation sends none",
		Path:    at.String(),
	})
}

// appendTerminatorViolation reports an empty terminator of the stream at owner:
// compared with each frame's data, it would end the stream at any frame holding
// none, a keep-alive among them (ir/stream-terminator-empty).
func appendTerminatorViolation(vs []Violation, terminator *string, owner ir.WalkPath) []Violation {
	if terminator == nil || *terminator != "" {
		return vs
	}
	return append(vs, Violation{
		Code:    "ir/stream-terminator-empty",
		Message: "stream terminator is empty, so a frame holding no data would end the stream",
		Path:    owner.String() + ".Terminator",
	})
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
