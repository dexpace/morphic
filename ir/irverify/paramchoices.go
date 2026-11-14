// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package irverify

import (
	"reflect"
	"strconv"

	"github.com/dexpace/morphic/ir"
)

// checkParamChoices asserts that each choice among an operation's parameters
// chooses (docs/ir/operations.md#choices-among-parameters): it has two variants
// or more (ir/param-choice-single), each naming a parameter
// (ir/param-variant-empty), none twice, as a set lists an entry once
// (ir/set-entry-duplicate). An empty ID is checkEmptyRefs's. It prunes all but
// operationPath, for checkStreamingModes's reason, and reads each operation's
// choices off the operation.
func checkParamChoices(*ir.Document) walkCheck {
	var vs []Violation
	// Every variant is read through this one, so a variant costs no map.
	var seen reusedMap[ir.ParamID, int]
	return walkCheck{
		visit: func(v reflect.Value, at ir.WalkPath) bool {
			if v.Type() != operationType {
				return operationPath[v.Type()]
			}
			if op, ok := reflect.TypeAssert[ir.Operation](v); ok {
				for i, choice := range op.ParamChoices {
					vs = appendChoiceViolations(vs, choice, at.String()+".ParamChoices["+strconv.Itoa(i)+"]", &seen)
				}
			}
			return false
		},
		report: reporting(&vs),
	}
}

// appendChoiceViolations reports what choice, at where, fails of a choice.
func appendChoiceViolations(vs []Violation, choice ir.ParamChoice, where string, seen *reusedMap[ir.ParamID, int]) []Violation {
	if len(choice.Variants) < 2 {
		vs = append(vs, Violation{
			Code:    "ir/param-choice-single",
			Message: "choice " + strconv.Quote(choice.Name.Source) + " has fewer than two variants, so it chooses nothing",
			Path:    where,
		})
	}
	for j, variant := range choice.Variants {
		at := where + ".Variants[" + strconv.Itoa(j) + "]"
		if len(variant.Params) == 0 {
			vs = append(vs, Violation{
				Code:    "ir/param-variant-empty",
				Message: "variant " + strconv.Quote(variant.Name.Source) + " names no parameter",
				Path:    at,
			})
			continue
		}
		first := seen.emptied()
		for k, id := range variant.Params {
			if earlier, repeated := first[id]; repeated {
				vs = append(vs, Violation{
					Code:    "ir/set-entry-duplicate",
					Message: "set entry " + strconv.Quote(string(id)) + " is listed here and at index " + strconv.Itoa(earlier),
					Path:    at + ".Params[" + strconv.Itoa(k) + "]",
				})
				continue
			}
			first[id] = k
		}
	}
	return vs
}
