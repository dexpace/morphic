// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package merge

import (
	"encoding/json/v2"
	"slices"
	"strings"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/ir"
)

// keptOnNode is what lastWritten says of a declaration the model writes
// itself whose note the field does not keep and the field's type holds, on
// the node its schema builds or one built from it (leaveNotesOnNodes).
const keptOnNode = "the field's type holds"

// byOrder ranks every declaration alike, so the last to give a detail keeps
// it (keptAt).
func byOrder(declaration) int { return 0 }

// ownFirst ranks a declaration the model writes itself over one another model
// holds (keptAt), which keeps its note there, so no note the model writes is
// lost to one another model's declaration brings.
func ownFirst(declarationValue declaration) int {
	if declarationValue.via == nil {
		return 1
	}
	return 0
}

// keptAt returns the index of the declaration of decls whose detail the field
// keeps, of those gives says give one: the last of the highest rank, or -1
// where none gives one.
func keptAt(decls []declaration, rank func(declaration) int, gives func(i int) bool) int {
	at := -1
	for i, declarationValue := range slices.Backward(decls) {
		if gives(i) && (at < 0 || rank(declarationValue) > rank(decls[at])) {
			at = i
		}
	}
	return at
}

// titleOf reads a property's title, one of the notes it holds where its schema
// builds a node of its own, which the lowering moves off that node
// (docs/ir/annotations.md#one-structural-home-per-declaration), as lastWritten
// reads a detail.
func titleOf(property *ir.Property) (string, bool) {
	return property.Docs.Summary, property.Docs.Summary != ""
}

// descriptionOf reads a property's description as titleOf reads its title.
func descriptionOf(property *ir.Property) (string, bool) {
	return property.Docs.Description, property.Docs.Description != ""
}

// externalDocsOf reads a property's external links as titleOf reads its title.
func externalDocsOf(property *ir.Property) ([]ir.Link, bool) {
	return property.Docs.ExternalDocs, len(property.Docs.ExternalDocs) > 0
}

// deprecationOf reads a property's deprecation as titleOf reads its title.
func deprecationOf(property *ir.Property) (*ir.Deprecation, bool) {
	return property.Deprecation, property.Deprecation != nil
}

// sameText reports whether two texts are one.
func sameText(a, b string) bool { return a == b }

// anyIndex reads a detail as lastWritten does, whichever declaration gives it.
func anyIndex[T any](of func(*ir.Property) (T, bool)) func(int, *ir.Property) (T, bool) {
	return func(_ int, property *ir.Property) (T, bool) { return of(property) }
}

// leftNotes is what leaveNotesOnNodes left on one declaration's node: the
// node, what the node held of its notes before, which takeBack writes back,
// and by detail whether a node holds the note the declaration gives of it.
type leftNotes struct {
	node   *ir.TypeCommon
	before nodeNotes
	holds  map[string]func(node *ir.TypeCommon) bool
}

// nodeNotes is what a node holds of the notes leaveNotesOnNodes writes.
type nodeNotes struct {
	docs        ir.Docs
	deprecation *ir.Deprecation
	inferred    ir.Inferred
}

// leaveNotesOnNodes keeps on its node each note a declaration of decls the
// model writes itself gives otherwise than the one the field keeps (keptAt,
// ownFirst), where the lowering hoisted that node at the declaration, no other
// member carries the note, and the node holds none (ownNode): the field has
// one slot, and that node is where
// the note sat before the lowering moved it to the property. It runs before
// the fold judges the types, so the node is judged holding it, as one its
// schema's notes stayed on is. It returns, by declaration, what it left.
func (g *Merger) leaveNotesOnNodes(decls []declaration) []leftNotes {
	left := make([]leftNotes, len(decls))
	leaveOnNode(g, decls, left, "title", titleOf, sameText, func(node *ir.TypeCommon, property *ir.Property) bool {
		if node.Docs.Summary != "" {
			return false
		}
		node.Docs.Summary = property.Docs.Summary
		return true
	})
	leaveOnNode(g, decls, left, "description", descriptionOf, sameText, func(node *ir.TypeCommon, property *ir.Property) bool {
		if node.Docs.Description != "" {
			return false
		}
		node.Docs.Description, node.Docs.Refs = property.Docs.Description, slices.Clone(property.Docs.Refs)
		return true
	})
	leaveOnNode(g, decls, left, "external docs", externalDocsOf, slices.Equal[[]ir.Link], func(node *ir.TypeCommon, property *ir.Property) bool {
		if len(node.Docs.ExternalDocs) > 0 {
			return false
		}
		node.Docs.ExternalDocs = slices.Clone(property.Docs.ExternalDocs)
		return true
	})
	leaveOnNode(g, decls, left, "deprecation", deprecationOf, sameDeprecation, func(node *ir.TypeCommon, property *ir.Property) bool {
		if node.Deprecation != nil {
			return false
		}
		deprecation := *property.Deprecation
		deprecation.ReplacedBy = slices.Clone(deprecation.ReplacedBy)
		node.Deprecation = &deprecation
		node.Provenance.Inferred = ir.JoinInferred(node.Provenance.Inferred, property.Provenance.Inferred)
		return true
	})
	return left
}

