package pass

import (
	"fmt"

	"github.com/dexpace/morphic/ir"
)

// checkDuplicateMemberIDs reports an enum that gives two members one ID.
//
// A member is identified by its value in its enum, and a repeated value's ID ends
// in its occurrence number, so a compiler following ir-design §4.5 never trips
// this. A document that does has two members an emitter cannot tell apart,
// which is what keying collision resolution by ID (emitter-design §4.12) rests
// on not happening.
func checkDuplicateMemberIDs(doc *ir.Document) []ir.Diagnostic {
	var diags []ir.Diagnostic
	for _, id := range liveTypeIDs(doc) {
		enum, ok := doc.Types[id].(*ir.Enum)
		if !ok {
			continue
		}
		seen := make(map[ir.EnumMemberID]bool, len(enum.Members))
		for i, m := range enum.Members {
			if m.ID == "" {
				continue
			}
			if seen[m.ID] {
				at := fmt.Sprintf("%s/members/%d", id, i)
				diags = append(diags, diag(ir.SeverityError, "pass/duplicate-enum-member-id",
					fmt.Sprintf("enum %s has more than one member with id %q", id, m.ID), at))
			}
			seen[m.ID] = true
		}
	}
	return diags
}
