// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package bind

import (
	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
)

// bases answers the resource a position read as a schema is in (Table.Base,
// Table.InOwnResource) as resources.holding does, remembering the walk at each
// position it passes. A position below one walked before starts there rather
// than at its document's root, so asking at every level of a deep schema walks
// its depth once rather than once a level.
type bases struct {
	res   resources
	walks map[Position]walked
}

// walked is the walk to a position, stopped there on the way to one below: the
// node there, the state the grammar reads it at, and the resource the walk is
// in past it, with whether that is known.
type walked struct {
	node  *yaml.Node
	in    resource
	s     state
	known bool
}

// newBases returns bases over r, having walked nothing.
func newBases(r resources) *bases {
	return &bases{res: r, walks: map[Position]walked{}}
}

// holding is resources.holding of p read as a schema, along what its pointer
// reaches from its document's root. A pointer that is not valid is walked from
// the root each time, as holding spells a resource's root on one otherwise than
// by its prefixes.
func (b *bases) holding(p Position) (resource, bool) {
	if !p.Pointer.IsValid() {
		path, _ := b.res.docs.path(p)
		return b.res.holding(p, path, true /* schema */)
	}
	at := prefixes(p, nodeview.MaxPointerSegments)
	reached, ok := b.walk(at)
	if !ok {
		return b.res.document(p.Doc), true
	}
	last := b.walks[at[reached]]
	if last.s == sSchema || last.s == sLoose {
		return last.in, last.known
	}
	// Read as a schema whatever the grammar reads it as, its own $id counts.
	before := walked{in: b.res.document(p.Doc), known: true}
	if reached > 0 {
		before = b.walks[at[reached-1]]
	}
	return b.res.entered(before, last.node, at[reached])
}

// walk walks at, the positions on the way to one, as far as its document holds
// them, from the deepest one walked before, remembering each it passes. It
// returns the index of the last one reached, and false where the document's
// root is no node at all.
func (b *bases) walk(at []Position) (int, bool) {
	doc := b.res.docs.list[at[0].Doc]
	k := b.deepest(at)
	if k < 0 {
		root := nodeview.Deref(doc.root)
		if root == nil {
			return 0, false
		}
		b.res.docs.work.spend(1)
		start := walked{in: b.res.document(at[0].Doc), known: true}
		b.walks[at[0]] = b.res.past(start, root, b.res.docs.identityState(at[0].Doc), at[0])
		k = 0
	}
	for ; k+1 < len(at); k++ {
		from, token := b.walks[at[k]], at[k+1].Pointer.LastToken()
		child := nodeview.Deref(doc.view.ChildByToken(from.node, token))
		if child == nil {
			break
		}
		b.res.docs.work.spend(1)
		b.walks[at[k+1]] = b.res.past(from, child, step(from.s, token), at[k+1])
	}
	return k, true
}

// deepest is the index of the deepest of at walked before, or -1 for none. A
// walk remembers every position on its way, so those walked are a run from the
// first, and a search halving it finds its end.
func (b *bases) deepest(at []Position) int {
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

// prefixes is the positions on the way to p from its document's root, p's
// among them, of at most most tokens, as nodeview.View.DocumentPath follows at
// most that many. p's pointer is valid, so each '/' past the first character
// begins a token.
func prefixes(p Position, most int) []Position {
	at := []Position{{Doc: p.Doc}}
	for i := 1; i < len(p.Pointer) && len(at) <= most; i++ {
		if p.Pointer[i] == '/' {
			at = append(at, Position{Doc: p.Doc, Pointer: p.Pointer[:i]})
		}
	}
	if p.Pointer != "" && len(at) <= most {
		at = append(at, p)
	}
	return at
}

// past is the walk from, gone on to n at here, which the grammar reads at s:
// a schema, or a position of a document of schemas, is entered, so an $id it
// writes starts the resource the walk is in past it (resources.holding).
func (r resources) past(from walked, n *yaml.Node, s state, here Position) walked {
	out := walked{node: n, s: s, in: from.in, known: from.known}
	if s == sSchema || s == sLoose {
		out.in, out.known = r.entered(from, n, here)
	}
	return out
}

// entered is the resource the walk from is in once it enters the schema n at
// here, and whether that is known.
func (r resources) entered(from walked, n *yaml.Node, here Position) (resource, bool) {
	next, rooted, ok := r.enter(from.in, n)
	if rooted {
		next.root = here
	}
	return next, from.known && ok
}
