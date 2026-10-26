package irverify

import (
	"reflect"
	"strconv"

	"github.com/dexpace/morphic/ir"
)

// checkCycles asserts that no pointer, slice or map in the document holds
// itself, however deep. The JSON encoder refuses such a document, so a walk that
// follows each pointer once calls it clean and the next step rejects it (GitHub
// #573). A reference two places merely share encodes twice, and is not one.
//
// No compiler builds one, since a document refers to its nodes by ID; a
// hand-built document can, through a value or a group slice that holds itself.
func checkCycles(*ir.Document) walkCheck {
	var vs []Violation
	return walkCheck{
		visit: func(reflect.Value, ir.WalkPath) bool { return true },
		cycle: func(at, entered ir.WalkPath, ref reflect.Type) {
			vs = append(vs, Violation{
				Code: "ir/cyclic-structure",
				Message: "document reaches the " + ref.String() + " it is already inside (entered at " +
					strconv.Quote(entered.String()) + "), so it cannot be encoded",
				Path: at.String(),
			})
		},
		report: reporting(&vs),
	}
}
