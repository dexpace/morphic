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

// lowerPrincipal lowers the branch r uses, at the pointer it is written at, so
// nothing is minted and each node stands at a pointer naming its schema. A $ref
// goes through Ref, as fillRefBranch's does, keeping the alias what is written
// beside it always has. An inline branch that reduces to a shared node leaves
// what it writes to the position, as a voice. Either takes a frame of its own, so
// a refinement counts two against maxSchemaDepth, a limit no real schema nears. A
// $ref the principal beat is still resolved: nothing else may lower what it names.
func lowerPrincipal(c lowering.Ctx, ts *compile.Types, depth int, r refinement, fin finisher) (ir.TypeID, []ir.Diagnostic) {
	e := r.principal
	resolveBeaten(c, ts, r)
	home := annotation.HomeCarrier
	if e.schema.Ref != nil {
		home = annotation.HomeOwnNode
	}
	ref, diags := schemaRefHomed(c, ts, depth, e.schema, e.pointer, home, fin)
	return ref.Target, diags
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
	fin finisher,
) (ir.TypeID, []ir.Diagnostic) {
	types := undecidedTypes(c, s, pointer)
	switch len(types) {
	case 0:
		return keepingUnread(c, ts, s, pointer, d, fin, func(finisher) (ir.TypeID, keySet, []ir.Diagnostic) {
			return ts.PrimID(ir.PrimAny), 0, nil
		})
	case 1:
		return keepingUnread(c, ts, s, pointer, d, fin, func(fin finisher) (ir.TypeID, keySet, []ir.Diagnostic) {
			return lowerTyped(c, ts, depth, s, pointer, types[0], fin)
		})
	default:
		return lowerUntypedUnion(c, ts, depth, s, pointer, types, false, fin)
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

// principalResidue returns the finisher keeping what binds a use on the node
// the principal of s, written at pointer, builds, where no carrier reads it,
// its report into into: a principal lowers with no home of its own
// (lowerPrincipal), so the branch keeps it as any declaration owning a node
// does. It is nil where s has no principal, or home is a carrier, which keeps
// it.
func principalResidue(c lowering.Ctx, s *statement.Schema, pointer jsontext.Pointer, home annotation.Home, into *[]ir.Diagnostic) finisher {
	r, refined := refinementOf(c, s, pointer)
	if home != annotation.HomeOwnNode || !refined || r.principal == nil || !stated(r.principal.schema.Schema) {
		return nil
	}
	principal := r.principal
	return func(td ir.TypeDef, _ keySet) {
		*into = recordResidue(c, td.Common(), principal.schema.Schema, principal.pointer)
	}
}
