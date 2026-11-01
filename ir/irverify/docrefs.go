// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package irverify

import (
	"fmt"
	"reflect"
	"strconv"
	"unicode/utf8"

	"github.com/dexpace/morphic/ir"
)

var docsType = reflect.TypeFor[ir.Docs]()

// checkDocRefs asserts every ir.Docs marks spans of its own Description, in
// order and without overlap, so an emitter can splice a link over each in one
// pass over the text (docs/ir/annotations.md#references-in-prose). A span
// outside the text, splitting a rune or empty names nothing a reader can link,
// and one replaced Description left behind is the likeliest way to get one.
//
// Whether a span's type exists is checkReferentialIntegrity's, since a DocRef's
// Type is a TypeID like any other; whether it is set is checkEmptyRefs'.
func checkDocRefs(*ir.Document) walkCheck {
	var vs []Violation
	return walkCheck{
		visit: func(v reflect.Value, at ir.WalkPath) bool {
			if v.Type() != docsType {
				return true
			}
			docs, ok := reflect.TypeAssert[ir.Docs](v)
			if !ok || len(docs.Refs) == 0 {
				return false // a Docs holds nothing else this check looks for
			}
			vs = append(vs, docRefViolations(docs, at)...)
			return false
		},
		report: reporting(&vs),
	}
}

// docRefViolations returns what is wrong with the spans of docs, which sits at
// at, spelling a path only for a finding. A span that fails is left out of the
// ordering, so one bad offset is one finding rather than one for each span after
// it.
func docRefViolations(docs ir.Docs, at ir.WalkPath) []Violation {
	var vs []Violation
	end := 0
	for i, ref := range docs.Refs {
		switch {
		case !isSpanOf(ref, docs.Description):
			vs = append(vs, Violation{
				Code: "ir/doc-ref-span",
				Message: fmt.Sprintf("doc reference [%d,%d) is no span of the %d bytes of its description: "+
					"it is empty, outside them or splits a rune", ref.Start, ref.End, len(docs.Description)),
				Path: docRefPath(at, i),
			})
		case ref.Start < end:
			vs = append(vs, Violation{
				Code: "ir/doc-ref-order",
				Message: fmt.Sprintf("doc reference [%d,%d) starts before the span ahead of it ends, at %d",
					ref.Start, ref.End, end),
				Path: docRefPath(at, i),
			})
			end = max(end, ref.End)
		default:
			end = ref.End
		}
	}
	return vs
}

// docRefPath spells the path of the span at index i of the Docs at at, as the
// walk would.
func docRefPath(at ir.WalkPath, i int) string {
	return at.String() + ".Refs[" + strconv.Itoa(i) + "]"
}

// isSpanOf reports whether ref marks a non-empty run of whole runes of text. A
// byte that does not decode is checkUTF8's to report, so it counts as a
// boundary here: utf8.RuneStart rejects only a continuation byte.
func isSpanOf(ref ir.DocRef, text string) bool {
	return 0 <= ref.Start && ref.Start < ref.End && ref.End <= len(text) &&
		utf8.RuneStart(text[ref.Start]) && (ref.End == len(text) || utf8.RuneStart(text[ref.End]))
}
