// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package merge

import (
	"slices"

	"github.com/dexpace/morphic/ir"
)

// heldNotes is what a property or a node holds of the notes a fold ranks, and
// the marker over them.
type heldNotes struct {
	CarriedNotes
	inferred ir.Inferred
}

// noteReading is how a fold reads one note, which detail names: what the
// declaration at i gives the field off its property (given), what a node holds
// (held), and whether two say the same.
type noteReading[T any] struct {
	detail string
	given  func(i int) (T, bool)
	held   func(notes heldNotes) (T, bool)
	same   func(a, b T) bool
}

// readingOf is the noteReading of a note a property of foldingValue's
// declarations holds as a node does (held).
func readingOf[T any](foldingValue *folding, detail string, held func(notes heldNotes) (T, bool), same func(a, b T) bool) noteReading[T] {
	given := func(i int) (T, bool) {
		property := &foldingValue.decls[i].property
		return held(heldNotes{CarriedNotes{property.Docs, property.Deprecation, property.Examples}, property.Provenance.Inferred})
	}
	return noteReading[T]{detail: detail, given: given, held: held, same: same}
}

// nodeNotes returns the notes the node id names holds as it stands, none where
// id names none.
func nodeNotes(g *Merger, id ir.TypeID) heldNotes {
	td, ok := g.Reg[id]
	if !ok {
		return heldNotes{}
	}
	common := td.Common()
	return heldNotes{CarriedNotes{common.Docs, common.Deprecation, common.Examples}, common.Provenance.Inferred}
}

// builtNodeNotes returns the notes the node declarationValue built holds, where
// the model writes declarationValue itself: the node of a member the model
// declares again keeps them, as does one a reference names and one naming a
// $ref.
func builtNodeNotes(g *Merger, declarationValue declaration) heldNotes {
	target := declarationValue.property.Type.Target
	td, ok := g.Reg[target]
	if !ok || isBrought(declarationValue) || !hoistedAt(td, declarationValue.sites()) {
		return heldNotes{}
	}
	return nodeNotes(g, target)
}

// settleNote returns what the field holds of the note reading reads: the last
// declaration's to give it, the model's own first (lastWritten, ownFirst). A
// model's own note standing on the node its declaration built (givenOnNodes) is
// kept over one an entry brings too, and the field holds it only where no
// reader of the field's type reads it there. A replacement only a key names
// stays on the node keeping the key, which resolves it (replacement.Resolve).
func settleNote[T any](foldingValue *folding, reading noteReading[T]) T {
	onNodes, read := givenOnNodes(foldingValue, reading)
	kept := settleBeside(foldingValue, reading, onNodes)
	if read {
		return *new(T)
	}
	return kept
}

// settleExamples is settleNote for the examples reading reads, which the field
// takes of no node: one whose type is read holding none of the model's own has
// not been shown to admit any of them.
func settleExamples(foldingValue *folding, reading noteReading[[]ir.Example]) []ir.Example {
	onNodes, read := givenOnNodes(foldingValue, reading)
	if !read {
		return settleBeside(foldingValue, reading, nil)
	}
	settleBeside(foldingValue, reading, onNodes)
	return nil
}

// settleBeside is lastWritten of the note reading reads, ranked ownFirst, each
// declaration of onNodes giving what the node it built holds.
func settleBeside[T any](foldingValue *folding, reading noteReading[T], onNodes []int) T {
	return lastWritten(foldingValue, reading.detail, ownFirst, func(i int, _ *ir.Property) (T, bool) {
		if slices.Contains(onNodes, i) {
			return reading.held(builtNodeNotes(foldingValue.g, foldingValue.decls[i]))
		}
		return reading.given(i)
	}, reading.same)
}

// givenOnNodes returns the model's own declarations whose note, which reading
// reads, stands on the node each built and is kept over the one an entry
// brings, and whether a reader of the field's type reads it: off the node that
// type names, one declaration's note alone, or off those the type composes, the
// last to give one standing for them. Otherwise they are all that give one. It
// returns none where no note an entry brings would be the field's, the model's
// own giving one on a property or none giving any.
func givenOnNodes[T any](foldingValue *folding, reading noteReading[T]) (onNodes []int, read bool) {
	onProperty := keptAt(foldingValue.decls, ownFirst, func(i int) bool {
		_, gives := reading.given(i)
		return gives
	})
	if onProperty < 0 || !isBrought(foldingValue.decls[onProperty]) {
		return nil, false
	}
	held, holds := reading.held(nodeNotes(foldingValue.g, foldingValue.merged.Type.Target))
	composes, last := len(foldingValue.conjuncts) > 0, -1
	for i := range foldingValue.decls {
		note, gives := reading.held(builtNodeNotes(foldingValue.g, foldingValue.decls[i]))
		if !gives {
			continue
		}
		onNodes = append(onNodes, i)
		if composes || holds && reading.same(held, note) {
			last = i
		}
	}
	if last >= 0 {
		return []int{last}, true
	}
	return onNodes, false
}
