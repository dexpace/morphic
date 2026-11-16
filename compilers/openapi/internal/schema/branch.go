// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema

import (
	"github.com/dexpace/morphic/compilers/openapi/internal/bind"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/statement"
)

// refBranchTarget returns the schema the chain of references from a
// composition branch ends on, as the bindings resolved it (Reference.Entry),
// and nil when the branch is no reference site (typed.Bindings.RefSite).
// Where the chain ends on no schema, or on a bare boolean schema, it returns
// one statement.Parsed reads no schema object of.
func refBranchTarget(c lowering.Ctx, b *statement.Statement) *statement.Schema {
	if b.Ref == nil {
		return nil
	}
	if e := b.Ref.Entry; b.Ref.Bound && e.Chain.Status == bind.Resolved {
		return c.Statements().At(e.Chain.End).Schema
	}
	return c.Statements().Read(nil).Schema
}

// declaredOf returns the statement of the schema written where st's $ref
// lands, as the bindings resolved its first hop (typed.Bindings.Declared), and
// nil where it lands on none.
func declaredOf(c lowering.Ctx, st *statement.Statement) *statement.Statement {
	if st.Ref == nil || !st.Ref.Bound || st.Ref.Entry.Hop.Status != bind.Resolved {
		return nil
	}
	return c.Statements().At(st.Ref.Entry.Hop.Target)
}

// refKeyword returns the keyword st is a reference site under
// (typed.Bindings.RefKeyword), "" for one writing no reference.
func refKeyword(st *statement.Statement) string {
	if st.Ref == nil {
		return ""
	}
	return st.Ref.Keyword
}

// allOfIn returns the entries s's allOf lists, nil for a nil s and one
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
