// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package reftable

import (
	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
)

// bases answers the resource a position read as a schema is in (ReferenceTargetTable.Base,
// ReferenceTargetTable.Resource) as resources.holding does, remembering the walk at each
// position it passes. A position below one walked before starts there rather
// than at its document's root, so asking at every level of a deep schema walks
// its depth once rather than once a level. Where ids says a document is read
// through its dialects (identifiers.readsDialects), each schema is read in the
// one the schema holding it enters (resources.enterIn).
type bases struct {
	res   resources
	walks map[DocumentPointer]walked
	ids   *identifiers
}

// walked is the walk to a position, stopped there on the way to one below: the
// node there, the state the grammar reads it at, the resource the walk is in
// past it, with whether that is known, the dialect what is below it is read
// in, and whether a schema holds it (heldBySchema).
type walked struct {
	node    *yaml.Node
	in      resource
	dialect string
	s       state
	known   bool
	held    bool
}

// newBases returns bases over resourcesValue, whose documents' dialect roots ids holds,
// having walked nothing.
func newBases(resourcesValue resources, ids *identifiers) *bases {
	return &bases{res: resourcesValue, walks: map[DocumentPointer]walked{}, ids: ids}
}

// holding is resources.holding of documentPointer read as a schema, along what its pointer
// reaches from its document's root. A pointer that is not valid is walked from
// the root each time, as holding spells a resource's root on one otherwise than
// by its prefixes.
func (b *bases) holding(documentPointer DocumentPointer) (resource, bool) {
	if !documentPointer.Pointer.IsValid() {
		path, _ := b.res.docs.path(documentPointer)
		return b.res.holding(documentPointer, path, true /* schema */, b.ids.readsDialects(documentPointer.Doc))
	}
	at := prefixes(documentPointer, nodeview.MaxPointerSegments)
	reached, ok := b.walk(at)
	if !ok {
		return b.res.document(documentPointer.Doc), true
	}
	// Read as a schema whatever the grammar reads it as, its own identifier
	// counts, so it is entered from the walk above it, as past enters a schema.
	before := b.start(documentPointer.Doc)
	if reached > 0 {
		before = b.walks[at[reached-1]]
	}
	entering := b.entered(before, b.walks[at[reached]].node, at[reached])
	return entering.res, entering.known
}

// start is where a walk of document id starts, above its root: in its own
// resource, read in its default dialect, held by nothing.
func (b *bases) start(id DocID) walked {
	return walked{in: b.res.document(id), known: true, dialect: defaultDialect(b.res.docs, id)}
}

// walk walks at, the positions on the way to one, as far as its document holds
// them, from the deepest one walked before, remembering each it passes. It
// returns the index of the last one reached, and false where the document's
// root is no node at all.
func (b *bases) walk(at []DocumentPointer) (int, bool) {
	doc := b.res.docs.list[at[0].Doc]
	k := b.deepest(at)
	if k < 0 {
		root := nodeview.Deref(doc.root)
		if root == nil {
			return 0, false
		}
		b.res.docs.work.spend(1)
		b.walks[at[0]] = b.past(b.start(at[0].Doc), root, b.res.docs.identityState(at[0].Doc), at[0])
		k = 0
	}
	for ; k+1 < len(at); k++ {
		from, token := b.walks[at[k]], at[k+1].Pointer.LastToken()
		child := nodeview.Deref(doc.view.ChildByToken(from.node, token))
		if child == nil {
			break
		}
		b.res.docs.work.spend(1)
		state := stepIn(b.res.docs, at[0].Doc, from.node, from.s, token, from.dialect)
		b.walks[at[k+1]] = b.past(from, child, state, at[k+1])
	}
	return k, true
}

// deepest is the index of the deepest of at walked before, or -1 for none. A
// walk remembers every position on its way, so those walked are a run from the
// first, and a search halving it finds its end.
func (b *bases) deepest(at []DocumentPointer) int {
	n := len(at) - 1
	if _, ok := b.walks[at[n]]; ok {
		return n
	}
	lo, hi := -1, n
	if n > 0 {
		if _, ok := b.walks[at[n-1]]; ok {
			return n - 1 // the parent of a position asked, as a deep schema's levels ask
		}
		hi = n - 1
	}
	for hi-lo > 1 {
		mid := lo + (hi-lo)/2
		if _, ok := b.walks[at[mid]]; ok {
			lo = mid
		} else {
			hi = mid
		}
	}
	return lo
}

// prefixes is the positions on the way to documentPointer from its document's root, documentPointer's
// among them, of at most most tokens, as nodeview.View.DocumentPath follows at
// most that many. documentPointer's pointer is valid, so each '/' past the first character
// begins a token.
func prefixes(documentPointer DocumentPointer, most int) []DocumentPointer {
	at := []DocumentPointer{{Doc: documentPointer.Doc}}
	for i := 1; i < len(documentPointer.Pointer) && len(at) <= most; i++ {
		if documentPointer.Pointer[i] == '/' {
			at = append(at, DocumentPointer{Doc: documentPointer.Doc, Pointer: documentPointer.Pointer[:i]})
		}
	}
	if documentPointer.Pointer != "" && len(at) <= most {
		at = append(at, documentPointer)
	}
	return at
}

// past is the walk from, gone on to node at here, which the grammar reads at stateValue:
// a schema, or a position of a document of schemas, is entered, so an
// identifier it writes starts the resource the walk is in past it, and its
// `$schema` the dialect (resources.holding).
func (b *bases) past(from walked, node *yaml.Node, stateValue state, here DocumentPointer) walked {
	out := walked{node: node, s: stateValue, in: from.in, known: from.known, dialect: from.dialect, held: heldBelow(from)}
	if stateValue == sSchema || stateValue == sLoose {
		entering := b.entered(from, node, here)
		out.in, out.known, out.dialect = entering.res, entering.known, entering.dialect
	}
	return out
}

// entered is what the walk from enters at the schema node at here: the resource
// it is in past it, whether that is known, and the dialect what node holds is
// read in, as resources.holding enters each schema on its way.
func (b *bases) entered(from walked, node *yaml.Node, here DocumentPointer) entering {
	enteringValue := entering{dialect: from.dialect}
	if b.ids.readsDialects(here.Doc) {
		held := heldBelow(from)
		enteringValue = b.res.enterIn(from.in, here, node, from.dialect, func() bool { return held })
	} else {
		enteringValue.res, enteringValue.rooted, enteringValue.known = b.res.enter(from.in, node, dialectRule(b.res.docs, here.Doc, from.dialect))
	}
	if enteringValue.rooted {
		enteringValue.res.root = here
	}
	enteringValue.known = from.known && enteringValue.known
	return enteringValue
}

// heldBelow reports whether a schema holds what lies directly below the walk
// from: one does at or above it.
func heldBelow(from walked) bool {
	return from.held || from.s == sSchema || from.s == sLoose
}
