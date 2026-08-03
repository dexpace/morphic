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
// pointer; checkPrimIDs holds it to the ID its kind derives instead.
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

// checkPrimIDs asserts the one TypeID ir can derive is the one the document
// carries: every primitive is interned at ir.PrimTypeID of its kind, and nothing
// that is not a primitive occupies the space those IDs live in.
//
// checkIDs cannot reach this. A primitive records no pointer for a path to be
// checked against, so shape alone accepts a string primitive at
// t/openapi/components/schemas/Name, and accepts one at t/prim/int32 — an ID
// contradicting the node it keys. Either way two documents lowered from
// different formats stop reaching the same node for the same kind, which is the
// agreement this ID exists to be (GitHub #73).
//
// The architecture sweep that stops a compiler spelling the ID itself reaches
// only this repository's production packages, so a Document decoded from JSON,
// produced by a compiler outside this tree, or rewritten by a pass is held by
// this and nothing else — the reasoning that put the naming grammar in ir.
//
// Deliberately not checked: whether the kind is one ir declares. This asks the
// ID to agree with the kind, and an invented kind agrees with itself — the node
// is consistent and wrong. Holding PrimKind to its constants is GitHub #240.
func checkPrimIDs(doc *ir.Document) []Violation {
	var vs []Violation
	for id, td := range doc.Types {
		if isNilTypeDef(td) {
			continue // checkRegistryKeys reports the nil entry itself
		}
		path := "types[" + string(id) + "]"
		prim, isPrim := td.(*ir.Primitive)
		if !isPrim {
			vs = appendReservedSpace(vs, id, td.Kind(), path)
			continue
		}
		want := ir.PrimTypeID(prim.Prim)
		if id == want {
			continue
		}
		vs = append(vs, Violation{
			Code: "ir/prim-id-not-derived",
			Message: "primitive of kind " + string(prim.Prim) + " is interned at " + string(id) +
				" rather than the shared " + string(want),
			Path: path,
		})
	}
	return vs
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
