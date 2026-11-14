// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package irverify

import (
	"reflect"
	"strconv"

	"github.com/dexpace/morphic/ir"
)

var examplesType = reflect.TypeFor[[]ir.Example]()

// checkExampleArms asserts each example holds the arms its carrier takes
// (docs/ir/annotations.md). Only an operation's example is a scenario, with an
// Input and one of Output or Error; every other carrier's is a value, with
// Headers beside it. No source writes one into the other's arm, and an example
// ending in both an output and an error says two outcomes for one run.
//
// The walk finds the lists by type, as checkInferred does, so a carrier added
// later is held at once. A path is spelled only for a violation.
func checkExampleArms(*ir.Document) walkCheck {
	var vs []Violation
	return walkCheck{
		visit: func(v reflect.Value, at ir.WalkPath) bool {
			if v.Type() != examplesType {
				return true
			}
			owner, _, _ := at.Field()
			for i := range v.Len() {
				if ex, ok := reflect.TypeAssert[*ir.Example](v.Index(i).Addr()); ok {
					vs = appendExampleViolations(vs, ex, owner == operationType, exampleSite{list: at, index: i})
				}
			}
			return false
		},
		report: reporting(&vs),
	}
}

// exampleSite is where an example sits: the list the walk reached and its index.
type exampleSite struct {
	list  ir.WalkPath
	index int
}

// path spells the site's arm, which only a violation needs.
func (s exampleSite) path(arm string) string {
	return s.list.String() + "[" + strconv.Itoa(s.index) + "]." + arm
}

// appendExampleViolations reports each arm ex, at site, holds that its carrier
// does not take, and an outcome stated twice. A scenario is the example of an
// operation.
func appendExampleViolations(vs []Violation, ex *ir.Example, scenario bool, site exampleSite) []Violation {
	if scenario {
		vs = appendArm(vs, ex.Value != nil, site, "Value",
			"operation's example holds a value, and a scenario states its input and its outcome")
		vs = appendArm(vs, ex.Headers != nil, site, "Headers",
			"operation's example holds headers, and a scenario states its input and its outcome")
	} else {
		vs = appendArm(vs, ex.Input != nil, site, "Input",
			"example holds a scenario's input, and only an operation's example may")
		vs = appendArm(vs, ex.Output != nil, site, "Output",
			"example holds a scenario's output, and only an operation's example may")
		vs = appendArm(vs, ex.Error != nil, site, "Error",
			"example holds a scenario's error, and only an operation's example may")
	}
	if ex.Output != nil && ex.Error != nil {
		vs = append(vs, Violation{
			Code:    "ir/example-outcome-twice",
			Message: "example ends in both an output and an error",
			Path:    site.path("Error"),
		})
	}
	return vs
}

// appendArm reports an arm of an example that is set where its carrier takes
// none.
func appendArm(vs []Violation, misplaced bool, site exampleSite, arm, message string) []Violation {
	if !misplaced {
		return vs
	}
	return append(vs, Violation{Code: "ir/example-arm-misplaced", Message: message, Path: site.path(arm)})
}
