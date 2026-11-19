// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package irverify

import (
	"encoding/json/jsontext"
	"reflect"
	"strconv"
	"strings"

	"github.com/dexpace/morphic/ir"
)

// checkIDs asserts every interned entity's ID is one the grammar could have
// produced (invariant #3): well-shaped, and — where the entity records the
// source coordinate it was derived from — carrying that coordinate as its path.
//
// Neither half implies the other. Shape alone misses an ID that lost the
// separator between its space and its path: "type/anonaddr" reads as a space named
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
		vs = appendRegistryIDDefect(vs, ir.IDKindType, "types", string(id), string(td.Common().ID), td.Common().Provenance)
	}
	for id, scheme := range doc.Auth {
		vs = appendRegistryIDDefect(vs, ir.IDKindAuth, "auth", string(id), string(scheme.ID), scheme.Provenance)
	}
	for id, server := range doc.Servers {
		vs = appendRegistryIDDefect(vs, ir.IDKindServer, "servers", string(id), string(server.ID), server.Provenance)
	}
	// A channel and a message are held to shape alone, as an operation is: one
	// may be mounted at one pointer and declared at another through a $ref, so
	// its ID's path need not be the pointer its provenance records (GitHub #107).
	for id, ch := range doc.Channels {
		vs = appendRegistryIDDefect(vs, ir.IDKindChannel, "channels", string(id), string(ch.ID), ir.Provenance{})
	}
	for id, msg := range doc.Messages {
		vs = appendRegistryIDDefect(vs, ir.IDKindMessage, "messages", string(id), string(msg.ID), ir.Provenance{})
	}
	return append(vs, checkMemberIDs(doc)...)
}

// appendRegistryIDDefect holds the ID of one entry of registry reg to the
// grammar and to the pointer prov records, where the entry is keyed by its
// node's own ID: an empty key, or another node's, is checkRegistryKeys' one
// report. The path is spelled only for a violation.
func appendRegistryIDDefect(vs []Violation, kind, reg, key, nodeID string, prov ir.Provenance) []Violation {
	if !keyedByOwnID(key, nodeID) {
		return vs
	}
	violation, bad := idDefect(kind, key, prov)
	if !bad {
		return vs
	}
	violation.Path = reg + "[" + key + "]"
	return append(vs, violation)
}

// keyedByOwnID reports whether a registry entry's key is set and is its node's
// own ID. An entry where it is not is checkRegistryKeys' one report, so no other
// check reads its key as the node's ID.
func keyedByOwnID(key, nodeID string) bool {
	return key != "" && key == nodeID
}

// kindPrefixes maps every class of ID to the kind prefix it opens with, so a
// check that needs the prefix of a class asks one table.
var kindPrefixes = map[reflect.Type]string{
	reflect.TypeFor[ir.TypeID]():       ir.IDKindType,
	reflect.TypeFor[ir.AuthID]():       ir.IDKindAuth,
	reflect.TypeFor[ir.OperationID]():  ir.IDKindOperation,
	reflect.TypeFor[ir.ServiceID]():    ir.IDKindService,
	reflect.TypeFor[ir.GroupID]():      ir.IDKindGroup,
	reflect.TypeFor[ir.PropertyID]():   ir.IDKindProperty,
	reflect.TypeFor[ir.ParameterID]():  ir.IDKindParameter,
	reflect.TypeFor[ir.EnumMemberID](): ir.IDKindEnumMember,
	reflect.TypeFor[ir.ServerID]():     ir.IDKindServer,
	reflect.TypeFor[ir.ChannelID]():    ir.IDKindChannel,
	reflect.TypeFor[ir.MessageID]():    ir.IDKindMessage,
}

// pointerDerived is the classes whose ID path is the pointer the declaring
// node's provenance records, so an ID that lost the separator between its space
// and its path disagrees with that pointer. checkIDSpaces sees the same defect
// in every class.
//
// Only a property is in it among the classes that declare their own ID. An
// operation's or parameter's provenance records where it is declared and its ID
// where it is mounted, and the two differ for a $ref'd path item (GitHub #107).
// A service records no pointer, and a group or an enum member no provenance.
var pointerDerived = map[reflect.Type]bool{
	reflect.TypeFor[ir.PropertyID](): true,
}

