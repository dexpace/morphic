package irverify

import (
	"strings"

	"github.com/dexpace/morphic/ir"
)

// checkIDs asserts every interned entity's ID is one the grammar could have
// produced (invariant #3): well-shaped, and — where the entity records the
// source coordinate it was derived from — carrying that coordinate as its path.
//
// Neither half implies the other: "t/anonaddr" lost the separator between its
// space and its path yet reads as a space named "anonaddr"; the recorded pointer
// catches it (GitHub #141). A primitive records no pointer, so checkPrimIDs holds
// it to the ID its kind derives, and checkGroupIDs holds the groups.
func checkIDs(doc *ir.Document) []Violation {
	var vs []Violation
	for id, td := range doc.Types {
		if ir.IsNilTypeDef(td) {
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

// checkPrimIDs asserts the one TypeID ir can derive is the one the document
// carries: every primitive is interned at ir.PrimTypeID of its kind, and nothing
// else occupies the space those IDs live in.
//
// checkIDs cannot reach this. A primitive records no pointer, so shape alone
// accepts a string primitive at t/openapi/components/schemas/Name or at
// t/prim/int32, and documents from different formats stop sharing one node per
// kind (GitHub #73).
//
// The architecture test on ID derivation reaches only this repository's
// compilers, so a Document decoded from JSON, rewritten by a pass or produced
// elsewhere is held by this alone.
func checkPrimIDs(doc *ir.Document) []Violation {
	var vs []Violation
	for id, td := range doc.Types {
		if ir.IsNilTypeDef(td) {
			continue // checkRegistryKeys reports the nil entry itself
		}
		path := "types[" + string(id) + "]"
		prim, isPrim := td.(*ir.Primitive)
		if !isPrim {
			vs = appendReservedSpace(vs, id, td.Kind(), path)
			continue
		}
		if want := ir.PrimTypeID(prim.Prim); id != want {
			vs = append(vs, primIDViolation(id, prim.Prim, want, path))
		}
	}
	return vs
}

// primIDViolation names what is wrong with one primitive's ID.
//
// A kind that is empty is reported on its own terms rather than against a
// destination. ir.PrimTypeID derives t/prim/ from it, which is not an ID at all
// — checkIDs reports it malformed wherever it is used — so offering it as the
// place the node belongs would send a reader to fix the wrong end.
func primIDViolation(id ir.TypeID, kind ir.PrimKind, want ir.TypeID, path string) Violation {
	msg := "primitive of kind " + string(kind) + " is interned at " + string(id) +
		" rather than the shared " + string(want)
	if kind == "" {
		msg = "primitive at " + string(id) + " carries no kind, so no shared ID derives from it"
	}
	return Violation{Code: "ir/prim-id-not-derived", Message: msg, Path: path}
}

// appendReservedSpace reports a type that is not a primitive addressing the
// space primitive IDs live in.
//
// The space is reserved rather than merely conventional: a node there either
// collides with the primitive of that kind outright, or squats a name the next
// PrimKind takes. Both make the node reached for a kind depend on which
// declaration lowered first, which is invariant 3's corollary.
func appendReservedSpace(vs []Violation, id ir.TypeID, kind ir.TypeKind, path string) []Violation {
	space, ok := ir.IDSpace(ir.IDKindType, string(id))
	if !ok || space != ir.IDSpacePrim {
		return vs
	}
	return append(vs, Violation{
		Code: "ir/prim-space-reserved",
		Message: "id " + string(id) + " addresses the reserved primitive space but names a " +
			string(kind),
		Path: path,
	})
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
	want := strings.TrimPrefix(string(prov.Pointer), ir.IDSeparator)
	if idPath == want {
		return vs
	}
	return append(vs, Violation{
		Code: "ir/id-provenance-disagreement",
		Message: "id " + id + " carries path " + idPath +
			", which is not the source pointer " + string(prov.Pointer) + " it records",
		Path: path,
	})
}
