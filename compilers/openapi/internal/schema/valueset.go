// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema

import (
	"encoding/json/jsontext"
	"slices"

	"github.com/dexpace/morphic/compilers/compile"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/merge"
	"github.com/dexpace/morphic/compilers/openapi/internal/spelling"
	"github.com/dexpace/morphic/compilers/openapi/internal/statement"
	"github.com/dexpace/morphic/compilers/openapi/internal/value"
	"github.com/dexpace/morphic/ir"
)

// valueSetSite is where a value set lowers: set, the schema writing it at
// written, and the node it lowers to, standing at pointer, where position is
// written. The pointers part only for a refinement's inline principal, whose
// node is the position's while its members and reports stay where they are
// written (docs/compilers/openapi.md#allof-refinement). position is nil for a
// member of an enum lowered as a union of literals, which no conjunct types.
// finish finishes the node at pointer, nil for a member's.
type valueSetSite struct {
	set      *statement.Schema
	written  jsontext.Pointer
	position *statement.Schema
	pointer  jsontext.Pointer
	finish   finisher
}

// ownValueSet is the site of the value set s, written at pointer, lowers to.
func ownValueSet(s *statement.Schema, pointer jsontext.Pointer) valueSetSite {
	return valueSetSite{set: s, written: pointer, position: s, pointer: pointer}
}

// finishedBy returns vs, its node finished by fin.
func (vs valueSetSite) finishedBy(fin finisher) valueSetSite {
	vs.finish = fin
	return vs
}

// memberSite is the site of a member of vs's set lowered as a Literal of its
// own at at, typed by its value and its set's schema alone.
func (vs valueSetSite) memberSite(at jsontext.Pointer) valueSetSite {
	return valueSetSite{set: vs.set, written: at, pointer: at}
}

// valueType types the node vs lowers to: own, what its set's schema selects,
// narrowed by every conjunct where its position refines (conjunctPrim), fits
// saying which primitives the values are of.
func (vs valueSetSite) valueType(c lowering.Ctx, own ir.PrimKind, fits func(ir.PrimKind) bool) ir.PrimKind {
	if vs.position == nil {
		return own
	}
	r, refines := refinementOf(c, vs.position, vs.pointer)
	if !refines {
		return own
	}
	return conjunctPrim(c, r, vs.position, own, fits)
}

// valueSetPrincipal returns the inline principal of r that lowers to a value
// set, an enum, a const or a type of null alone, whose node stands at the
// position, and false where r's principal is none, a $ref, or another schema.
func valueSetPrincipal(r refinement) (allOfBranch, bool) {
	e := r.principal
	if e == nil || e.schema.Ref != nil {
		return allOfBranch{}, false
	}
	if won := principalDispatch(*e).won; won == "const" || won == "enum" || won == "" && nullAlone(e.schema.Schema) {
		return *e, true
	}
	return allOfBranch{}, false
}

// nullAlone reports whether s writes a type naming null alone, which lowers
// to the value set of null (lowerNullAlone).
func nullAlone(s *statement.Schema) bool {
	return writesType(s) && len(effectiveTypes(s)) == 0
}

// electedSet names the keyword of the value set r, a refinement of s whose
// conjuncts share no value, is elected for: its inline principal's, else the
// one written beside the allOf, an enum or a const either way (setElected).
func electedSet(r refinement, s *statement.Schema) string {
	if e, ok := valueSetPrincipal(r); ok {
		return principalDispatch(e).won
	}
	return dispatchOf(s).besideAllOf().won
}

// principalDispatch is the election of the inline principal e's own keywords:
// a flattened wrapper's allOf is no rival of its value set, its entries being
// conjuncts of the position (allOfBranches).
func principalDispatch(e allOfBranch) dispatch {
	d := dispatchOf(e.schema.Schema)
	if e.wrapper {
		return d.besideAllOf()
	}
	return d
}