// appendDeclaredIDViolations reports what is wrong with the ID a node declares
// for itself: nothing at all, a spelling the grammar could not have produced,
// any spelling of a class kindPrefixes does not name, or, where the class is
// pointer-derived, a path that is not the pointer the node records.
//
// It is the grammar checkIDs holds a type's, a scheme's or a server's to, for the
// classes that have no registry for checkIDs to read.
func appendDeclaredIDViolations(vs []Violation, node reflect.Value, class reflect.Type, id string, at ir.WalkPath) []Violation {
	if id == "" {
		noun := ir.RefNoun(class)
		return append(vs, Violation{
			Code:    "ir/empty-" + noun + "-id",
			Message: noun + " declares no identity of its own, so nothing can reference it",
			Path:    at.String(),
		})
	}
	prefix, held := kindPrefixes[class]
	if !held {
		return append(vs, Violation{
			Code:    "ir/id-malformed",
			Message: "id " + strconv.Quote(id) + " is of class " + class.Name() + ", which opens with no kind prefix",
			Path:    at.String(),
		})
	}
	var prov ir.Provenance
	if pointerDerived[class] {
		prov.Pointer = recordedPointer(node)
	}
	violation, bad := idDefect(prefix, id, prov)
	if !bad {
		return vs
	}
	violation.Path = at.String()
	return append(vs, violation)
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
// carries: every primitive is interned at ir.PrimitiveTypeID of its kind, and nothing
// else occupies the space those IDs live in.
//
// checkIDs cannot reach this. A primitive records no pointer, so shape alone
// accepts a string primitive at type/openapi/components/schemas/Name or at
// type/primitive/int32, and documents from different formats stop sharing one node per
// kind (GitHub #73).
//
// The architecture test on ID derivation reaches only this repository's
// compilers, so a Document decoded from JSON, rewritten by a pass or produced
// elsewhere is held by this alone.
func checkPrimIDs(doc *ir.Document) []Violation {
	var vs []Violation
	for id, td := range doc.Types {
		if ir.IsNilTypeDef(td) || !keyedByOwnID(string(id), string(td.Common().ID)) {
			continue // checkRegistryKeys reports the entry itself
		}
		prim, isPrim := td.(*ir.Primitive)
		if !isPrim {
			vs = appendReservedSpace(vs, id, td.Kind())
			continue
		}
		if want := ir.PrimitiveTypeID(prim.PrimitiveKind); id != want {
			vs = append(vs, primIDViolation(id, prim.PrimitiveKind, want))
		}
		if namesSource(doc, prim) {
			vs = append(vs, Violation{
				Code: "ir/prim-names-source",
				Message: "primitive is credited to source " + strconv.Itoa(prim.Provenance.Source) +
					", and a primitive shared by every source names none",
				Path: typePath(id),
			})
		}
	}
	return vs
}

// namesSource reports whether prim is credited to a source. A primitive is one
// node every source shares, so it is on ir.NoSource; the zero Provenance is
// admitted only where the document declares no sources, as a node built without
// one carries it.
func namesSource(doc *ir.Document, prim *ir.Primitive) bool {
	if prim.Provenance.Source == ir.NoSource {
		return false
	}
	return len(doc.Sources) > 0 || prim.Provenance != (ir.Provenance{})
}

// primIDViolation names what is wrong with one primitive's ID.
//
// A kind that is empty is reported on its own terms rather than against a
// destination. ir.PrimitiveTypeID derives type/primitive/ from it, which is not an ID at all
// — checkIDs reports it malformed wherever it is used — so offering it as the
// place the node belongs would send a reader to fix the wrong end.
func primIDViolation(id ir.TypeID, kind ir.PrimitiveKind, want ir.TypeID) Violation {
	msg := "primitive of kind " + strconv.Quote(string(kind)) + " is interned at " + strconv.Quote(string(id)) +
		" rather than the shared " + strconv.Quote(string(want))
	if kind == "" {
		msg = "primitive at " + strconv.Quote(string(id)) + " carries no kind, so no shared ID derives from it"
	}
	return Violation{Code: "ir/prim-id-not-derived", Message: msg, Path: typePath(id)}
}

// appendReservedSpace reports a type that is not a primitive addressing the
// space primitive IDs live in.
//
// The space is reserved rather than merely conventional: a node there either
// collides with the primitive of that kind outright, or squats a name the next
// PrimitiveKind takes. Both make the node reached for a kind depend on which
// declaration lowered first, which is invariant 3's corollary.
func appendReservedSpace(vs []Violation, id ir.TypeID, kind ir.TypeKind) []Violation {
	space, ok := ir.IDSpace(ir.IDKindType, string(id))
	if !ok || space != ir.IDSpacePrimitive {
		return vs
	}
	return append(vs, Violation{
		Code: "ir/prim-space-reserved",
		Message: "id " + strconv.Quote(string(id)) + " addresses the reserved primitive space but names a " +
			string(kind),
		Path: typePath(id),
	})
}

// idDefect returns the way one ID fails to be a derived one, if it does, with
// no Path: the caller spells where, and only once there is a defect to place.
// The two ways exclude each other, since a malformed ID has no path to compare.
func idDefect(kind, id string, prov ir.Provenance) (Violation, bool) {
	if !ir.WellFormedID(kind, id) {
		return Violation{
			Code:    "ir/id-malformed",
			Message: "id " + strconv.Quote(id) + " is not " + kind + "/<space>[/<path>]; every segment must be non-empty",
		}, true
	}
	if prov.Pointer == "" {
		return Violation{}, false // nothing was recorded to disagree with
	}
	idPath, _ := ir.IDPath(kind, id)
	want := strings.TrimPrefix(string(prov.Pointer), ir.IDSeparator)
	if idPath == want {
		return Violation{}, false
	}
	return Violation{
		Code: "ir/id-provenance-disagreement",
		Message: "id " + strconv.Quote(id) + " carries path " + strconv.Quote(idPath) +
			", which is not the source pointer " + strconv.Quote(string(prov.Pointer)) + " it records",
	}, true
}