// leaveOnNode is leaveNotesOnNodes for one detail, which of reads and same
// compares, and put writes on a node from a property, false where the node
// holds one already.
func leaveOnNode[T any](g *Merger, decls []declaration, left []leftNotes, detail string, of func(*ir.Property) (T, bool),
	same func(a, b T) bool, put func(node *ir.TypeCommon, property *ir.Property) bool,
) {
	at := keptAt(decls, ownFirst, func(i int) bool {
		_, gives := of(&decls[i].property)
		return gives
	})
	if at < 0 {
		return
	}
	kept, _ := of(&decls[at].property)
	for i := range decls {
		v, gives := of(&decls[i].property)
		if !gives || same(kept, v) {
			continue
		}
		node, owned := g.ownNode(decls[i])
		if !owned {
			continue
		}
		before := nodeNotes{docs: node.Docs, deprecation: node.Deprecation, inferred: node.Provenance.Inferred}
		if !put(node, &decls[i].property) {
			continue
		}
		if left[i].holds == nil {
			left[i] = leftNotes{node: node, before: before, holds: map[string]func(*ir.TypeCommon) bool{}}
		}
		left[i].holds[detail] = func(other *ir.TypeCommon) bool {
			held, gives := of(&ir.Property{Docs: other.Docs, Deprecation: other.Deprecation})
			return gives && same(held, v)
		}
	}
}

// takeBack writes back what leaveNotesOnNodes left on the node of each
// declaration whose node does not stay (stays), and reports whether it wrote
// any back. Of those notes, one counts as held only where held says another
// node holds it: the node the field's type is built from adopts it
// (builder.adoptInto), so the node it came from keeps no copy. Any other is
// forgotten, so that declaration is kept verbatim, as two nodes judged apart
// for a note alone, two lists say, need not meet, and the fold judges the
// types again as though no note were left.
func takeBack(left []leftNotes, stays func(i int) bool, held func(holds func(*ir.TypeCommon) bool) bool) bool {
	took := false
	for i := range left {
		if left[i].node == nil || stays(i) {
			continue
		}
		node, before := left[i].node, left[i].before
		node.Docs, node.Deprecation, node.Provenance.Inferred = before.docs, before.deprecation, before.inferred
		took = true
		for detail, holds := range left[i].holds {
			if !held(holds) {
				delete(left[i].holds, detail)
			}
		}
	}
	return took
}

// holdsNone says no node holds a note taken back, which is what the fold
// knows before it builds the field's type.
func holdsNone(func(*ir.TypeCommon) bool) bool { return false }

// namesNode returns whether the node leaveNotesOnNodes left decls[i]'s notes
// on keeps them: the field's type, target composing conjuncts, is that node or
// composes it. The fold builds that type of the cone alone, and no other
// declaration names decls[i]'s node (ownNode), so it is none outside the cone.
func namesNode(decls []declaration, target ir.TypeID, conjuncts []conjunct) func(i int) bool {
	return func(i int) bool {
		id := decls[i].property.Type.Target
		return id == target || slices.ContainsFunc(conjuncts, func(c conjunct) bool { return c.ref.Target == id })
	}
}

// heldIn returns whether the field's type, target, a node the fold built,
// holds the note holds reads, which it adopted from the node that note was
// left on or from another left the same note, as two notes that differ are
// kept apart.
func heldIn(g *Merger, target ir.TypeID) func(holds func(*ir.TypeCommon) bool) bool {
	td, ok := g.resolve(target)
	// No fold yet reaches here with a type the registry lacks, but one would
	// read a nil node (TestHeldIn_ATypeTheRegistryLacksHoldsNoNote).
	return func(holds func(*ir.TypeCommon) bool) bool { return ok && holds(td.Common()) }
}

