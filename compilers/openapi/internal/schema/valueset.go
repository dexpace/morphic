// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema

import (
	"encoding/json/jsontext"
	"slices"

	"github.com/dexpace/morphic/compilers/compile"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/merge"
	"github.com/dexpace/morphic/compilers/openapi/internal/statement"
	"github.com/dexpace/morphic/compilers/openapi/internal/typeformat"
	"github.com/dexpace/morphic/compilers/openapi/internal/value"
	"github.com/dexpace/morphic/ir"
)

// valueSetSite is where a value set lowers: set, the schema writing it at
// written, and the node it lowers to, standing where at says: at the position
// for a refinement's inline principal, whose node is the position's while its
// members and reports stay where they are written
// (docs/compilers/openapi.md#allof-refinement). member marks a member of an
// enum lowered as a union of literals, which no conjunct types. finish
// finishes the node, nil for a member's.
type valueSetSite struct {
	set     *statement.Schema
	finish  finisher
	at      stand
	written jsontext.Pointer
	member  bool
}

// standingValueSet is the site of the value set schema, written at pointer,
// lowers to, its node standing where st says.
func standingValueSet(schema *statement.Schema, pointer jsontext.Pointer, st stand) valueSetSite {
	return valueSetSite{set: schema, written: pointer, at: st}
}

// finishedBy returns vs, its node finished by fin.
func (vs valueSetSite) finishedBy(fin finisher) valueSetSite {
	vs.finish = fin
	return vs
}

// memberSite is the site of a member of vs's set lowered as a Literal of its
// own at at, typed by its value and its set's schema alone.
func (vs valueSetSite) memberSite(at jsontext.Pointer) valueSetSite {
	return valueSetSite{set: vs.set, written: at, member: true}
}

// pointer returns where the node vs lowers to stands.
func (vs valueSetSite) pointer() jsontext.Pointer {
	return vs.at.pointer(vs.written)
}

// valueType types the node vs lowers to: own, what its set's schema selects,
// narrowed by every conjunct where its position refines (conjunctPrimitive),
// fits saying which primitives the values are of. The position is the stand's,
// else the set's own; a member has none.
func (vs valueSetSite) valueType(loweringContext lowering.Ctx, own ir.PrimitiveKind, fits func(ir.PrimitiveKind) bool) ir.PrimitiveKind {
	if vs.member {
		return own
	}
	position := vs.set
	if vs.at.of != nil {
		position = vs.at.of
	}
	refinement, refines := refinementOf(loweringContext, position, vs.pointer())
	if !refines {
		return own
	}
	return conjunctPrimitive(loweringContext, refinement, position, own, fits)
}

// valueSetPrincipal returns the inline principal of refinementValue that lowers
// to a value set, an enum, a const or a type of null alone, whose node stands
// at the position, and false where refinementValue's principal is none, a $ref,
// or another schema.
func valueSetPrincipal(refinementValue refinement) (allOfBranch, bool) {
	allOfBranchValue := refinementValue.principal
	if allOfBranchValue == nil || allOfBranchValue.schema.Ref != nil {
		return allOfBranch{}, false
	}
	if won := principalDispatch(*allOfBranchValue).won; won == "const" || won == "enum" || won == "" && nullAlone(allOfBranchValue.schema.Schema) {
		return *allOfBranchValue, true
	}
	return allOfBranch{}, false
}

// nullAlone reports whether schema writes a type naming null alone, which
// lowers to the value set of null (lowerNullAlone).
func nullAlone(schema *statement.Schema) bool {
	return writesType(schema) && len(effectiveTypes(schema)) == 0
}

// electedSet names the keyword of the value set refinementValue, a refinement
// of schema whose conjuncts share no value, is elected for: its inline
// principal's, else the one written beside the allOf, an enum or a const either
// way (setElected).
func electedSet(refinementValue refinement, schema *statement.Schema) string {
	if allOfBranch, ok := valueSetPrincipal(refinementValue); ok {
		return principalDispatch(allOfBranch).won
	}
	return dispatchOf(schema).besideAllOf().won
}

// principalDispatch is the election of the inline principal allOfBranchValue's
// own keywords: a flattened wrapper's allOf is no rival of its value set, its
// entries being conjuncts of the position (allOfBranches).
func principalDispatch(allOfBranchValue allOfBranch) dispatch {
	dispatchValue := dispatchOf(allOfBranchValue.schema.Schema)
	if allOfBranchValue.wrapper {
		return dispatchValue.besideAllOf()
	}
	return dispatchValue
}

