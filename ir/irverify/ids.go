package irverify

import (
	"strings"

	"github.com/dexpace/morphic/ir"
)

// checkIDs asserts every interned entity's ID is one the grammar could have
// produced (invariant #3): well-shaped, and — where the entity records the
// source coordinate it was derived from — carrying that coordinate as its path.
//
// The two halves catch different things and neither implies the other. Shape
// alone misses an ID that lost the separator between its space and its path:
// "t/anonaddr" reads as a space named "anonaddr" and is well-shaped by
// inspection. The path/pointer agreement is what catches that one, because the
// pointer it was derived from is still recorded beside it (GitHub #141).
//
// Agreement is asked only of entities that record a pointer. A primitive is
// shared across every source position and derives from none, so it carries no
// pointer and is held to shape alone.
func checkIDs(doc *ir.Document) []Violation {
	var vs []Violation
	for id, td := range doc.Types {
		if isNilTypeDef(td) {
			continue // checkRegistryKeys reports the nil entry itself
		}
		vs = appendIDViolations(vs, ir.IDKindType, string(id),
			td.Common().Provenance, "types["+string(id)+"]")
	}
	for id, scheme := range doc.Auth {
		vs = appendIDViolations(vs, ir.IDKindAuth, string(id),
			scheme.Provenance, "auth["+string(id)+"]")
	}
	return vs
}

// appendIDViolations reports the ways one ID can fail to be a derived one.
func appendIDViolations(vs []Violation, kind, id string, prov ir.Provenance, path string) []Violation {
	if !ir.WellFormedID(kind, id) {
		return append(vs, Violation{
			Code:    "ir/id-malformed",
			Message: "id " + id + " is not " + kind + "/<space>[/<path>]; every segment must be non-empty",
			Path:    path,
		})
	}
	if prov.Pointer == "" {
		return vs // nothing was recorded to disagree with
	}
	idPath, _ := ir.IDPath(kind, id)
	want := strings.TrimPrefix(prov.Pointer, ir.IDSeparator)
	if idPath == want {
		return vs
	}
	return append(vs, Violation{
		Code: "ir/id-provenance-disagreement",
		Message: "id " + id + " carries path " + idPath +
			", which is not the source pointer " + prov.Pointer + " it records",
		Path: path,
	})
}
