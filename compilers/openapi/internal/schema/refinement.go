// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema

import (
	"encoding/json/jsontext"
	"slices"

	"github.com/dexpace/morphic/compilers/compile"
	"github.com/dexpace/morphic/compilers/openapi/internal/annotation"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/statement"
	"github.com/dexpace/morphic/ir"
)

// lowerPrincipal lowers the branch refinementValue uses where schema, written
// at pointer, refines. A $ref goes through Ref, as fillRefBranch's does,
// keeping the alias what is written beside it always has, in a frame of its
// own. An inline branch lowers by its own keywords, its node standing at
// pointer for the whole conjunction, finished by fin, its children and the
// reports on what it writes where they are written
// (docs/compilers/openapi.md#allof-refinement). A $ref the principal beat is
// still resolved: nothing else may lower what it names.
func lowerPrincipal(loweringContext lowering.Ctx, ts *compile.Types, depth int, schema *statement.Schema, pointer jsontext.Pointer, refinementValue refinement,
	fin finisher,
) (ir.TypeID, []ir.Diagnostic) {
	allOfBranch := refinementValue.principal
	resolveBeaten(loweringContext, ts, refinementValue)
	if allOfBranch.schema.Ref != nil {
		ref, diags := schemaRefHomed(loweringContext, ts, depth, allOfBranch.schema, allOfBranch.pointer, annotation.HomeOwnNode)
		return ref.Target, diags
	}
	// The node keeps the content vocabulary the branch writes where it holds
	// none, as the branch's own node would (recordUnplacedContent).
	var content, built []ir.Diagnostic
	st := stand{of: schema, at: pointer}
	// What the principal's build kept on the node itself is held back while
	// the position writes, the last of the principal's to leave and the first
	// kept again.
	outside := fin
	position := finisher(func(td ir.TypeDef, read keySet, held *principalKept) {
		built = keepBuiltAround(loweringContext, st, td, read, held, allOfBranch.schema.Schema, allOfBranch.pointer, outside)
	})
	fin = func(td ir.TypeDef, read keySet, held *principalKept) {
		keeping := unplacedContent(loweringContext, allOfBranch.schema.Schema, td, allOfBranch.pointer)
		content = keepAround(loweringContext, ts, st, td, read, held, allOfBranch.schema.Schema, allOfBranch.pointer, keeping, position)
	}
	ref, diags := lowerSchemaBody(loweringContext, ts, depth, allOfBranch.schema.Schema, allOfBranch.pointer, fin, st)
	return ref.Target, slices.Concat(missingArrayItemsConjoined(loweringContext, allOfBranch.schema.Schema, allOfBranch.pointer), diags, built, content)
}

// resolveBeaten resolves each $ref of refinementValue but its principal:
// nothing else may lower what a $ref the principal beat names.
func resolveBeaten(loweringContext lowering.Ctx, ts *compile.Types, refinementValue refinement) {
	for _, allOfBranch := range refinementValue.comp.branches {
		if allOfBranch.schema.Ref != nil && !refinementValue.principalIs(allOfBranch) {
			resolveSchemaRef(loweringContext, ts, allOfBranch.schema)
		}
	}
}

// refinedShape describes what the position the refinement of comp decides
// lowered to, for the reports of what it keeps and of conjuncts sharing no
// value, principal being the branch elected and standing the node the position
// owns, nil for none. Only a $ref principal makes it a use of a conjunct, named
// as comp's subject names them; electing none lowers by the keywords beside
// them (undecidedShape); an inline principal is named as written alone: the
// reference it collapses or expands to (referenceOutcomeKeywords), its node as
// its builder names it (held), else the node standing or the
// shared primitive.
func refinedShape(loweringContext lowering.Ctx, schema *statement.Schema, comp allOfComposition, principal *allOfBranch, standing ir.TypeDef,
	held *principalKept,
) string {
	switch {
	case principal == nil:
		return undecidedShape
	case principal.schema.Ref != nil && comp.subject() == "this schema":
		return "a use of one of its conjuncts"
	case principal.schema.Ref != nil:
		return "a use of one of its allOf conjuncts"
	}
	if electsOneOfPair(loweringContext, schema) {
		// The union passed over conjoins, so the elected one collapses no more
		// than one schema writing both does (pairedPrincipal).
		if _, ok := standing.(*ir.Union); ok {
			_, key, _ := unionBranches(loweringContext, schema, principal.schema.Schema)
			return "the Union of its " + key
		}
	} else if _, shape := referenceOutcomeKeywords(loweringContext, principal.schema.Schema, principal.pointer); shape != "" {
		return shape
	}
	if named := held.named(); named != "" {
		return named
	}
	if standing != nil {
		return nodeShape(standing.Kind())
	}
	return nodeShape(ir.KindPrimitive)
}

// lowerUndecided lowers schema, written at pointer, which writes no kind and no
// shape keyword, by the types its allOf, refining electing no entry, admits
// (undecidedTypes), as a type list naming them lowers, and to the top type
// where it admits none of a primitive's kinds alone. fin finishes the node
// standing at pointer after its census.
func lowerUndecided(loweringContext lowering.Ctx, ts *compile.Types, depth int, schema *statement.Schema, pointer jsontext.Pointer, dispatchValue dispatch,
	fin finisher, st stand,
) (ir.TypeID, []ir.Diagnostic) {
	types := undecidedTypes(loweringContext, schema, pointer)
	switch len(types) {
	case 0:
		return keepingUnread(loweringContext, ts, schema, pointer, dispatchValue, fin, st, func(finisher) (ir.TypeID, keySet, []ir.Diagnostic) {
			return ts.PrimID(ir.PrimitiveAny), 0, nil
		})
	case 1:
		return keepingUnread(loweringContext, ts, schema, pointer, dispatchValue, fin, st, func(fin finisher) (ir.TypeID, keySet, []ir.Diagnostic) {
			return lowerTyped(loweringContext, ts, depth, schema, pointer, types[0], fin, st)
		})
	default:
		return lowerUntypedUnion(loweringContext, ts, depth, schema, pointer, types, false, fin, st)
	}
}

// primitiveKinds are the kinds a primitive holds.
const primitiveKinds = kindString | kindNumber | kindBoolean

// undecidedTypes returns the types schema, written at pointer, lowers by where
// its allOf refines electing no entry: the kinds its conjuncts admit together,
// null aside (lowersTo), and where they state value sets of the members those
// share (sharedKinds), in one order whatever the order written. It returns none
// where those are none, as for sets sharing no member, or hold an object or an
// array, whose shape no entry was elected to give, every kind among them, or
// the kind-inference policy read them, as it reads only those: the position is
// then the top type.
func undecidedTypes(loweringContext lowering.Ctx, schema *statement.Schema, pointer jsontext.Pointer) []statement.JSONSchemaTypeName {
	refinement, refines := refinementOf(loweringContext, schema, pointer)
	if !refines || refinement.principal != nil {
		return nil
	}
	kinds, inferred := lowersTo(loweringContext, loweringContext.Statements().Read(schema))
	kinds &= sharedKinds(refinement.joint) &^ kindNull
	if kinds == 0 || kinds&^primitiveKinds != 0 || inferred {
		return nil
	}
	var out []statement.JSONSchemaTypeName
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