// lowerPrincipalValueSet lowers the value set allOfBranchValue, r's inline
// principal, writes at pointer, the position schema is written at, so one node
// stands for the position whichever conjunct decides it
// (docs/compilers/openapi.md#allof-refinement). What allOfBranchValue writes
// beside its set that the node has no field for is kept on it, each keyword at
// its own pointer, as on any node a schema lowers to, before fin finishes it.
func lowerPrincipalValueSet(loweringContext lowering.Ctx, ts *compile.Types, schema *statement.Schema, allOfBranchValue allOfBranch, pointer jsontext.Pointer,
	fin finisher,
) (ir.TypeID, []ir.Diagnostic) {
	bs := allOfBranchValue.schema.Schema
	dispatch := principalDispatch(allOfBranchValue)
	var kept, own finished
	st := stand{of: schema, at: pointer}
	// The position's own keywords type the value set as a conjunct does, and
	// its census keeps what they leave before the principal's keeps its own.
	outside := own.on(func(td ir.TypeDef) []ir.Diagnostic {
		return keepUnread(loweringContext, ts, schema, pointer, td, dispatchOf(schema).besideAllOf(), valueSetReads(schema, td), stand{}, nil, nil)
	}).then(fin)
	var heldBack []ir.Diagnostic
	position := finisher(func(td ir.TypeDef, read keySet, held *principalKept) {
		heldBack = keepBuiltAround(loweringContext, st, td, read, held, bs, allOfBranchValue.pointer, outside)
	})
	site := valueSetSite{set: bs, written: allOfBranchValue.pointer, at: st, finish: func(td ir.TypeDef, read keySet, held *principalKept) {
		kept.diags = keepUnread(loweringContext, ts, bs, allOfBranchValue.pointer, td, dispatch, read, st, held, position)
	}}
	var id ir.TypeID
	var built []ir.Diagnostic
	switch dispatch.won {
	case "const":
		id, built = lowerConst(loweringContext, ts, site)
	case "enum":
		id, built = lowerEnum(loweringContext, ts, site)
	default:
		id = lowerNullAlone(loweringContext, ts, site)
	}
	// The value set is the position's node, which its build censuses.
	return id, slices.Concat(built, missingArrayItems(loweringContext, bs, allOfBranchValue.pointer), kept.diags, own.diags, heldBack)
}

// setType is the ValueType of the Enum or Literal a refinement's value set
// lowers to, ok false where it lowers to no such node (refinedValueType).
type setType struct {
	primitive ir.PrimitiveKind
	ok        bool
}

// refinedValueType returns the ValueType of the Enum or Literal the refinement
// refinementValue of schema, written at pointer, lowers to, as its build types
// it, and none where it lowers to no such node: its principal is a $ref or no
// value set, or its set lowers to a union of literals or the top type. It reads
// the source alone, so a reader of the position asks no registry.
func refinedValueType(loweringContext lowering.Ctx, refinementValue refinement, schema *statement.Schema, pointer jsontext.Pointer) setType {
	site := standingValueSet(schema, pointer, stand{})
	dispatch := dispatchOf(schema).besideAllOf()
	if allOfBranch, ok := valueSetPrincipal(refinementValue); ok {
		site = valueSetSite{set: allOfBranch.schema.Schema, written: allOfBranch.pointer, at: stand{of: schema, at: pointer}}
		dispatch = principalDispatch(allOfBranch)
	} else if refinementValue.principal != nil {
		return setType{}
	}
	switch dispatch.won {
	case "enum":
		return enumSetType(loweringContext, site)
	case "const":
		return constSetType(loweringContext, site)
	default:
		return setType{primitive: site.enumType(loweringContext, admitsEvery, ir.PrimitiveAny), ok: nullAlone(site.set)}
	}
}

// enumSetType is refinedValueType for an enum: the Enum's ValueType, as
// lowerEnum gives it, and none where the enum lowers to anything else.
func enumSetType(loweringContext lowering.Ctx, site valueSetSite) setType {
	written := site.set.Enum()
	if !loweringContext.Limits.MaxEnumMembers.Admits(len(written)) {
		return setType{}
	}
	kept := slices.DeleteFunc(readEnum(written), enumEntry.isNull)
	primitive, ok := enumMemberPrimitive(kept)
	if !ok {
		return setType{primitive: site.enumType(loweringContext, admitsEvery, primitive)}
	}
	return setType{primitive: site.enumType(loweringContext, func(primitiveKind ir.PrimitiveKind) bool { return entriesAre(kept, primitiveKind) }, primitive), ok: true}
}

