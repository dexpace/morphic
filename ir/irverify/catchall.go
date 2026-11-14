// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package irverify

import (
	"reflect"
	"slices"
	"strconv"

	"github.com/dexpace/morphic/ir"
)

// checkCatchAllEncodings asserts each entry of a content's CatchAllEncoding
// states something (docs/ir/payloads.md#a-catch-alls-parts): a value, or a
// pattern, since a holder of whose parts the source states nothing has no key
// (ir/catch-all-encoding-empty). Whether a key names a holder among the body's
// shapes is pass.Validate's, and an empty key checkEmptyRefs's.
//
// It prunes all but operationPath and contentPath, for checkStreamingModes's
// reason.
func checkCatchAllEncodings(*ir.Document) walkCheck {
	var vs []Violation
	return walkCheck{
		visit: func(v reflect.Value, at ir.WalkPath) bool {
			if v.Type() != contentType {
				return operationPath[v.Type()] || contentPath[v.Type()]
			}
			if c, ok := reflect.TypeAssert[ir.Content](v); ok && len(c.CatchAllEncoding) > 0 {
				vs = appendEmptyCatchAlls(vs, c.CatchAllEncoding, at)
			}
			return false
		},
		report: reporting(&vs),
	}
}

// appendEmptyCatchAlls reports each entry of catch, the CatchAllEncoding of the
// content at at, stating neither member, in the order of its keys.
func appendEmptyCatchAlls(vs []Violation, catch map[ir.TypeID]ir.CatchAllParts, at ir.WalkPath) []Violation {
	var empty []ir.TypeID
	for holder, parts := range catch {
		if parts.Value == nil && len(parts.Patterns) == 0 {
			empty = append(empty, holder)
		}
	}
	slices.Sort(empty)
	for _, holder := range empty {
		vs = append(vs, Violation{
			Code:    "ir/catch-all-encoding-empty",
			Message: "catch-all encoding of " + strconv.Quote(string(holder)) + " states neither a value nor a pattern",
			Path:    at.String() + ".CatchAllEncoding[" + string(holder) + "]",
		})
	}
	return vs
}
