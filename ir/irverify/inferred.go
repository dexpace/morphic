// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package irverify

import (
	"reflect"
	"strconv"

	"github.com/dexpace/morphic/ir"
)

var (
	inferredType     = reflect.TypeFor[ir.Inferred]()
	partEncodingType = reflect.TypeFor[ir.PartEncoding]()
)

// checkInferred asserts every inferred-marker list is in the one form
// ir.JoinInferred writes (docs/ir/unmodeled.md#promotion), so that two lists
// naming one set of heuristics in one order are one string. ir.Inferred is a
// defined string type, so a conversion skips the join, a decoded document never
// meets it, and a round trip carries "a,,b" faithfully.
//
// The walk finds the lists by type, so a new carrier is held at once. A list is
// read byte by byte, as the join reads it, a name that does not decode being
// checkUTF8's to report. A marker on a part with no Filename is reported.
func checkInferred(*ir.Document) walkCheck {
	var vs []Violation
	return walkCheck{
		visit: func(v reflect.Value, at ir.WalkPath) bool {
			if v.Type() == partEncodingType {
				vs = append(vs, partMarkerViolations(v, at)...)
				return true
			}
			if v.Type() != inferredType {
				return true
			}
			if list := ir.Inferred(v.String()); !list.IsCanonical() {
				vs = append(vs, Violation{
					Code: "ir/inferred-not-canonical",
					Message: "inferred marker " + strconv.Quote(string(list)) + " is not the list " +
						strconv.Quote(string(ir.JoinInferred(list))) + " ir.JoinInferred writes for it",
					Path: at.String(),
				})
			}
			return false
		},
		report: reporting(&vs),
	}
}

// partMarkerViolations reports the marker of part, a PartEncoding at at, when
// the part carries no Filename for it to qualify.
func partMarkerViolations(part reflect.Value, at ir.WalkPath) []Violation {
	inferred := part.FieldByName("Inferred").String()
	if inferred == "" || part.FieldByName("Filename").Bool() {
		return nil
	}
	return []Violation{{
		Code:    "ir/part-encoding-inferred-not-filename",
		Message: "part encoding is marked inferred by " + strconv.Quote(inferred) + " but carries no filename",
		Path:    at.String() + ".Inferred",
	}}
}
