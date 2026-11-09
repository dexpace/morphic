// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema

import (
	"encoding/json/jsontext"
	"slices"

	"github.com/speakeasy-api/openapi/jsonschema/oas3"

	"github.com/dexpace/morphic/compilers/compile"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/merge"
	"github.com/dexpace/morphic/compilers/openapi/internal/spelling"
	"github.com/dexpace/morphic/ir"
)

// typeValueSet types the value set r's principal lowered to by the conjunction,
// s being the schema the allOf is written in and pointer where s lowered: the
// primitive the conjuncts select narrowing every other, where each value is one
// of it (conjunctPrim). It runs after the census read the node's own keywords
// against the ValueType its own schema gave it, which this only narrows.
func typeValueSet(c lowering.Ctx, ts *compile.Types, s *oas3.Schema, r refinement, pointer jsontext.Pointer) {
	switch n := valueSetNode(c, ts, r, pointer).(type) {
	case *ir.Literal:
		n.ValueType = conjunctPrim(c, r, s, n.ValueType, func(p ir.PrimKind) bool { return merge.Admits(p, n.Value) })
	case *ir.Enum:
		n.ValueType = conjunctPrim(c, r, s, n.ValueType, func(p ir.PrimKind) bool { return enumMembersAre(n.Members, p) })
	default:
		// No value set: the principal is a $ref, or a schema of another kind.
	}
}

// valueSetNode returns the Literal or Enum r's principal owns, at its own
// pointer or, for the keywords beside the allOf, at pointer, and nil where it
// owns none there: a $ref branch's node is what it names, never its own.
func valueSetNode(c lowering.Ctx, ts *compile.Types, r refinement, pointer jsontext.Pointer) ir.TypeDef {
	at := pointer
	if r.principal != nil {
		at = r.principal.pointer
	}
	id, owned := ts.Lookup(c.IDs().Coordinate(at))
	if !owned {
		return nil
	}
	td, ok := ts.Node(id)
	if !ok || !(isKind(td, ir.KindLiteral) || isKind(td, ir.KindEnum)) {
		return nil
	}
	return td
}

// conjunctPrim returns, of own and the primitive each conjunct of r selects
// (declaredPrim), the one narrowing every other, and own where none does: two
// formats neither of which narrows the other say no one primitive. The conjuncts
// are s, each inline branch, and what decides what a $ref branch lands on
// (conjunctSchemas). own is "" for a value no primitive types, which none fits,
// so it stands.
func conjunctPrim(c lowering.Ctx, r refinement, s *oas3.Schema, own ir.PrimKind, fits func(ir.PrimKind) bool) ir.PrimKind {
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
func conjunctSchemas(c lowering.Ctx, r refinement, s *oas3.Schema) []*oas3.Schema {
	out := []*oas3.Schema{s}
	for _, e := range r.comp.branches {
		bs := e.schema.GetSchema()
		if isRefBranch(e.schema) {
			bs = DecidingSchema(c, c.RefScope().TargetSchema(e.schema, bs))
		}
		if bs != nil {
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
func declaredPrim(s *oas3.Schema, fits func(ir.PrimKind) bool) (ir.PrimKind, bool) {
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
