// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema

import (
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/reftable"
	"github.com/dexpace/morphic/compilers/openapi/internal/statement"
)

// refBranchTarget returns the schema the chain of references from a
// composition branch ends on, as the bindings resolved it (Reference.Entry),
// and nil when the branch is no reference site (typed.Bindings.RefSite).
// Where the chain ends on no schema, or on a bare boolean schema, it returns
// one that is no schema object (stated).
func refBranchTarget(loweringContext lowering.Ctx, b *statement.Statement) *statement.Schema {
	if b.Ref == nil {
		return nil
	}
	if end, ok := runEnd(loweringContext, b); ok {
		return end.Schema
	}
	return loweringContext.Statements().Read(nil).Schema
}

// runEnd returns the statement the run of $refs from st ends on: the first
// schema it reaches writing no $ref, or writing keys beside one, which
// conjoins the two rather than naming one (statement.Schema.BesideRef),
// whether or not the chain the bindings follow past it resolves. It is false
// where a hop before it lands on no schema, and for a loop of $refs, which
// maxChainHops ends.
func runEnd(loweringContext lowering.Ctx, st *statement.Statement) (*statement.Statement, bool) {
	if referenceSite := st.Ref.Entry; st.Ref.Bound && referenceSite.Chain.Status == reftable.Resolved && referenceSite.Chain.Hops == 1 {
		return loweringContext.Statements().At(referenceSite.Chain.End), true // the one hop is the run
	}
	for range maxChainHops {
		next, _ := declaredOf(loweringContext, st)
		switch {
		case next == nil:
			return nil, false
		case next.Ref == nil:
			return next, true
		}
		st = next
	}
	return nil, false
}

// declaredOf returns the statement of the schema written where st's $ref
// lands, as the bindings resolved its first hop (typed.Bindings.DeclaredAt),
// and the position it lands at; nil and the zero position where it lands on
// none.
func declaredOf(loweringContext lowering.Ctx, st *statement.Statement) (*statement.Statement, reftable.DocumentPointer) {
	if st.Ref == nil || !st.Ref.Bound || st.Ref.Entry.Hop.Status != reftable.Resolved {
		return nil, reftable.DocumentPointer{}
	}
	return loweringContext.Statements().At(st.Ref.Entry.Hop.Target), st.Ref.Entry.Hop.Target
}

// declared returns the statement of the schema written where schemaStatement's $ref lands,
// as declaredOf reads it, and an Unwritten one where it lands on none, for a
// reader taking the statement of none (fallbackAnnotationsOf).
func declared(loweringContext lowering.Ctx, schemaStatement *statement.Statement) *statement.Statement {
	if st, _ := declaredOf(loweringContext, schemaStatement); st != nil {
		return st
	}
	return loweringContext.Statements().Read(nil)
}

// siteOf returns the position the bindings read schema at, and false for a schema
// they do not place, which the statement reads at the zero position.
func siteOf(schema *statement.Schema) (reftable.DocumentPointer, bool) {
	if schema == nil || schema.At == (reftable.DocumentPointer{}) {
		return reftable.DocumentPointer{}, false
	}
	return schema.At, true
}

// refKeyword returns the keyword st is a reference site under
// (typed.Bindings.RefKeyword), "" for one writing no reference.
func refKeyword(st *statement.Statement) string {
	if st.Ref == nil {
		return ""
	}
	return st.Ref.Keyword
}

// writesConjuncts reports whether schema says what an allOf says: it writes one,
// or it is the rest of an object writing a reference beside it, whose
// conjuncts are the reference and the rest (statement.Schema.BesideRef).
func writesConjuncts(schema *statement.Schema) bool {
	return schema != nil && (len(schema.AllOf) > 0 || schema.BesideRef != nil)
}

// conjunctsIn returns the statements of the conjuncts schema writes beside its own
// keywords, unflattened: the reference written beside them
// (writesConjuncts), then each schema its allOf lists.
func conjunctsIn(loweringContext lowering.Ctx, schema *statement.Schema) []*statement.Statement {
	if schema == nil {
		return nil
	}
	out := make([]*statement.Statement, 0, 1+len(schema.AllOf))
	if schema.BesideRef != nil {
		out = append(out, loweringContext.Statements().Read(schema.BesideRef))
	}
	for _, b := range schema.AllOf {
		out = append(out, loweringContext.Statements().Subschema(b))
	}
	return out
}

// referenceOf returns the statement of the $ref st writes: st where it writes
// one alone, the reference written beside its keywords where it writes both
// (statement.Schema.BesideRef), and nil where it writes none.
func referenceOf(loweringContext lowering.Ctx, st *statement.Statement) *statement.Statement {
	switch {
	case st.Ref != nil:
		return st
	case st.BesideRef != nil:
		return loweringContext.Statements().Read(st.BesideRef)
	default:
		return nil
	}
}

// allOfIn returns the schemas schema's allOf lists, nil for a nil schema and one
// writing none.
func allOfIn(schema *statement.Schema) []statement.Subschema {
	if schema == nil {
		return nil
	}
	return schema.AllOf
}

// refWritten reports whether schema writes `$ref`, as the parser's model holds it:
// a reference site under that keyword, not a $dynamicRef the bindings read as
// one (statement.Reference).
func refWritten(schema *statement.Schema) bool {
	return schema != nil && schema.Ref != nil && schema.Ref.Keyword == "$ref"
}
