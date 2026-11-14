// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package oas

import (
	"encoding/json/jsontext"

	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
	"github.com/dexpace/morphic/ir"
)

// keyPlacer places the keys of one mapping, written at at, as the table
// places a field: at its value, where it is written. The value of a key the
// mapping writes itself as an alias is written where its anchor is, another
// key's coordinate, so that key is placed at its own instead, at the alias.
// Every finding about a key goes through one, whatever its kind, so no two
// kinds place one key apart.
type keyPlacer struct {
	w       *walker
	mapping *yaml.Node
	at      jsontext.Pointer
	// aliases holds each key the mapping writes itself whose last copy is an
	// alias, read on the first key placed: n keys placed cost one scan.
	aliases map[string]*yaml.Node
	scanned bool
}

// newKeyPlacer returns the placer of the keys mapping, written at at, writes.
func newKeyPlacer(w *walker, mapping *yaml.Node, at jsontext.Pointer) *keyPlacer {
	return &keyPlacer{w: w, mapping: nodeview.Deref(mapping), at: at}
}

// place returns where a finding about key is placed. A key whose value is no
// alias of the mapping's own is placed as its pointer is located. An aliased
// one is placed at the mapping's place with key appended, where that place
// names a pointer or the mapping is the root's, and at the alias's line.
func (k *keyPlacer) place(key string) ir.Provenance {
	if !k.scanned {
		k.aliases, k.scanned = ownAliases(k.mapping), true
	}
	alias := k.aliases[key]
	if alias == nil {
		return k.w.locate(k.at.AppendToken(key))
	}
	place := k.w.locate(k.at)
	if place.Pointer != "" || k.w.rootAt+k.at == "" {
		place.Pointer = place.Pointer.AppendToken(key)
	}
	place.Position = ir.Position{Line: alias.Line, Column: alias.Column}
	return place
}

// ownAliases returns each key mapping writes itself, not through a merge,
// whose last copy is an alias, with that alias; nil where there is none.
func ownAliases(mapping *yaml.Node) map[string]*yaml.Node {
	var aliases map[string]*yaml.Node
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		key, own := nodeview.KeyOf(mapping.Content[i])
		if !own || nodeview.IsMergeKey(mapping.Content[i]) {
			continue
		}
		if value := mapping.Content[i+1]; value.Kind == yaml.AliasNode {
			if aliases == nil {
				aliases = map[string]*yaml.Node{}
			}
			aliases[key] = value
			continue
		}
		delete(aliases, key)
	}
	return aliases
}