// ownNode returns the node the type of declarationValue, which the model
// writes itself, names where the lowering hoisted it at the declaration: the
// node its schema builds, which no reference names, as a node one names keeps
// its notes (docs/ir/annotations.md#one-structural-home-per-declaration). A
// declaration another model holds, as its own or as one an entry a reference
// names carries (apart), whose member holds its notes, or one whose type is
// shared or written elsewhere, has none.
func (g *Merger) ownNode(declarationValue declaration) (*ir.TypeCommon, bool) {
	if declarationValue.via != nil || declarationValue.apart {
		return nil, false
	}
	td, ok := g.resolve(declarationValue.property.Type.Target)
	if !ok {
		return nil, false
	}
	node, at := td.Common(), declarationValue.property.Provenance
	return node, node.Anonymous && node.Provenance.Source == at.Source && node.Provenance.Pointer == at.Pointer
}

// heldExamples returns the examples the field holds, of those each
// declaration gives that it takes (taken): every one a declaration the model
// writes itself gives, each once, as JSON Schema collects an annotation from
// each subschema, named or not, in an order no order of the entries moves
// (ownExampleLists); and, only where none gives one, those of the last
// declaration another model holds (lastWritten). One such declaration giving
// an example the field does not hold is reported: it keeps it there.
func heldExamples(f *folding, taken []bool) []ir.Example {
	of := func(i int, property *ir.Property) ([]ir.Example, bool) {
		return property.Examples, taken[i] && len(property.Examples) > 0
	}
	held := func(i int) bool {
		_, gives := of(i, &f.decls[i].property)
		return gives && f.decls[i].via == nil
	}
	lists := ownExampleLists(f.decls, of, held)
	if len(lists) == 0 {
		return lastWritten(f, "examples", byOrder, of, sameExamples)
	}
	var own []ir.Example
	for _, examples := range lists {
		for _, example := range examples {
			if !slices.ContainsFunc(own, func(held ir.Example) bool { return sameExample(held, example) }) {
				own = append(own, example)
			}
		}
	}
	kept := "those the declarations the model writes itself give"
	if len(lists) == 1 {
		kept = "the one the declaration at " + f.at(keptAt(f.decls, byOrder, held)) + " gives"
	}
	for i := len(f.decls) - 1; i >= 0; i-- {
		examples, gives := of(i, &f.decls[i].property)
		if !gives || containsExamples(own, examples) {
			continue
		}
		f.note(i, ir.SeverityInfo, diag.DegradedConstruct,
			"declarations of field %q give its examples differently; kept %s over the one at %s, which %s",
			f.merged.WireName, kept, f.at(i), f.decls[i].keptAs())
	}
	return own
}

// ownExampleLists returns the example lists the declarations of decls that
// held picks give (of), ordered by their encodings, so the field holds them in
// one order whatever order its allOf entries come in, as JSON Schema gives
// them none. A list that does not encode, such as one holding invalid UTF-8,
// which no document holding it encodes either, sorts first, in declaration
// order.
func ownExampleLists(decls []declaration, of func(i int, property *ir.Property) ([]ir.Example, bool), held func(i int) bool) [][]ir.Example {
	type encodedList struct {
		key      string
		examples []ir.Example
	}
	var lists []encodedList
	for i := range decls {
		if !held(i) {
			continue
		}
		examples, _ := of(i, &decls[i].property)
		key, err := json.Marshal(examples, json.Deterministic(true))
		if err != nil {
			key = []byte{}
		}
		lists = append(lists, encodedList{key: string(key), examples: examples})
	}
	slices.SortStableFunc(lists, func(a, b encodedList) int { return strings.Compare(a.key, b.key) })
	out := make([][]ir.Example, len(lists))
	for k, list := range lists {
		out[k] = list.examples
	}
	return out
}

// containsExamples reports whether held holds each example of examples.
func containsExamples(held, examples []ir.Example) bool {
	for _, example := range examples {
		if !slices.ContainsFunc(held, func(heldExample ir.Example) bool { return sameExample(heldExample, example) }) {
			return false
		}
	}
	return true
}
