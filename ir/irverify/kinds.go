package irverify

import (
	"strconv"

	"github.com/dexpace/morphic/ir"
)

// checkPrimKinds asserts every primitive names a kind ir declares.
//
// checkPrimIDs cannot reach this and says so: it asks the ID to agree with the
// kind, and an invented kind agrees with itself — the node is consistent and
// wrong (GitHub #240). PrimKind is the vocabulary an emitter switches on to pick
// a target type, so a kind outside the declared set is the one case an emitter
// can neither lower nor report usefully, by which point nothing is left to say
// where the kind came from.
//
// Nothing rejects it earlier either. PrimKind is a plain string type with no
// UnmarshalJSON, so a document decoded from JSON or produced by a compiler
// outside this tree carries an invented kind in unchallenged.
//
// Document.Types is the only place a Primitive lives, so iterating it is the
// whole population. A primitive whose kind is empty is also reported by
// checkPrimIDs wherever it is interned somewhere other than the ID that derives
// from it: the two are different repairs — the ID is wrong there, the kind is
// wrong here — so each is stated on its own terms.
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
// Document.Auth is reached otherwise only by checkRegistryKeys and checkIDs,
// which hold an entry to its key and its ID shape; nothing looks at what the
// scheme says, so a scheme whose mechanism is empty or misspelled verifies
// exactly like one that names oauth2 (GitHub #295). Kind is the only field of an
// AuthScheme backed by a declared constant set — In and the OAuth flow URLs are
// documented shapes rather than Go constants, and holding those here would be
// re-validating the source rather than checking a structure this repository
// minted.
//
// This is the class-level guard for a defect one compiler already refuses at the
// source (GitHub #294, #296). The compiler-side refusal could not be caught here
// or through internal/harness — harness.Check returns at the first error
// diagnostic, before Verify runs — so this exists for the reachable case: a
// compiler that mints the shape from an otherwise clean spec, including the
// compilers not written yet.
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