// lowerPrincipalValueSet lowers the value set e, r's inline principal, writes
// at pointer, the position s is written at, so one node stands for the
// position whichever conjunct decides it (docs/compilers/openapi.md#allof-refinement).
// What e writes beside its set that the node has no field for is kept on it,
// each keyword at its own pointer, as on any node a schema lowers to, before
// fin finishes it.
func lowerPrincipalValueSet(c lowering.Ctx, ts *compile.Types, s *statement.Schema, e allOfBranch, pointer jsontext.Pointer,
	fin finisher,
) (ir.TypeID, []ir.Diagnostic) {
	bs := e.schema.Schema
	d := principalDispatch(e)
	var kept, own finished
	site := valueSetSite{set: bs, written: e.pointer, position: s, pointer: pointer, finish: kept.onRead(func(td ir.TypeDef, read keySet) []ir.Diagnostic {
		return keepUnread(c, ts, bs, e.pointer, td, d, read)
	}).then(own.on(func(td ir.TypeDef) []ir.Diagnostic {
		// The position's own keywords type the value set as a conjunct does.
		return keepUnread(c, ts, s, pointer, td, dispatchOf(s).besideAllOf(), valueSetReads(s, td))
	})).then(fin)}
	var id ir.TypeID
	var built []ir.Diagnostic
	switch d.won {
	case "const":
		id, built = lowerConst(c, ts, site)
	case "enum":
		id, built = lowerEnum(c, ts, site)
	default:
		id = lowerNullAlone(c, ts, site)
	}
	// The value set is the position's node, which its build censuses.
	return id, slices.Concat(built, missingArrayItems(c, bs, e.pointer), kept.diags, own.diags)
}

// setType is the ValueType of the Enum or Literal a refinement's value set
// lowers to, ok false where it lowers to no such node (refinedValueType).
type setType struct {
	prim ir.PrimKind
	ok   bool
}

// refinedValueType returns the ValueType of the Enum or Literal the refinement
// r of s, written at pointer, lowers to, as its build types it, and none where
// it lowers to no such node: its principal is a $ref or no value set, or its
// set lowers to a union of literals or the top type. It reads the source
// alone, so a reader of the position asks no registry.
func refinedValueType(c lowering.Ctx, r refinement, s *statement.Schema, pointer jsontext.Pointer) setType {
	site := ownValueSet(s, pointer)
	d := dispatchOf(s).besideAllOf()
	if e, ok := valueSetPrincipal(r); ok {
		site.set, site.written, d = e.schema.Schema, e.pointer, principalDispatch(e)
	} else if r.principal != nil {
		return setType{}
	}
	switch d.won {
	case "enum":
		return enumSetType(c, site)
	case "const":
		return constSetType(c, site)
	default:
		return setType{prim: site.enumType(c, admitsEvery, ir.PrimAny), ok: nullAlone(site.set)}
	}
}

// enumSetType is refinedValueType for an enum: the Enum's ValueType, as
// lowerEnum gives it, and none where the enum lowers to anything else.
func enumSetType(c lowering.Ctx, site valueSetSite) setType {
	written := enumIn(site.set)
	if !c.Limits.MaxEnumMembers.Admits(len(written)) {
		return setType{}
	}
	kept := slices.DeleteFunc(readEnum(written), enumEntry.isNull)
	prim, ok := enumMemberPrim(kept)
	if !ok {
		return setType{prim: site.enumType(c, admitsEvery, prim)}
	}
	return setType{prim: site.enumType(c, func(p ir.PrimKind) bool { return entriesAre(kept, p) }, prim), ok: true}
}

// constSetType is refinedValueType for a const: the Literal's ValueType, or
// the ValueType of the Enum with no member a null lowers to, and none where
// the value converts to none.
func constSetType(c lowering.Ctx, site valueSetSite) setType {
	val, err := value.FromNode(constIn(site.set))
	if err == nil && val.Kind == ir.ValueNull {
		return setType{prim: site.enumType(c, admitsEvery, ir.PrimAny), ok: true}
	}
	return setType{prim: site.literalType(c, val), ok: err == nil}
}

