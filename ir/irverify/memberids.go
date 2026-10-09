package irverify

import (
	"strconv"
	"strings"

	"github.com/dexpace/morphic/ir"
)

// checkMemberIDs holds every enum member's ID to the grammar its enum fixes: an
// e/ ID in the space of the enum's own ID, under the enum's path. A member
// carries no Provenance, so unlike checkIDs there is no recorded pointer to
// compare against; the owning enum is the only coordinate it can be held to.
//
// An empty ID is checkDeclaredIDs's, and a repeated one checkDuplicateIDs's.
func checkMemberIDs(doc *ir.Document) []Violation {
	var vs []Violation
	for id, td := range doc.Types {
		enum, ok := td.(*ir.Enum)
		if !ok || enum == nil {
			continue
		}
		for i, m := range enum.Members {
			if m.ID == "" {
				continue
			}
			path := "types[" + string(id) + "].members[" + strconv.Itoa(i) + "]"
			vs = appendMemberIDViolations(vs, id, string(m.ID), path)
		}
	}
	return vs
}

// appendMemberIDViolations reports the ways one member ID can fail to belong to
// the enum at enumID.
func appendMemberIDViolations(vs []Violation, enumID ir.TypeID, id, path string) []Violation {
	if !ir.WellFormedID(ir.IDKindMember, id) {
		return append(vs, Violation{
			Code:    "ir/id-malformed",
			Message: "id " + id + " is not " + ir.IDKindMember + "/<space>[/<path>]; every segment must be non-empty",
			Path:    path,
		})
	}
	space, _ := ir.IDSpace(ir.IDKindMember, id)
	memberPath, _ := ir.IDPath(ir.IDKindMember, id)
	enumSpace, _ := ir.IDSpace(ir.IDKindType, string(enumID))
	enumPath, _ := ir.IDPath(ir.IDKindType, string(enumID))
	if space == enumSpace && strings.HasPrefix(memberPath, enumPath+ir.IDSeparator) {
		return vs
	}
	return append(vs, Violation{
		Code: "ir/member-id-scope",
		Message: "id " + id + " is not in the space and under the path of its enum " +
			string(enumID),
		Path: path,
	})
}
