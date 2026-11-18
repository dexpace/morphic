// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema

import (
	"encoding/json/jsontext"

	"github.com/dexpace/morphic/compilers/compile"
	"github.com/dexpace/morphic/compilers/openapi/internal/annotation"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/statement"
	"github.com/dexpace/morphic/ir"
)

// lowerPrincipal lowers the branch r uses where s, written at pointer, refines.
// A $ref goes through Ref, as fillRefBranch's does, keeping the alias what is
// written beside it always has, in a frame of its own. An inline branch lowers
// by its own keywords, its node standing at pointer for the whole conjunction,
// finished by fin, its children and the reports on what it writes where they are
// written (docs/compilers/openapi.md#allof-refinement). A $ref the principal beat
// is still resolved: nothing else may lower what it names.
func lowerPrincipal(c lowering.Ctx, ts *compile.Types, depth int, s *statement.Schema, pointer jsontext.Pointer, r refinement,
	fin finisher,
) (ir.TypeID, []ir.Diagnostic) {
	e := r.principal
	resolveBeaten(c, ts, r)
	if e.schema.Ref != nil {
		ref, diags := schemaRefHomed(c, ts, depth, e.schema, e.pointer, annotation.HomeOwnNode)
		return ref.Target, diags
	}
	ref, diags := lowerSchemaBody(c, ts, depth, e.schema.Schema, e.pointer, fin, stand{of: s, at: pointer})
	return ref.Target, append(missingArrayItemsConjoined(c, e.schema.Schema, e.pointer), diags...)
}

// resolveBeaten resolves each $ref of r but its principal: nothing else may
// lower what a $ref the principal beat names.
func resolveBeaten(c lowering.Ctx, ts *compile.Types, r refinement) {
	for _, e := range r.comp.branches {
		if e.schema.Ref != nil && !r.principalIs(e) {
			resolveSchemaRef(c, ts, e.schema)
		}
	}
}

// refinedShape describes what a refinement lowers to, for the diagnostic
// recordUnhomedAt reports, its conjuncts named as comp's subject names them.
func refinedShape(comp allOfComposition) string {
	if comp.subject() == "this schema" {
		return "a use of one of its conjuncts"
	}
	return "a use of one of its allOf conjuncts"
}

// lowerUndecided lowers s, written at pointer, which writes no kind and no
// shape keyword, by the types its allOf, refining electing no entry, admits
// (undecidedTypes), as a type list naming them lowers, and to the top type
// where it admits none of a primitive's kinds alone. fin finishes the node
// standing at pointer after its census.
func lowerUndecided(c lowering.Ctx, ts *compile.Types, depth int, s *statement.Schema, pointer jsontext.Pointer, d dispatch,
	fin finisher, st stand,
) (ir.TypeID, []ir.Diagnostic) {
	types := undecidedTypes(c, s, pointer)
	switch len(types) {
	case 0:
		return keepingUnread(c, ts, s, pointer, d, fin, st, func(finisher) (ir.TypeID, keySet, []ir.Diagnostic) {
			return ts.PrimID(ir.PrimAny), 0, nil
		})
	case 1:
		return keepingUnread(c, ts, s, pointer, d, fin, st, func(fin finisher) (ir.TypeID, keySet, []ir.Diagnostic) {
			return lowerTyped(c, ts, depth, s, pointer, types[0], fin, st)
		})
	default:
		return lowerUntypedUnion(c, ts, depth, s, pointer, types, false, fin, st)
	}
}

// primitiveKinds are the kinds a primitive holds.
const primitiveKinds = kindString | kindNumber | kindBoolean

// undecidedTypes returns the types s, written at pointer, lowers by where its
// allOf refines electing no entry: the kinds its conjuncts admit together, null
// aside (lowersTo), and where they state value sets of the members those share
// (sharedKinds), in one order whatever the order written. It returns none
// where those are none, as for sets sharing no member, or hold an object or an
// array, whose shape no entry was elected to give, every kind among them, or
// the kind-inference policy read them, as it reads only those: the position is
// then the top type.
func undecidedTypes(c lowering.Ctx, s *statement.Schema, pointer jsontext.Pointer) []statement.Kind {
	r, refines := refinementOf(c, s, pointer)
	if !refines || r.principal != nil {
		return nil
	}
	kinds, inferred := lowersTo(c, c.Statements().Read(s))
	kinds &= sharedKinds(r.joint) &^ kindNull
	if kinds == 0 || kinds&^primitiveKinds != 0 || inferred {
		return nil
	}
	var out []statement.Kind
	if kinds&kindString != 0 {
		out = append(out, statement.KindString)
	}
	switch kinds & kindNumber {
	case 0:
	case kindInteger:
		out = append(out, statement.KindInteger)
	default:
		out = append(out, statement.KindNumber)
	}
	if kinds&kindBoolean != 0 {
		out = append(out, statement.KindBoolean)
	}
	return out
}

// sharedKinds returns the kinds of what joint, what the conjuncts of a
// refinement admit together as its election reads them, holds: where they
// state value sets, the kinds of the members every set shares, as an instance
// meets each set only as a member of it; else its kinds.
func sharedKinds(joint admitted) kindSet {
	if !joint.fixed {
		return joint.kinds
	}
	var out kindSet
	for _, kind := range joint.members {
		out |= kind
	}
	return out
}

// undecidedShape describes what a refinement electing no entry lowers to, for
// the report of what an entry of it keeps.
const undecidedShape = "what the keywords beside it say, as no entry of it is the one it lowers from " +
	"(where they name no kind, the primitive kinds its entries admit together, else the top type)"
