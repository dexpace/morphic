// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema

import (
	"github.com/speakeasy-api/openapi/jsonschema/oas3"

	"github.com/dexpace/morphic/compilers/openapi/internal/bind/typed"
)

// refBranchTarget returns the schema the chain of references from a
// composition branch ends on, as refs resolved it, or nil when the branch is
// no reference site (typed.Bindings.RefSite), its chain ends on no schema, or
// on a bare boolean schema (which has no *oas3.Schema of its own).
func refBranchTarget(refs *typed.Bindings, b *oas3.JSONSchema[oas3.Referenceable]) *oas3.Schema {
	if !refs.RefSite(b) {
		return nil
	}
	return refs.End(b).GetSchema()
}
