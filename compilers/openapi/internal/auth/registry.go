// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package auth

import (
	"fmt"
	"maps"
	"strings"

	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/ir"
)

// Registry is every security scheme of one compile, keyed by ID: what the
// document's securitySchemes entries lower to, and a scheme for each name a
// requirement uses that no entry declares. A requirement can add to it, so it is
// not safe for concurrent use. The zero value is not usable; RegistryOf builds
// one.
type Registry struct {
	byID map[ir.AuthID]ir.AuthScheme
	// entries holds the name of every securitySchemes entry of the source,
	// whatever it lowered to, which a requirement's key is read as first
	// (named). It is filled once, so no requirement a walk meets first reaches
	// what another's key names.
	entries map[string]bool
	// near indexes the names of the entries that reach a mechanism, which an
	// unresolved name's error may name (nearIndex.nearest), and missed holds
	// the one each minted name is a near miss of, read once, as it is minted.
	near   nearIndex
	missed map[string]string
	// root is where a scheme no entry declares is placed: the source's, whichever
	// document holds the requirement naming it.
	root ir.Provenance
}

// RegistryOf returns a Registry holding a copy of declared, the schemes
// LowerSecuritySchemes lowers, so a scheme a requirement adds reaches the
// registry and never the caller's map. Such a scheme is placed at the root of
// c's source.
func RegistryOf(c lowering.Ctx, declared map[ir.AuthID]ir.AuthScheme) *Registry {
	byID := make(map[ir.AuthID]ir.AuthScheme, len(declared))
	entries := make(map[string]bool, len(declared))
	var reaching []string
	for id, scheme := range declared {
		byID[id] = scheme
		entries[scheme.Name.Source] = true
		if scheme.Kind != ir.AuthKindUnresolved {
			reaching = append(reaching, scheme.Name.Source)
		}
	}
	return &Registry{byID: byID, entries: entries, near: nearIndexOf(reaching), missed: map[string]string{},
		root: c.Place().At("")}
}

// Schemes returns a copy of the registry, or nil when it holds nothing, so a
// Document naming no scheme leaves the field unset.
func (r *Registry) Schemes() map[ir.AuthID]ir.AuthScheme {
	if len(r.byID) == 0 {
		return nil
	}
	return maps.Clone(r.byID)
}

// unresolvedClauses returns what an unresolved scheme's error adds about the key
// a requirement writes: the declared scheme near names, if any, and why, where
// the key is a reference, it lands on no Security Scheme Object.
func unresolvedClauses(near, why string) string {
	var clauses strings.Builder
	if near != "" {
		fmt.Fprintf(&clauses, "; a scheme named %q is declared", near)
	}
	if why != "" {
		clauses.WriteString("; " + why)
	}
	return clauses.String()
}

// unresolved returns id, the scheme name lowers to where it reaches no
// mechanism, minting it when no entry did, and the declared name a minted one
// is a near miss of, or "". The scheme and the near miss are functions of the
// name and the document writing it, so which requirement reaches it first
// decides nothing (docs/ir/identity.md).
func (r *Registry) unresolved(id ir.AuthID, name string) (ir.AuthID, string) {
	if _, held := r.byID[id]; !held {
		r.byID[id] = unresolvedScheme(id, name, r.root)
		if near, ok := r.near.nearest(name); ok {
			r.missed[name] = near
		}
	}
	return id, r.missed[name]
}

// unresolvedScheme returns the scheme id of no mechanism for name, placed at
// prov.
func unresolvedScheme(id ir.AuthID, name string, prov ir.Provenance) ir.AuthScheme {
	return ir.AuthScheme{
		ID:         id,
		Name:       ir.NamingFor(name),
		Kind:       ir.AuthKindUnresolved,
		Provenance: prov,
	}
}
