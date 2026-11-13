// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package irverify

import (
	"reflect"
	"strconv"

	"github.com/dexpace/morphic/ir"
)

// checkPrefixBindings asserts every parameter binding under a prefix spreads a
// map over the headers or the query parameters, naming no field of its own
// (docs/ir/http-binding.md#prefix-bindings). Another location is
// ir/prefix-binding-location, since no other carries a field per entry; a
// WireName beside the prefix is ir/prefix-binding-wire-name; and a binding of
// a whole parameter whose Type reaches no MapT is ir/prefix-binding-not-map,
// since an emitter declares the input as a map. A member ParamPath names is
// left unread, as nothing here resolves a ParamPath.
//
// It prunes all but operationPath, for checkStreamingModes's reason.
func checkPrefixBindings(doc *ir.Document) walkCheck {
	var vs []Violation
	return walkCheck{
		visit: func(v reflect.Value, at ir.WalkPath) bool {
			if v.Type() != operationType {
				return operationPath[v.Type()]
			}
			if op, ok := reflect.TypeAssert[ir.Operation](v); ok {
				vs = appendPrefixBindingViolations(vs, doc.Types, &op, at)
			}
			return false
		},
		report: reporting(&vs),
	}
}

// appendPrefixBindingViolations appends what is wrong with each binding under
// a prefix among op's HTTP bindings, op sitting at at.
func appendPrefixBindingViolations(vs []Violation, types ir.TypeRegistry, op *ir.Operation, at ir.WalkPath,
) []Violation {
	for i := range op.Bindings.HTTP {
		for j, pb := range op.Bindings.HTTP[i].ParamBindings {
			if pb.Prefix == nil {
				continue
			}
			where := at.String() + ".Bindings.HTTP[" + strconv.Itoa(i) + "].ParamBindings[" + strconv.Itoa(j) + "]"
			vs = appendPrefixBindingViolation(vs, types, op, &pb, where)
		}
	}
	return vs
}

// appendPrefixBindingViolation appends what is wrong with pb, a binding under
// a prefix of a parameter of op, at where.
func appendPrefixBindingViolation(vs []Violation, types ir.TypeRegistry, op *ir.Operation, pb *ir.HTTPParamBinding,
	where string,
) []Violation {
	if pb.Location != ir.HTTPLocationHeader && pb.Location != ir.HTTPLocationQuery {
		vs = append(vs, Violation{
			Code:    "ir/prefix-binding-location",
			Message: "binding under a prefix binds location " + strconv.Quote(string(pb.Location)) + ", which is neither header nor query",
			Path:    where + ".Location",
		})
	}
	if pb.WireName != "" {
		vs = append(vs, Violation{
			Code:    "ir/prefix-binding-wire-name",
			Message: "binding under a prefix also names the field " + strconv.Quote(pb.WireName),
			Path:    where + ".WireName",
		})
	}
	if ref, whole := wholeParamType(op, pb); whole && !reachesMap(types, ref) {
		vs = append(vs, Violation{
			Code:    "ir/prefix-binding-not-map",
			Message: "binding under a prefix binds a parameter of type " + strconv.Quote(string(ref.Target)) + ", which is no map",
			Path:    where + ".Param",
		})
	}
	return vs
}

// wholeParamType returns the type of the parameter of op pb binds, where pb
// binds it whole. It is false for a binding of a member, and for one naming no
// parameter of op, which pass/validate reports.
func wholeParamType(op *ir.Operation, pb *ir.HTTPParamBinding) (ir.TypeRef, bool) {
	if len(pb.ParamPath) > 0 {
		return ir.TypeRef{}, false
	}
	for i := range op.Params {
		if op.Params[i].ID == pb.Param {
			return op.Params[i].Type, true
		}
	}
	return ir.TypeRef{}, false
}
