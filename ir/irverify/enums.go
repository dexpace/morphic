// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package irverify

import (
	"maps"
	"reflect"
	"strconv"

	"github.com/dexpace/morphic/ir"
)

// heldEnums names each string enum of ir that Verify holds to its declared set,
// with the code a value outside it draws. An enum is a bare string type, so
// nothing else rejects an invented value on the wire, and a consumer's switch
// has no arm for one. TestEnums_EveryDeclaredEnumIsHeld holds this table and
// enumExceptions to the enums ir declares.
var heldEnums = map[string]string{
	"AdditionalMode":  "ir/unknown-additional-mode",
	"AuthKind":        "ir/unknown-auth-kind",
	"HTTPInbound":     "ir/unknown-http-inbound",
	"HTTPLocation":    "ir/unknown-http-location",
	"IdempotencyKind": "ir/unknown-idempotency-kind",
	"LinkValueKind":   "ir/unknown-link-value-kind",
	"MsgDirection":    "ir/unknown-message-direction",
	"PageStop":        "ir/unknown-page-stop",
	"PageStrategy":    "ir/unknown-page-strategy",
	"PresenceKind":    "ir/unknown-presence-kind",
	"PrimKind":        "ir/unknown-prim-kind",
	"Severity":        "ir/unknown-severity",
	"Stability":       "ir/unknown-stability",
	"StreamingMode":   "ir/unknown-streaming-mode",
	"UnmodeledReason": "ir/unknown-unmodeled-reason",
	"ValueKind":       "ir/unknown-value-kind",
}

// enumExceptions names each string enum of ir that Verify does not hold, with
// the reason it need not.
var enumExceptions = map[string]string{
	"TypeKind":      "the codec refuses an unknown tag, so no decoded document holds one",
	"Undistributed": "no Document holds one: it is the answer of a derivation, not a field",
	"treeClass":     "the editor's own state, never part of a Document",
}

// enumRule is how the walk holds one enum: whether a value is declared, and the
// code and words of the violation an undeclared one draws.
type enumRule struct {
	valid func(string) bool
	code  string
	what  string
}

// wireEnum is the rule of enum E, declared by valid.
func wireEnum[E ~string](valid func(E) bool, code, what string) enumRule {
	return enumRule{valid: func(s string) bool { return valid(E(s)) }, code: code, what: what}
}

// walkEnums are the enums checkEnums holds, each at the one type that carries
// it. An enum whose carrier has its own check is held there, and one carried by
// every model by checkModelEnums.
var walkEnums = map[reflect.Type]enumRule{
	reflect.TypeFor[ir.HTTPLocation](): wireEnum(ir.HTTPLocation.Valid,
		"ir/unknown-http-location", "parameter binding carries undeclared location"),
	reflect.TypeFor[ir.IdempotencyKind](): wireEnum(ir.IdempotencyKind.Valid,
		"ir/unknown-idempotency-kind", "operation carries undeclared idempotency kind"),
	reflect.TypeFor[ir.MsgDirection](): wireEnum(ir.MsgDirection.Valid,
		"ir/unknown-message-direction", "message binding carries undeclared direction"),
	reflect.TypeFor[ir.PageStrategy](): wireEnum(ir.PageStrategy.Valid,
		"ir/unknown-page-strategy", "pagination names undeclared strategy"),
	reflect.TypeFor[ir.Severity](): wireEnum(ir.Severity.Valid,
		"ir/unknown-severity", "diagnostic carries undeclared severity"),
}

// enumPath is operationPath and the types between an Operation, or a document's
// or a run's diagnostics, and each field walkEnums holds
// (TestEnumPath_ListsEveryCarrier).
var enumPath = func() map[reflect.Type]bool {
	path := maps.Clone(webhookPath)
	for _, t := range []reflect.Type{
		reflect.TypeFor[ir.HTTPBinding](), reflect.TypeFor[[]ir.HTTPParamBinding](), reflect.TypeFor[ir.HTTPParamBinding](),
		reflect.TypeFor[*ir.MessageBinding](), reflect.TypeFor[ir.MessageBinding](),
		reflect.TypeFor[*ir.Pagination](), reflect.TypeFor[ir.Pagination](),
		reflect.TypeFor[ir.Idempotency](),
		reflect.TypeFor[[]ir.Diagnostic](), reflect.TypeFor[ir.Diagnostic](),
	} {
		path[t] = true
	}
	return path
}()

// checkEnums asserts every field of an enum in walkEnums holds a declared value.
// It prunes all but enumPath, for checkStreamingModes's reason.
func checkEnums(*ir.Document) walkCheck {
	var vs []Violation
	return walkCheck{
		visit: func(v reflect.Value, at ir.WalkPath) bool {
			rule, held := walkEnums[v.Type()]
			if !held {
				return enumPath[v.Type()]
			}
			if value := v.String(); !rule.valid(value) {
				vs = append(vs, Violation{
					Code:    rule.code,
					Message: rule.what + " " + strconv.Quote(value),
					Path:    at.String(),
				})
			}
			return false
		},
		report: reporting(&vs),
	}
}

// checkModelEnums asserts every model's Additional and each of its properties'
// Presence is a value ir declares. A model is a registry entry, not a field
// the walk prunes to, so these are read from the registry.
func checkModelEnums(doc *ir.Document) []Violation {
	var vs []Violation
	for id, td := range doc.Types {
		model, isModel := td.(*ir.Model)
		if !isModel || ir.IsNilTypeDef(td) {
			continue
		}
		if !model.Additional.Valid() {
			vs = append(vs, Violation{
				Code:    "ir/unknown-additional-mode",
				Message: "model carries undeclared additional mode " + strconv.Quote(string(model.Additional)),
				Path:    typePath(id) + ".Additional",
			})
		}
		for i, prop := range model.Properties {
			if prop.Presence.Valid() {
				continue
			}
			vs = append(vs, Violation{
				Code:    "ir/unknown-presence-kind",
				Message: "property carries undeclared presence " + strconv.Quote(string(prop.Presence)),
				Path:    typePath(id) + ".Properties[" + strconv.Itoa(i) + "].Presence",
			})
		}
	}
	return vs
}
