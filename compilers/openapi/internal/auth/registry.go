// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package auth

import (
	"maps"

	"github.com/dexpace/morphic/compilers/compile"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/ir"
)

// Registry is every security scheme of one compile, keyed by ID: what the
// document's securitySchemes entries lower to, and a scheme for each name a
// requirement uses that no entry declares. A requirement can add to it, so it is
// not safe for concurrent use. The zero value is not usable; NewRegistry builds
// one.
type Registry struct {
	byID map[ir.AuthID]ir.AuthScheme
	// root is where a scheme no entry declares is placed: the source's, whichever
	// document holds the requirement naming it.
	root ir.Provenance
}

// NewRegistry returns a Registry holding a copy of declared, the schemes
// LowerSecuritySchemes lowers, so a scheme a requirement adds reaches the
// registry and never the caller's map. Such a scheme is placed at the root of
// c's source.
func NewRegistry(c lowering.Ctx, declared map[ir.AuthID]ir.AuthScheme) *Registry {
	byID := make(map[ir.AuthID]ir.AuthScheme, len(declared))
	maps.Copy(byID, declared)
	return &Registry{byID: byID, root: c.ProvenanceAt("")}
}

// Schemes returns a copy of the registry, or nil when it holds nothing, so a
// Document naming no scheme leaves the field unset.
func (r *Registry) Schemes() map[ir.AuthID]ir.AuthScheme {
	if len(r.byID) == 0 {
		return nil
	}
	return maps.Clone(r.byID)
}

// unresolved returns the ID of the scheme name lowers to, minting it when no
// entry did. The scheme is a function of the name and the source, so which
// requirement reaches it first decides nothing (docs/ir/identity.md).
func (r *Registry) unresolved(name string) ir.AuthID {
	id := ids.UnresolvedAuth(name)
	if _, held := r.byID[id]; !held {
		r.byID[id] = unresolvedScheme(name, r.root)
	}
	return id
}

// unresolvedScheme returns the scheme of no mechanism for name, placed at prov.
func unresolvedScheme(name string, prov ir.Provenance) ir.AuthScheme {
	return ir.AuthScheme{
		ID:         ids.UnresolvedAuth(name),
		Name:       compile.NamingFor(name),
		Kind:       ir.AuthKindUnresolved,
		Provenance: prov,
	}
}
