// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package irverify

import (
	"reflect"
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
			Path:    typePath(id),
		})
	}
	return vs
}

var streamingModeType = reflect.TypeFor[ir.StreamingMode]()

// streamingPath holds the types between a Document and Operation.Streaming, the
// one field of its type (TestStreamingPath_ListsEveryCarrier).
var streamingPath = map[reflect.Type]bool{
	reflect.TypeFor[*ir.Document]():        true,
	reflect.TypeFor[ir.Document]():         true,
	reflect.TypeFor[[]ir.Service]():        true,
	reflect.TypeFor[ir.Service]():          true,
	reflect.TypeFor[[]ir.OperationGroup](): true,
	reflect.TypeFor[ir.OperationGroup]():   true,
	reflect.TypeFor[[]ir.Operation]():      true,
	reflect.TypeFor[ir.Operation]():        true,
}

// checkStreamingModes asserts every Operation.Streaming holds a mode ir
// declares. An emitter switches on the mode to shape a method, and StreamingMode
// is a plain string type, so nothing else rejects an invented one. The other
// wire enums with no Valid method are not held yet (GitHub #1190).
//
// It prunes all but streamingPath: shown every value, it cost each verified
// compile 0.8% for one field of one struct.
func checkStreamingModes(*ir.Document) walkCheck {
	var vs []Violation
	return walkCheck{
		visit: func(v reflect.Value, at ir.WalkPath) bool {
			if v.Type() != streamingModeType {
				return streamingPath[v.Type()]
			}
			mode, ok := reflect.TypeAssert[ir.StreamingMode](v)
			if !ok || mode.Valid() {
				return false
			}
			vs = append(vs, Violation{
				Code:    "ir/unknown-streaming-mode",
				Message: "operation carries undeclared streaming mode " + strconv.Quote(string(mode)),
				Path:    at.String(),
			})
			return false
		},
		report: reporting(&vs),
	}
}

// checkAuthKinds asserts every interned auth scheme has a kind ir declares.
//
// Nothing else looks at what a scheme says (checkRegistryKeys and checkIDs hold
// only its key and ID shape), so an empty or misspelled kind verifies like
// oauth2 (GitHub #295). Kind is the only AuthScheme field backed by a declared
// constant set.
//
// This is the class-level guard for a defect one compiler already reports at
// the source (GitHub #294, #296): that report covers a spec spelling the shape,
// and this covers a compiler that mints it from a clean spec.
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
