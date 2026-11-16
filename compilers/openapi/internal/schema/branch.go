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