// enumType is the ValueType an Enum of members classified as memberPrim takes
// at vs (enumValueType), admits saying whether every member is a value of a
// primitive, narrowed where its position refines.
func (vs valueSetSite) enumType(c lowering.Ctx, admits func(ir.PrimKind) bool, memberPrim ir.PrimKind) ir.PrimKind {
	return vs.valueType(c, enumValueType(vs.set, admits, memberPrim), admits)
}

// admitsEvery is what enumType asks of an Enum with no member: every value of
// any primitive is one of its members, as there is none.
func admitsEvery(ir.PrimKind) bool { return true }

// literalType is the ValueType a Literal of val takes at vs
// (literalValueType), narrowed where its position refines.
func (vs valueSetSite) literalType(c lowering.Ctx, val ir.Value) ir.PrimKind {
	return vs.valueType(c, literalValueType(vs.set, val), func(p ir.PrimKind) bool { return merge.Admits(p, val) })
}

// valueTypeHoldsFormat reports whether a value set typed prim holds the format
// s pairs with its type: prim is that format's primitive or a narrower one.
func valueTypeHoldsFormat(prim ir.PrimKind, s *statement.Schema) bool {
	paired, ok := formatPrimitive(s)
	return ok && narrowsOrIs(prim, paired)
}

// conjunctPrim returns, of own and the primitive each conjunct of r selects
// (declaredPrim), the one narrowing every other, and own where none does: two
// formats neither of which narrows the other say no one primitive. The conjuncts
// are s, each inline branch, and what decides what a $ref branch lands on
// (conjunctSchemas). own is "" for a value no primitive types, which none fits,
// so it stands.
func conjunctPrim(c lowering.Ctx, r refinement, s *statement.Schema, own ir.PrimKind, fits func(ir.PrimKind) bool) ir.PrimKind {
	prims := []ir.PrimKind{own}
	for _, cs := range conjunctSchemas(c, r, s) {
		if prim, ok := declaredPrim(cs, fits); ok {
			prims = append(prims, prim)
		}
	}
	for _, p := range prims {
		if !slices.ContainsFunc(prims, func(o ir.PrimKind) bool { return !narrowsOrIs(p, o) }) {
			return p
		}
	}
	return own
}

// conjunctSchemas returns the schemas whose keywords r's conjuncts write: s, the
// keywords beside the allOf, then each branch, a $ref's being what decides what
// it lands on, read past a chain of $refs and refinements (DecidingSchema).
func conjunctSchemas(c lowering.Ctx, r refinement, s *statement.Schema) []*statement.Schema {
	out := []*statement.Schema{s}
	for _, e := range r.comp.branches {
		bs := e.schema.Schema
		if e.schema.Ref != nil {
			bs = DecidingSchema(c, refBranchTarget(c, e.schema))
		}
		if stated(bs) {
			out = append(out, bs)
		}
	}
	return out
}

// narrowsOrIs reports whether p is o or narrower.
func narrowsOrIs(p, o ir.PrimKind) bool {
	rel := spelling.Relate(p, o)
	return rel == spelling.Same || rel == spelling.ANarrower
}

// declaredPrim returns the primitive s's type and format select, else its type
// alone, where fits says its values are of it, as a value set's own schema types
// it (literalValueType), and false where s selects none they fit.
func declaredPrim(s *statement.Schema, fits func(ir.PrimKind) bool) (ir.PrimKind, bool) {
	if prim, paired := formatPrimitive(s); paired && fits(prim) {
		return prim, true
	}
	if types := effectiveTypes(s); len(types) == 1 {
		if prim, known := spelling.Lookup(string(types[0])); known && fits(prim) {
			return prim, true
		}
	}
	return "", false
}
