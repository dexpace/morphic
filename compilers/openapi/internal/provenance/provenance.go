// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package provenance

import (
	"encoding/json/jsontext"

	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/ir"
)

// Attribution is the positions an applied overlay supplied: those it introduced
// or rewrote, by pointer and by the node sitting at each. Its zero value
// attributes nothing, the answer for a compile with no overlay.
type Attribution struct {
	// index is the overlay's position in Document.Sources.
	index int
	// pointers holds every position the overlay introduced or rewrote, closed
	// downwards: a cloned subtree contributes each of its own nodes, so a lookup
	// is one map hit rather than a walk up the pointer's prefixes.
	pointers map[jsontext.Pointer]bool
	// nodes holds the same positions keyed by the node that sits at each: the
	// value the walk attributed, and the key beside it when the overlay
	// introduced that too. A finding raised on a raw node has the node and not
	// the pointer, and a grafted node carries no line and column of its own,
	// since the library's clone keeps neither (GitHub #476).
	nodes map[*yaml.Node]jsontext.Pointer
}

// Attributed returns the attribution of the overlay at index in
// Document.Sources, which supplied the positions pointers holds and the nodes
// nodes holds, each at its pointer. It keeps both maps, which nothing may write
// afterwards; nil maps attribute nothing.
func Attributed(index int, pointers map[jsontext.Pointer]bool, nodes map[*yaml.Node]jsontext.Pointer) Attribution {
	return Attribution{index: index, pointers: pointers, nodes: nodes}
}

// IndexAt returns the index of the source that supplied the position at pointer:
// the overlay's, if the overlay introduced or rewrote it, and fallback
// otherwise.
//
// A pointer the attribution never recorded, such as a position reached through
// an alias or a `<<` merge key, which the node tree holds once at the anchor's
// own position, falls back: an unrecognized pointer under-attributes rather than
// misattributes.
func (a Attribution) IndexAt(pointer jsontext.Pointer, fallback int) int {
	if a.pointers[pointer] {
		return a.index
	}
	return fallback
}

// IndexOf is IndexAt for a construct no single position addresses, recorded at
// pointer and assembled from the positions in from: the index of the source that
// supplied every one of them when one source did, and IndexAt's answer for
// pointer otherwise, which is also the answer when from is empty.
//
// A construct the overlay wrote entirely is the overlay's, and one it only added
// to stays with the document that declares it, as a mapping it merged into does
// (GitHub #534).
func (a Attribution) IndexOf(pointer jsontext.Pointer, from []jsontext.Pointer, fallback int) int {
	own := a.IndexAt(pointer, fallback)
	if len(from) == 0 {
		return own
	}
	index := a.IndexAt(from[0], fallback)
	for _, p := range from[1:] {
		if a.IndexAt(p, fallback) != index {
			return own
		}
	}
	return index
}

// At returns the provenance of a node the overlay introduced or rewrote, the
// overlay's index and the pointer the node sits at, and false for any other
// node, nil included.
func (a Attribution) At(n *yaml.Node) (ir.Provenance, bool) {
	pointer, ok := a.nodes[n]
	if !ok {
		return ir.Provenance{}, false
	}
	return ir.Provenance{Source: a.index, Pointer: pointer}, true
}

// Doc places the pointers of one input document: at Source, the document's
// index in Document.Sources, unless Overlay attributes a position to the overlay
// applied to it. The zero Doc places every pointer in source 0, with no overlay.
type Doc struct {
	Source  int
	Overlay Attribution
}

// At returns the provenance to record for the position at pointer: the pointer,
// and the index of the input document that supplied what sits there.
//
// from is for a record no single position addresses, such as a validation-only
// entry folding several keywords. It lists the positions the record was
// assembled from, so the record names the document that wrote all of them, when
// one did (Attribution.IndexOf).
func (d Doc) At(pointer jsontext.Pointer, from ...jsontext.Pointer) ir.Provenance {
	return ir.Provenance{Source: d.Overlay.IndexOf(pointer, from, d.Source), Pointer: pointer}
}

// Tree places the nodes of one document's decoded tree, which Doc places the
// pointers of. Pointers returns a node's pointer in the tree and whether the
// tree holds it, and is nil for a tree that names no pointer.
//
// Pointers stays a function because the index behind it is built lazily, under
// the context of the load reading the tree, which a struct may not hold.
type Tree struct {
	Doc      Doc
	Pointers func(*yaml.Node) (jsontext.Pointer, bool)
}

// Node returns where a finding about n is placed: where the overlay put n, for
// a node it introduced or rewrote, and otherwise in Doc.Source at n's line and
// column and its pointer in the tree. A node the parser did not place has no
// line or column, which is the zero Position rather than one at 0:0. A nil n
// is the document as a whole, placed in Doc.Source alone.
//
// The tree's root has the empty pointer, which beside a position reads as none,
// so a finding on the document's top mapping is placed by position alone.
func (t Tree) Node(n *yaml.Node) ir.Provenance {
	if prov, ok := t.Doc.Overlay.At(n); ok {
		return prov
	}
	prov := ir.Provenance{Source: t.Doc.Source}
	if n == nil {
		return prov
	}
	prov.Position = ir.Position{Line: n.Line, Column: n.Column}
	if t.Pointers != nil {
		prov.Pointer, _ = t.Pointers(n)
	}
	return prov
}
