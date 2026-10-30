// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package resolve

import (
	"github.com/dexpace/morphic/compilers/openapi/internal/fragment"
)

// Hop is one reference a chain passes through after its entry, and whether
// another document holds it, as the hop before it found it.
type Hop[R any] struct {
	Ref     R
	Foreign bool
}

// Chained reports whether the chain of ref passes through a reference after ref
// itself, which Hops would then list. It reads no scope, so a caller can build
// the one Hops reads only for a chain that has a hop.
func Chained[T, S any, R interface {
	*S
	Referenced[T, S]
	IsReference() bool
}](ref R) bool {
	info := ref.GetReferenceResolutionInfo()
	return info != nil && info.Object != nil && R(info.Object).IsReference()
}

// Hops returns the references the chain of ref passes through after ref itself,
// nearest first: each entry its resolution recorded that is a reference too,
// read from scope, the scope ref is read in, as ScopeOf reads a chain's end.
// The entry the chain ends at is no hop, and neither an inline entry nor one
// that resolved nothing has any. Past maxRefChain entries the rest are not read.
func Hops[T, S any, R interface {
	*S
	Referenced[T, S]
	IsReference() bool
}](scope Scope, ref R) []Hop[R] {
	var hops []Hop[R]
	// The entry the chain ends at is no hop, so its scope is not read.
	for range maxRefChain {
		if !Chained[T, S](ref) {
			break
		}
		info := ref.GetReferenceResolutionInfo()
		next := R(info.Object)
		scope = scope.reached(End{Document: info.ResolvedDocument, Path: info.AbsoluteDocumentPath,
			Pointer: fragment.Pointer(string(ref.GetReference()))})
		ref = next
		hops = append(hops, Hop[R]{Ref: ref, Foreign: scope.Foreign})
	}
	return hops
}
