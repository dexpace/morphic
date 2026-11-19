// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema

import (
	"encoding/json/jsontext"

	"github.com/dexpace/morphic/compilers/openapi/internal/annotation"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/statement"
	"github.com/dexpace/morphic/ir"
)

// stand is where the node a schema lowers to stands where that is not where the
// schema is written: at, the pointer of of, the position whose allOf the schema
// is the inline principal of, as the node the position owns stands for the
// whole conjunction (docs/compilers/openapi.md#allof-refinement). The zero
// stand is the schema's own. It is a parameter, never read off the schema or
// the context, since a $ref to the entry lowers the same schema at its own
// pointer.
type stand struct {
	of *statement.Schema
	at jsontext.Pointer
	// held says the node is a type list's variant whose principal the position
	// keeps whole under its entry's key (heldWhole), so it keeps none of its
	// bounds verbatim.
	held bool
}

// position returns the schema a union schema writes is elected at
// (docs/compilers/openapi.md#competing-keywords): the position schema is the
// principal of, whose branches declare shape against all it conjoins, else schema.
func (st stand) position(schema *statement.Schema) *statement.Schema {
	if st.of == nil {
		return schema
	}
	return st.of
}

// pointer returns where the node of a schema written at written stands.
func (st stand) pointer(written jsontext.Pointer) jsontext.Pointer {
	if st.of == nil {
		return written
	}
	return st.at
}

// foldedBranches returns what the node of schema, written at written, reads beside schema: the
// foldedBranches of its position (positionFoldedBranches), where it stands at another, that
// position's own keywords and each folded branch of its allOf but schema, whose keywords the
// node reads as its own.
func (st stand) foldedBranches(loweringContext lowering.Ctx, schema *statement.Schema, written jsontext.Pointer) []annotation.FoldedAllOfBranch {
	if st.of == nil {
		return positionFoldedBranches(loweringContext, schema, written, ir.TypeRef{})
	}
	foldedBranches := positionFoldedBranches(loweringContext, st.of, st.at, ir.TypeRef{})
	out := make([]annotation.FoldedAllOfBranch, 0, len(foldedBranches)+1)
	beside := foldedBranchAt(loweringContext, st.of, st.at)
	beside.Beside = true
	out = append(out, beside)
	for _, foldedAllOfBranch := range foldedBranches {
		if foldedAllOfBranch.Node != schema || foldedAllOfBranch.At != written {
			out = append(out, foldedAllOfBranch)
		}
	}
	return out
}

// takes returns the kinds whose value bounds the node of schema takes (boundKinds):
// those its position admits.
func (st stand) takes(loweringContext lowering.Ctx, schema *statement.Schema) kindSet {
	if st.of == nil {
		return boundKinds(loweringContext, schema)
	}
	return boundKinds(loweringContext, st.of)
}

// standsAtPosition reports whether refinementValue's principal is inline and built the node
// the position written at pointer owns, having lowered to target: its census ran
// on that node (keepsAtOutcome), and what that node holds is the principal's
// (ownHomes).
func standsAtPosition(loweringContext lowering.Ctx, refinementValue refinement, target ir.TypeRef, pointer jsontext.Pointer) bool {
	return refinementValue.principal != nil && refinementValue.principal.schema.Ref == nil && LoweredToOwnNode(loweringContext, pointer, target)
}
