// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package irverify

import (
	"reflect"
	"strconv"

	"github.com/dexpace/morphic/ir"
)

var headerType = reflect.TypeFor[ir.Header]()

// headerPath holds the types between a Document and each ir.Header, past
// operationPath: the responses and errors declaring headers, and the payloads
// whose part encodings do, a message's included
// (TestOperationPath_ListsEveryCarrier).
var headerPath = map[reflect.Type]bool{
	reflect.TypeFor[[]ir.Response]():                 true,
	reflect.TypeFor[ir.Response]():                   true,
	reflect.TypeFor[*ir.Response]():                  true,
	reflect.TypeFor[[]ir.ErrorCase]():                true,
	reflect.TypeFor[ir.ErrorCase]():                  true,
	reflect.TypeFor[map[ir.MessageID]ir.Message]():   true,
	reflect.TypeFor[ir.Message]():                    true,
	reflect.TypeFor[*ir.Payload]():                   true,
	reflect.TypeFor[ir.Payload]():                    true,
	reflect.TypeFor[[]ir.Content]():                  true,
	reflect.TypeFor[ir.Content]():                    true,
	reflect.TypeFor[*ir.PartEncoding]():              true,
	reflect.TypeFor[ir.PartEncoding]():               true,
	reflect.TypeFor[map[ir.PropID]ir.PartEncoding](): true,
	reflect.TypeFor[[]ir.Header]():                   true,
}

// checkPrefixHeaders asserts every header under a prefix is a map named by no
// field of its own (docs/ir/responses.md#prefix-headers). A Type reaching no
// MapT is ir/prefix-header-not-map, since an emitter declares the header as a
// map, and a WireName beside the prefix is ir/prefix-header-wire-name, since
// the prefix and each key name its fields.
//
// It prunes all but operationPath and headerPath, for checkStreamingModes's
// reason.
func checkPrefixHeaders(doc *ir.Document) walkCheck {
	var vs []Violation
	return walkCheck{
		visit: func(v reflect.Value, at ir.WalkPath) bool {
			if v.Type() != headerType {
				return operationPath[v.Type()] || headerPath[v.Type()]
			}
			if h, ok := reflect.TypeAssert[ir.Header](v); ok && h.Prefix != nil {
				vs = appendPrefixHeaderViolations(vs, doc, &h, at)
			}
			return false
		},
		report: reporting(&vs),
	}
}

// appendPrefixHeaderViolations appends what is wrong with h, a header under a
// prefix, at at.
func appendPrefixHeaderViolations(vs []Violation, doc *ir.Document, h *ir.Header, at ir.WalkPath) []Violation {
	if !reachesMap(doc.Types, h.Type) {
		vs = append(vs, Violation{
			Code:    "ir/prefix-header-not-map",
			Message: "header under a prefix has type " + strconv.Quote(string(h.Type.Target)) + ", which is no map",
			Path:    at.String() + ".Type",
		})
	}
	if h.WireName != "" {
		vs = append(vs, Violation{
			Code:    "ir/prefix-header-wire-name",
			Message: "header under a prefix also names the field " + strconv.Quote(h.WireName),
			Path:    at.String() + ".WireName",
		})
	}
	return vs
}

// reachesMap reports whether ref names a MapT, directly or through decorators.
// A target the registry does not hold, or holds as nil, reports true, as
// ir/dangling-type-ref and ir/nil-type report those. The chain is followed once
// per registered type at most, so a cycle of decorators, which never reaches a
// map, ends and reports false.
func reachesMap(types ir.TypeRegistry, ref ir.TypeRef) bool {
	for range len(types) + 1 {
		td, ok := types[ref.Target]
		if !ok || ir.IsNilTypeDef(td) {
			return true
		}
		if _, isMap := td.(*ir.MapT); isMap {
			return true
		}
		base, decorates := ir.Decorated(td)
		if !decorates {
			return false
		}
		ref = base
	}
	return false
}