// constSetType is refinedValueType for a const: the Literal's ValueType, or
// the ValueType of the Enum with no member a null lowers to, and none where
// the value converts to none.
func constSetType(loweringContext lowering.Ctx, site valueSetSite) setType {
	val, err := value.FromNode(site.set.Const())
	if err == nil && val.Kind == ir.ValueNull {
		return setType{primitive: site.enumType(loweringContext, admitsEvery, ir.PrimitiveAny), ok: true}
	}
	return setType{primitive: site.literalType(loweringContext, val), ok: err == nil}
}

// enumType is the ValueType an Enum of members classified as memberPrimitive
// takes at vs (enumValueType), admits saying whether every member is a value of
// a primitive, narrowed where its position refines.
func (vs valueSetSite) enumType(loweringContext lowering.Ctx, admits func(ir.PrimitiveKind) bool, memberPrimitive ir.PrimitiveKind) ir.PrimitiveKind {
	return vs.valueType(loweringContext, enumValueType(vs.set, admits, memberPrimitive), admits)
}

// admitsEvery is what enumType asks of an Enum with no member: every value of
// any primitive is one of its members, as there is none.
func admitsEvery(ir.PrimitiveKind) bool { return true }

// literalType is the ValueType a Literal of val takes at vs
// (literalValueType), narrowed where its position refines.
func (vs valueSetSite) literalType(loweringContext lowering.Ctx, val ir.Value) ir.PrimitiveKind {
	return vs.valueType(loweringContext, literalValueType(vs.set, val), func(primitiveKind ir.PrimitiveKind) bool { return merge.Admits(primitiveKind, val) })
}

// valueTypeHoldsFormat reports whether a value set typed primitive holds the
// format schema pairs with its type: primitive is that format's primitive or a
// narrower one.
func valueTypeHoldsFormat(primitive ir.PrimitiveKind, schema *statement.Schema) bool {
	paired, ok := formatPrimitive(schema)
	return ok && narrowsOrIs(primitive, paired)
}

// conjunctPrimitive returns, of own and the primitive each conjunct of
// refinementValue selects (declaredPrimitive), the one narrowing every other,
// and own where none does: two formats neither of which narrows the other say
// no one primitive. The conjuncts are schema, each inline branch, and what
// decides what a $ref branch lands on (conjunctSchemas). own is "" for a value
// no primitive types, which none fits, so it stands.
func conjunctPrimitive(loweringContext lowering.Ctx, refinementValue refinement, schema *statement.Schema, own ir.PrimitiveKind, fits func(ir.PrimitiveKind) bool) ir.PrimitiveKind {
	primitives := []ir.PrimitiveKind{own}
	for _, cs := range conjunctSchemas(loweringContext, refinementValue, schema) {
		if primitive, ok := declaredPrimitive(cs, fits); ok {
			primitives = append(primitives, primitive)
		}
	}
	for _, primitiveKind := range primitives {
		if !slices.ContainsFunc(primitives, func(irPrimitiveKind ir.PrimitiveKind) bool { return !narrowsOrIs(primitiveKind, irPrimitiveKind) }) {
			return primitiveKind
		}
	}
	return own
}

// conjunctSchemas returns the schemas whose keywords refinementValue's
// conjuncts write: schema, the keywords beside the allOf, then each branch, a
// $ref's being what decides what it lands on, read past a chain of $refs and
// refinements (DecidingSchema).
func conjunctSchemas(loweringContext lowering.Ctx, refinementValue refinement, schema *statement.Schema) []*statement.Schema {
	out := []*statement.Schema{schema}
	for _, allOfBranch := range refinementValue.comp.branches {
		bs := allOfBranch.schema.Schema
		if allOfBranch.schema.Ref != nil {
			bs = DecidingSchema(loweringContext, refBranchTarget(loweringContext, allOfBranch.schema))
		}
		if stated(bs) {
			out = append(out, bs)
		}
	}
	return out
}

// narrowsOrIs reports whether primitiveKind is irPrimitiveKind or narrower.
func narrowsOrIs(primitiveKind, irPrimitiveKind ir.PrimitiveKind) bool {
	rel := typeformat.Relate(primitiveKind, irPrimitiveKind)
	return rel == typeformat.Same || rel == typeformat.ANarrower
}

// declaredPrimitive returns the primitive schema's type and format select, else
// its type alone, where fits says its values are of it, as a value set's own
// schema types it (literalValueType), and false where schema selects none they
// fit.
func declaredPrimitive(schema *statement.Schema, fits func(ir.PrimitiveKind) bool) (ir.PrimitiveKind, bool) {
	if primitive, paired := formatPrimitive(schema); paired && fits(primitive) {
		return primitive, true
	}
	if types := effectiveTypes(schema); len(types) == 1 {
		if primitive, known := typeformat.Lookup(string(types[0])); known && fits(primitive) {
			return primitive, true
		}
	}
	return "", false
}
