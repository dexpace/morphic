// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema

import (
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/statement"
)

// refBranchTarget returns the schema the chain of references from a
// composition branch ends on, as refs resolved it, and nil when the branch is
// no reference site (typed.Bindings.RefSite). Where the chain ends on no
// schema, or on a bare boolean schema, it returns one statement.Parsed reads
// no schema object of.
func refBranchTarget(c lowering.Ctx, b *statement.Statement) *statement.Schema {
	m := statement.ParsedAt(b)
	if !c.Bindings().RefSite(m) {
		return nil
	}
	return c.Statements().Of(c.Bindings().End(m)).Schema
}
