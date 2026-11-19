// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package promotion

import (
	"maps"
	"slices"
	"strings"

	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/ir"
)

// Carried returns own with, for each key promoter reads that own does not hold
// unscoped, the entry of the first of scopes holding it beneath, keyed
// unscoped. It returns own itself where no scope adds a key. It reads a valued
// fact, which the nearest statement settles, the carrier's own first
// (docs/compilers/openapi.md#extension-promotion).
func Carried(promoter Promoter, own ir.Unmodeled, scopes ...string) ir.Unmodeled {
	var picked map[string]carried
	for key, entry := range own {
		name, at, ok := carriedName(promoter.keys, key, scopes)
		if !ok {
			continue
		}
		if _, held := own[extensionKeyPrefix+name]; held {
			continue
		}
		if picked == nil {
			picked = map[string]carried{}
		}
		if prev, seen := picked[name]; !seen || at < prev.at {
			picked[name] = carried{at: at, entry: entry}
		}
	}
	if picked == nil {
		return own
	}
	out := maps.Clone(own)
	for name, carried := range picked {
		out[extensionKeyPrefix+name] = carried.entry
	}
	return out
}

// Shadowed returns, for each of scopes holding one, the entries own holds
// beneath it for a key promoter reads that the carrier's own key or a nearer scope
// states too, keyed unscoped: the statements Carried passes over. A carrier
// reads each for its findings alone, so a misfit behind a nearer statement of
// its key is reported once, at its own key. It returns nil where Carried
// passes over nothing.
func Shadowed(promoter Promoter, own ir.Unmodeled, scopes ...string) []ir.Unmodeled {
	var out []ir.Unmodeled
	for i, scope := range scopes {
		var layer ir.Unmodeled
		for key, entry := range scopedEntries(promoter.keys, own, scope) {
			if !statedNearer(own, key, scopes[:i]) {
				continue
			}
			if layer == nil {
				layer = ir.Unmodeled{}
			}
			layer[key] = entry
		}
		if layer != nil {
			out = append(out, layer)
		}
	}
	return out
}

// statedNearer reports whether own holds key, "openapi:<name>", unscoped or
// beneath one of nearer.
func statedNearer(own ir.Unmodeled, key string, nearer []string) bool {
	if _, held := own[key]; held {
		return true
	}
	name := strings.TrimPrefix(key, extensionKeyPrefix)
	for _, scope := range nearer {
		if _, held := own[extensionKeyPrefix+scope+"/"+name]; held {
			return true
		}
	}
	return false
}

// CarrierScopes returns the scopes a parameter or a header keeps the keys of
// the objects it carries under, the nearest first: the content entry
// mediaType names and then that entry's schema, or the schema alone where
// mediaType is empty, the schema spelling (docs/ir/unmodeled.md#keys).
func CarrierScopes(mediaType string) []string {
	if mediaType == "" {
		return []string{ids.Scope("schema")}
	}
	return []string{ids.Scope("content", mediaType), ids.Scope("content", mediaType, "schema")}
}

// carried is an entry a scope holds beneath a carrier, and which of the scopes
// asked holds it, the nearest first.
type carried struct {
	at    int
	entry ir.UnmodeledEntry
}

// WithOwnCarriedState returns from with what the objects own carries state, each read as a
// $ref target's keys are: a single-sided fact holds where any states it, and
// a deprecation's members fill from the nearest stating them. The carrier's
// own keys are its caller's to read, and from is farther than any scope.
func (p Promoter) WithOwnCarriedState(from Referent, own ir.Unmodeled, scopes ...string) Referent {
	var fallbackReferent Referent
	for i := range scopes {
		entries := scopedEntries(p.keys, own, scopes[i])
		if entries == nil {
			continue
		}
		fallbackReferent = nearer(fallbackReferent, p.readReferent(true, entries))
	}
	return nearer(fallbackReferent, from)
}

// scopedEntries returns the entries own holds beneath scope for one of keys, keyed
// unscoped, or nil where it holds none.
func scopedEntries(keys []string, own ir.Unmodeled, scope string) ir.Unmodeled {
	var out ir.Unmodeled
	for key, entry := range own {
		name, _, ok := carriedName(keys, key, []string{scope})
		if !ok {
			continue
		}
		if out == nil {
			out = ir.Unmodeled{}
		}
		out[extensionKeyPrefix+name] = entry
	}
	return out
}

// carriedName returns the key an entry keyed key holds beneath one of scopes,
// and which, where that key is one of keys, sorted: "openapi:<scope>/<name>"
// names name. It allocates nothing.
func carriedName(keys []string, key string, scopes []string) (string, int, bool) {
	// Every key this compiler keeps opens with the prefix.
	rest := strings.TrimPrefix(key, extensionKeyPrefix)
	for i, scope := range scopes {
		name, under := strings.CutPrefix(rest, scope)
		if !under || !strings.HasPrefix(name, "/") {
			continue
		}
		if _, read := slices.BinarySearch(keys, name[1:]); read {
			return name[1:], i, true
		}
	}
	return "", 0, false
}

// nearer returns a with what b states that a does not: a single-sided fact
// either states, and each deprecation member a leaves empty.
func nearer(a, b Referent) Referent {
	a.Deprecated = a.Deprecated || b.Deprecated
	a.Redacted = a.Redacted || b.Redacted
	a.Homes = a.Homes || b.Homes
	farther := deprecationMembers(&b.Members)
	for i, member := range deprecationMembers(&a.Members) {
		if *member.field == "" {
			*member.field = *farther[i].field
		}
	}
	return a
}
