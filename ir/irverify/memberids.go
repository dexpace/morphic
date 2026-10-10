package irverify

import (
	"slices"
	"strconv"
	"strings"

	"github.com/dexpace/morphic/ir"
)

// checkMemberIDs asserts every enum member's ID lives where its enum does: in
// the namespace of the enum's TypeID and beneath its path.
//
// No node records a pointer for a member to agree with, so this scope is the
// only check tying a member's ID to its enum's.
//
// A malformed or empty member ID is skipped: checkDeclaredIDs reports those once.
func checkMemberIDs(doc *ir.Document) []Violation {
	var vs []Violation
	for _, id := range sortedTypeIDs(doc) {
		enum, isEnum := doc.Types[id].(*ir.Enum)
		if !isEnum || enum == nil {
			continue
		}
		for i, m := range enum.Members {
			if v, bad := memberScopeViolation(id, m.ID, i); bad {
				vs = append(vs, v)
			}
		}
	}
	return vs
}

// memberScopeViolation reports whether member, the i-th of the enum identified
// by enumID, is outside that enum's namespace or path.
func memberScopeViolation(enumID ir.TypeID, member ir.EnumMemberID, i int) (Violation, bool) {
	space, spaceOK := ir.IDSpace(ir.IDKindEnumMember, string(member))
	path, hasPath := ir.IDPath(ir.IDKindEnumMember, string(member))
	enumSpace, enumSpaceOK := ir.IDSpace(ir.IDKindType, string(enumID))
	enumPath, enumHasPath := ir.IDPath(ir.IDKindType, string(enumID))
	if !spaceOK || !hasPath || !enumSpaceOK || !enumHasPath {
		return Violation{}, false
	}
	rest, under := strings.CutPrefix(path, enumPath+ir.IDSeparator)
	if space == enumSpace && under && rest != "" {
		return Violation{}, false
	}
	return Violation{
		Code: "ir/member-id-scope",
		Message: "member id " + string(member) + " is not in the namespace " + enumSpace +
			" and beneath the path " + enumPath + " of its enum " + string(enumID),
		Path: ir.DocumentPath + ".Types[" + string(enumID) + "].Members[" + strconv.Itoa(i) + "]",
	}, true
}

// sortedTypeIDs returns the registry's keys in byte order, so the report does not
// follow map iteration.
func sortedTypeIDs(doc *ir.Document) []ir.TypeID {
	ids := make([]ir.TypeID, 0, len(doc.Types))
	for id := range doc.Types {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	return ids
}
