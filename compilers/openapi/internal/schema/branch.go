// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema

import (
	"github.com/dexpace/morphic/compilers/openapi/internal/bind"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/statement"
)

// refBranchTarget returns the schema the chain of references from b, an
// allOf entry's or a union branch's statement, ends on, as the bindings
// resolved it (Reference.Entry), and nil where b is no reference site
// (typed.Bindings.RefSite).
// Where the chain ends on no schema, or on a bare boolean schema, it returns
// one that is no schema object (stated).
func refBranchTarget(c lowering.Ctx, b *statement.Statement) *statement.Schema {
	if b.Ref == nil {
		return nil
	}
	if end, ok := runEnd(c, b); ok {
		return end.Schema
	}
	return c.Statements().Read(nil).Schema
}

// runEnd returns the statement the run of $refs from st ends on: the first
// schema it reaches writing no $ref, or writing keys beside one, which
// conjoins the two rather than naming one (statement.Schema.BesideRef),
// whether or not the chain the bindings follow past it resolves. It is false
// where a hop before it lands on no schema, and for a loop of $refs, which
// maxChainHops ends.
func runEnd(c lowering.Ctx, st *statement.Statement) (*statement.Statement, bool) {
	if e := st.Ref.Entry; st.Ref.Bound && e.Chain.Status == bind.Resolved && e.Chain.Hops == 1 {
		return c.Statements().At(e.Chain.End), true // the one hop is the run
	}
	for range maxChainHops {
		next, _ := declaredOf(c, st)
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
func declaredOf(c lowering.Ctx, st *statement.Statement) (*statement.Statement, bind.Position) {
	if st.Ref == nil || !st.Ref.Bound || st.Ref.Entry.Hop.Status != bind.Resolved {
		return nil, bind.Position{}
	}
	return c.Statements().At(st.Ref.Entry.Hop.Target), st.Ref.Entry.Hop.Target
}

// declared returns the statement of the schema written where x's $ref lands,
// as declaredOf reads it, and an Unwritten one where it lands on none, for a
// reader taking the statement of none (lentBy).
func declared(c lowering.Ctx, x *statement.Statement) *statement.Statement {
	if st, _ := declaredOf(c, x); st != nil {
		return st
	}
	return c.Statements().Read(nil)
}

// siteOf returns the position the bindings read s at, and false for a schema
// they do not place, which the statement reads at the zero position.
func siteOf(s *statement.Schema) (bind.Position, bool) {
	if s == nil || s.At == (bind.Position{}) {
		return bind.Position{}, false
	}
	return s.At, true
}

// refKeyword returns the keyword st is a reference site under
// (typed.Bindings.RefKeyword), "" for one writing no reference.
func refKeyword(st *statement.Statement) string {
	if st.Ref == nil {
		return ""
	}
	return st.Ref.Keyword
}

// writesConjuncts reports whether s says what an allOf says: it writes one,
// or it is the rest of an object writing a reference beside it, whose
// conjuncts are the reference and the rest (statement.Schema.BesideRef).
func writesConjuncts(s *statement.Schema) bool {
	return s != nil && (len(s.AllOf) > 0 || s.BesideRef != nil)
}

// conjunctsIn returns the statements of the conjuncts s writes beside its own
// keywords, unflattened: the reference written beside them
// (writesConjuncts), then each schema its allOf lists.
func conjunctsIn(c lowering.Ctx, s *statement.Schema) []*statement.Statement {
	if s == nil {
		return nil
	}
	out := make([]*statement.Statement, 0, 1+len(s.AllOf))
	if s.BesideRef != nil {
		out = append(out, c.Statements().Read(s.BesideRef))
	}
	for _, b := range s.AllOf {
		out = append(out, c.Statements().Sub(b))
	}
	return out
}

// referenceOf returns the statement of the $ref st writes: st where it writes
// one alone, the reference written beside its keywords where it writes both
// (statement.Schema.BesideRef), and nil where it writes none.
func referenceOf(c lowering.Ctx, st *statement.Statement) *statement.Statement {
	switch {
	case st.Ref != nil:
		return st
	case st.BesideRef != nil:
		return c.Statements().Read(st.BesideRef)
	default:
		return nil
	}
}

// allOfIn returns the schemas s's allOf lists, nil for a nil s and one
// writing none.
func allOfIn(s *statement.Schema) []statement.Sub {
	if s == nil {
		return nil
	}
	return s.AllOf
}

// refWritten reports whether s writes `$ref`, as the parser's model holds it:
// a reference site under that keyword, not a $dynamicRef the bindings read as
// one (statement.Reference).
func refWritten(s *statement.Schema) bool {
	return s != nil && s.Ref != nil && s.Ref.Keyword == "$ref"
}
