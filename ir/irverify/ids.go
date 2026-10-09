package irverify

import (
	"encoding/json/jsontext"
	"reflect"
	"strings"

	"github.com/dexpace/morphic/ir"
)

// checkIDs asserts every interned entity's ID is one the grammar could have
// produced (invariant #3): well-shaped, and — where the entity records the
// source coordinate it was derived from — carrying that coordinate as its path.
//
// Neither half implies the other. Shape alone misses an ID that lost the
// separator between its space and its path: "t/anonaddr" reads as a space named
// "anonaddr". The recorded pointer catches it (GitHub #141).
//
// Agreement is asked only of entities that record a pointer. A primitive
// derives from no source position, so checkPrimIDs holds it to the ID its kind
// derives.
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

// kindPrefixes maps every class of ID that opens with a kind prefix to it, so a
// check that needs the prefix of a class asks one table. Channels and messages
// are absent: no compiler mints either, so ir defines no prefix for them yet.
var kindPrefixes = map[reflect.Type]string{
	reflect.TypeFor[ir.TypeID]():    ir.IDKindType,
	reflect.TypeFor[ir.AuthID]():    ir.IDKindAuth,
	reflect.TypeFor[ir.OpID]():      ir.IDKindOp,
	reflect.TypeFor[ir.ServiceID](): ir.IDKindService,
	reflect.TypeFor[ir.GroupID]():   ir.IDKindGroup,
	reflect.TypeFor[ir.PropID]():    ir.IDKindProp,
}

// pointerDerived is the classes whose ID path is the pointer the declaring
// node's provenance records, which is the only way irverify catches an ID that
// lost the separator between its space and its path.
//
// Only a property is in it among the classes that declare their own ID. An
// operation's provenance records where its body is declared and its ID where it
// is mounted, and the two differ for one reached through a $ref'd path item
// (GitHub #107). A service records no pointer and a group no provenance.
var pointerDerived = map[reflect.Type]bool{
	reflect.TypeFor[ir.PropID](): true,
}

// appendDeclaredIDViolations reports what is wrong with the ID a node declares
// for itself: nothing at all, or, for a class with a kind prefix, a spelling the
// grammar could not have produced or, where the class is pointer-derived, a path
// that is not the pointer the node records.
//
// It is the grammar checkIDs holds a type's or a scheme's to, for the classes
// that have no registry for checkIDs to read.
func appendDeclaredIDViolations(vs []Violation, node reflect.Value, class reflect.Type, id, path string) []Violation {
	if id == "" {
		noun := ir.RefNoun(class)
		return append(vs, Violation{
			Code:    "ir/empty-" + noun + "-id",
			Message: noun + " declares no identity of its own, so nothing can reference it",
			Path:    path,
		})
	}
	prefix, held := kindPrefixes[class]
	if !held {
		return vs
	}
	var prov ir.Provenance
	if pointerDerived[class] {
		prov.Pointer = recordedPointer(node)
	}
	return appendIDViolations(vs, prefix, id, prov, path)
}

// recordedPointer returns the pointer the Provenance beside a node's ID records,
// or none when the node has no Provenance. It reads the field rather than
// converting the value for the reason fingerprintOf gives.
func recordedPointer(node reflect.Value) jsontext.Pointer {
	prov := node.FieldByName("Provenance")
	if !prov.IsValid() {
		return ""
	}
	return jsontext.Pointer(prov.FieldByName("Pointer").String())
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
