// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package resolve

import (
	"github.com/dexpace/morphic/compilers/openapi/internal/bind"
	"github.com/dexpace/morphic/compilers/openapi/internal/bind/typed"
)

// Hop is one reference a chain passes after its entry: the wrapper read where
// it is written, and that position, in whichever document.
type Hop[R any] struct {
	Ref R
	At  bind.Position
}

// Hops returns the references the chain of ref passes after ref itself,
// nearest first, as the bindings' table follows it: each position a hop lands
// on that writes a $ref of its own. The position the chain ends at is no hop,
// and neither an inline entry nor one whose chain does not resolve has any. It
// follows as many hops as the chain counts (bind.Chain.Hops), so a loop, which
// resolves nowhere, is never followed.
func Hops[T, S any, R interface {
	*S
	Referenced[T]
}](b *typed.Bindings, ref R) []Hop[R] {
	r, ok := b.Reference(ref)
	if !ok || r.Chain.Status != bind.Resolved {
		return nil
	}
	var hops []Hop[R]
	for range r.Chain.Hops - 1 {
		at := r.Hop.Target
		next, ok := b.Table().Reference(at, r.Kind)
		wrapper, held := b.ObjectAt(at, r.Kind).(R)
		if !ok || !held {
			break
		}
		hops = append(hops, Hop[R]{Ref: wrapper, At: at})
		r = next
	}
	return hops
}
