// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package pass

import (
	"fmt"

	"github.com/dexpace/morphic/ir"
)

// checkDuplicateMemberIDs reports an enum that gives two members one ID.
//
// A member is identified by its value in its enum, and a repeated value's ID ends
// in its occurrence number, so a compiler following docs/ir/enums.md never
// trips this. A document that does has two members an emitter cannot tell
// apart, which is what keying collision resolution by ID
// (docs/emitters/refine/naming.md) rests on not happening. Every enum is read
// through the same map, so none costs one of its own.
func checkDuplicateMemberIDs(doc *ir.Document) []ir.Diagnostic {
	var diags []ir.Diagnostic
	var members reusedMap[ir.EnumMemberID, bool]
	for _, id := range liveTypeIDs(doc) {
		enum, ok := doc.Types[id].(*ir.Enum)
		if !ok {
			continue
		}
		seen := members.emptied()
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
