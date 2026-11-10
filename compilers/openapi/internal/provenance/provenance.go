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

// WriterAt returns the overlay's index when it introduced or rewrote the
// position at pointer, and 0 otherwise: what ir.Provenance.WrittenBy holds for
// that position of the patched document.
//
// A pointer the attribution never recorded, such as a position reached through
// an alias or a `<<` merge key, which the node tree holds once at the anchor's
// own position, answers 0: an unrecognized pointer under-attributes rather than
// misattributes.
func (a Attribution) WriterAt(pointer jsontext.Pointer) int {
	if a.pointers[pointer] {
		return a.index
	}
	return 0
}

// WriterOf is WriterAt for a construct no single position addresses, recorded
// at pointer and assembled from the positions in from: the overlay's index when
// it wrote every one of them, and WriterAt's answer for pointer otherwise, which
// is also the answer when from is empty.
//
// A construct the overlay wrote entirely is credited to it, and one it only
// added to stays with the document that declares it, as a mapping it merged
// into does (GitHub #534).
func (a Attribution) WriterOf(pointer jsontext.Pointer, from []jsontext.Pointer) int {
	own := a.WriterAt(pointer)
	if len(from) == 0 {
		return own
	}
	writer := a.WriterAt(from[0])
	for _, p := range from[1:] {
		if a.WriterAt(p) != writer {
			return own
		}
	}
	return writer
}

// At returns the pointer at which the overlay put a node it introduced or
// rewrote, and false for any other node, nil included.
func (a Attribution) At(n *yaml.Node) (jsontext.Pointer, bool) {
	pointer, ok := a.nodes[n]
	return pointer, ok
}

// Doc places the pointers of one input document: at Source, the document's
// index in Document.Sources, the overlay applied to it named as the writer of a
// position Overlay attributes to it. The zero Doc places every pointer in
// source 0, with no overlay.
type Doc struct {
	Source  int
	Overlay Attribution
}

// At returns the provenance to record for the position at pointer: the pointer
// in Source, and the overlay as its writer where the overlay supplied what sits
// there.
//
// from is for a record no single position addresses, such as a validation-only
// entry folding several keywords. It lists the positions the record was
// assembled from, so the record names the overlay as its writer only where the
// overlay wrote all of them (Attribution.WriterOf).
func (d Doc) At(pointer jsontext.Pointer, from ...jsontext.Pointer) ir.Provenance {
	return ir.Provenance{Source: d.Source, Pointer: pointer, WrittenBy: d.Overlay.WriterOf(pointer, from)}
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

// Node returns where a finding about n is placed in Doc.Source: where the
// overlay put n, with the overlay as its writer, for a node it introduced or
// rewrote, and otherwise at n's line and column and its pointer in the tree. A
// node the parser did not place has no line or column, which is the zero
// Position rather than one at 0:0. A nil n is the document as a whole.
//
// The tree's root has the empty pointer, which beside a position reads as none,
// so a finding on the document's top mapping is placed by position alone.
func (t Tree) Node(n *yaml.Node) ir.Provenance {
	prov := ir.Provenance{Source: t.Doc.Source}
	if pointer, ok := t.Doc.Overlay.At(n); ok {
		prov.Pointer, prov.WrittenBy = pointer, t.Doc.Overlay.index
		return prov
	}
	if n == nil {
		return prov
	}
	prov.Position = ir.Position{Line: n.Line, Column: n.Column}
	if t.Pointers != nil {
		prov.Pointer, _ = t.Pointers(n)
	}
	return prov
}
