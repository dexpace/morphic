package schema

import (
	"github.com/speakeasy-api/openapi/jsonschema/oas3"

	"github.com/dexpace/morphic/compilers/openapi/internal/bind/typed"
)

// isRefBranch reports whether a composition branch is a $ref rather than an
// inline schema.
func isRefBranch(b *oas3.JSONSchema[oas3.Referenceable]) bool {
	if b == nil {
		return false
	}
	if b.IsReference() {
		return true
	}
	s := b.GetSchema()
	return s != nil && s.Ref != nil
}

// isInlineBranch reports whether a composition branch is written inline rather
// than as a $ref. It is isRefBranch's negation, named so a search over branches
// reads as the question being asked.
func isInlineBranch(b *oas3.JSONSchema[oas3.Referenceable]) bool {
	return !isRefBranch(b)
}

// refBranchTarget returns the schema the chain of $refs from a composition
// branch ends on, as refs resolved it, or nil when the branch is inline, its
// chain ends on no schema, or on a bare boolean schema (which has no
// *oas3.Schema of its own).
func refBranchTarget(refs *typed.Bindings, b *oas3.JSONSchema[oas3.Referenceable]) *oas3.Schema {
	if !isRefBranch(b) {
		return nil
	}
	return refs.End(b).GetSchema()
}
