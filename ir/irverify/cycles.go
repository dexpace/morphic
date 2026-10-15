package irverify

import (
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
func checkCycles(doc *ir.Document, _ declarations) ([]Violation, bool) {
	cycles, truncated := ir.ValueCycles(doc, ir.DocumentPath)
	vs := make([]Violation, 0, len(cycles))
	for _, c := range cycles {
		vs = append(vs, Violation{
			Code: "ir/cyclic-structure",
			Message: "document reaches the " + c.Type + " it is already inside (entered at " +
				strconv.Quote(c.Entered) + "), so it cannot be encoded",
			Path: c.Path,
		})
	}
	return vs, truncated
}
