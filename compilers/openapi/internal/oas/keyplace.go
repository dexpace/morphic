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
// mapping writes itself that another place may read, an alias or an anchored
// node (sharedNode), is placed at the least position reading it, maybe another
// key's, so that key is placed at its own instead, at its value. Every finding
// about a key goes through one, whatever its kind, so no two kinds place one
// key apart.
type keyPlacer struct {
	w       *walker
	mapping *yaml.Node
	at      jsontext.Pointer
	// shared holds each key the mapping writes itself whose last copy another
	// place may read, read on the first key placed: n keys placed cost one scan.
	shared  map[string]*yaml.Node
	scanned bool
}

// newKeyPlacer returns the placer of the keys mapping, written at at, writes.
func newKeyPlacer(w *walker, mapping *yaml.Node, at jsontext.Pointer) *keyPlacer {
	return &keyPlacer{w: w, mapping: nodeview.Deref(mapping), at: at}
}

// place returns where a finding about key is placed. A key whose value no
// other place may read is placed as its pointer is located. A shared one is
// placed at the mapping's place with key appended, where that place names a
// pointer or the mapping is the root's, and at its value's line.
func (k *keyPlacer) place(key string) ir.Provenance {
	if !k.scanned {
		k.shared, k.scanned = ownShared(k.mapping), true
	}
	value := k.shared[key]
	if value == nil {
		return k.w.locate(k.at.AppendToken(key))
	}
	place := k.w.locate(k.at)
	if place.Pointer != "" || k.w.rootAt+k.at == "" {
		place.Pointer = place.Pointer.AppendToken(key)
	}
	place.Position = ir.Position{Line: value.Line, Column: value.Column}
	return place
}

// ownShared returns each key mapping writes itself, not through a merge, whose
// last copy another place may read (sharedNode), with that copy; nil where
// there is none.
func ownShared(mapping *yaml.Node) map[string]*yaml.Node {
	var shared map[string]*yaml.Node
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		key, own := nodeview.KeyOf(mapping.Content[i])
		if !own || nodeview.IsMergeKey(mapping.Content[i]) {
			continue
		}
		if value := mapping.Content[i+1]; sharedNode(value) {
			if shared == nil {
				shared = map[string]*yaml.Node{}
			}
			shared[key] = value
			continue
		}
		delete(shared, key)
	}
	return shared
}
