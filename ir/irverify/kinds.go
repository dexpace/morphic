package irverify

import (
	"strconv"

	"github.com/dexpace/morphic/ir"
)

// checkPrimKinds asserts every primitive names a kind ir declares.
//
// checkPrimIDs cannot reach this: it asks the ID to agree with the kind, and an
// invented kind agrees with itself (GitHub #240). An emitter switches on
// PrimKind to pick a target type, so an undeclared kind is one it can neither
// lower nor report usefully.
//
// Nothing rejects it earlier: PrimKind is a plain string type with no
// UnmarshalJSONFrom, so a decoded or foreign document carries an invented kind
// in unchallenged. An empty kind at the wrong ID is also reported by
// checkPrimIDs, since the repairs differ.
func checkPrimKinds(doc *ir.Document) []Violation {
	var vs []Violation
	for id, td := range doc.Types {
		if ir.IsNilTypeDef(td) {
			continue // checkRegistryKeys reports the nil entry itself
		}
		prim, isPrim := td.(*ir.Primitive)
		if !isPrim || prim.Prim.Valid() {
			continue
		}
		vs = append(vs, Violation{
			Code:    "ir/unknown-prim-kind",
			Message: "primitive carries undeclared kind " + strconv.Quote(string(prim.Prim)),
			Path:    "types[" + string(id) + "]",
		})
	}
	return vs
}

// checkAuthKinds asserts every interned auth scheme names a mechanism ir
// declares.
//
// Nothing else looks at what a scheme says (checkRegistryKeys and checkIDs hold
// only its key and ID shape), so an empty or misspelled mechanism verifies like
// oauth2 (GitHub #295). Kind is the only AuthScheme field backed by a declared
// constant set.
//
// This is the class-level guard for a defect one compiler already refuses at
// the source (GitHub #294, #296). A spec that trips that refusal never reaches
// Verify through internal/harness, which stops at the first error diagnostic,
// so this covers a compiler that mints the shape from a clean spec.
func checkAuthKinds(doc *ir.Document) []Violation {
	var vs []Violation
	for id, scheme := range doc.Auth {
		if scheme.Kind.Valid() {
			continue
		}
		vs = append(vs, Violation{
			Code:    "ir/unknown-auth-kind",
			Message: "auth scheme names undeclared mechanism " + strconv.Quote(string(scheme.Kind)),
			Path:    "auth[" + string(id) + "]",
		})
	}
	return vs
}
