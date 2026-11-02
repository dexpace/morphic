// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package irverify

import (
	"reflect"
	"strconv"

	"github.com/dexpace/morphic/ir"
)

// linkPayloads names, for each kind ir declares, the one LinkValue field it
// sets, "" for a kind that sets none (docs/ir/responses.md#links).
var linkPayloads = map[ir.LinkValueKind]string{
	ir.LinkConstant: "Constant",
	ir.LinkRequest:  "Request",
	ir.LinkResponse: "Response",
	ir.LinkURL:      "",
	ir.LinkMethod:   "",
	ir.LinkStatus:   "",
}

// checkLinkValues asserts every ir.LinkValue sets exactly the field its kind
// names, as checkValues does for an ir.Value. An emitter switches on the kind,
// so one ir does not declare is ir/unknown-link-value-kind, and a field the
// kind names left unset, or another field set beside it, is
// ir/link-value-missing-payload or ir/link-value-stray-payload. A response
// value roots in the response carrying the link, so a Root on its path is
// stray too.
func checkLinkValues(*ir.Document) walkCheck {
	var vs []Violation
	return walkCheck{
		visit: func(v reflect.Value, at ir.WalkPath) bool {
			if lv, ok := reflect.TypeAssert[ir.LinkValue](v); ok {
				vs = appendLinkValueViolations(vs, &lv, at)
			}
			return true
		},
		report: reporting(&vs),
	}
}

// appendLinkValueViolations appends what is wrong with lv, at at.
func appendLinkValueViolations(vs []Violation, lv *ir.LinkValue, at ir.WalkPath) []Violation {
	want, declared := linkPayloads[lv.Kind]
	if !declared {
		return append(vs, Violation{
			Code:    "ir/unknown-link-value-kind",
			Message: "link value of undeclared kind " + strconv.Quote(string(lv.Kind)),
			Path:    at.String() + ".Kind",
		})
	}
	set := map[string]bool{"Constant": lv.Constant != nil, "Request": lv.Request != nil, "Response": lv.Response != nil}
	for _, field := range []string{"Constant", "Request", "Response"} {
		switch {
		case field == want && !set[field]:
			vs = append(vs, Violation{
				Code:    "ir/link-value-missing-payload",
				Message: "link value of kind " + strconv.Quote(string(lv.Kind)) + " sets no " + field,
				Path:    at.String() + "." + field,
			})
		case field != want && set[field]:
			vs = append(vs, Violation{
				Code:    "ir/link-value-stray-payload",
				Message: "link value of kind " + strconv.Quote(string(lv.Kind)) + " also sets " + field,
				Path:    at.String() + "." + field,
			})
		}
	}
	if lv.Response != nil && lv.Response.Root != nil {
		vs = append(vs, Violation{
			Code:    "ir/link-value-stray-payload",
			Message: "a response value roots in the response carrying the link, so it names no root",
			Path:    at.String() + ".Response.Root",
		})
	}
	return vs
}
