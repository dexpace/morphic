// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package resolve

import (
	"github.com/dexpace/morphic/compilers/openapi/internal/bind"
	"github.com/dexpace/morphic/compilers/openapi/internal/bind/typed"
)

// Referenced is what Object reads of a reference-or-inline wrapper, each soa
// "Referenced*" alias and ReferencedMediaType alike: the object an inline one
// holds. Whether it is a reference, and where its chain ends, the bindings say
// (typed.Bindings.Reference), never the wrapper, whose resolution the compiler
// does not use (docs/compilers/openapi.md#references).
type Referenced[T any] interface {
	GetObject() *T
}

// Object returns what the reference-or-inline wrapper ref stands for: the
// object the chain of the Reference Object written where ref was read ends on,
// in whichever document, or ref's own object where the table reads no $ref
// there. It is nil for a nil ref and for a chain that does not resolve. The *S
// term constrains R to a pointer type so `ref == nil` is legal in generic code.
func Object[T, S any, R interface {
	*S
	Referenced[T]
}](b *typed.Bindings, ref R) *T {
	obj, _ := objectAt[T, S](b, ref)
	return obj
}

// ObjectAt is Object with the position declaring what it returns, and use for
// an inline entry, which declares its own object where it is written. It notes
// in scope.Reached each position of the source the chain passes, its end
// included: the declarations the IR carries through their uses.
func ObjectAt[T, S any, R interface {
	*S
	Referenced[T]
}](scope Scope, ref R, use bind.Position) (*T, bind.Position) {
	obj, r := objectAt[T, S](scope.Bindings, ref)
	if obj == nil || r == nil {
		return obj, use
	}
	for _, hop := range Hops[T, S](scope.Bindings, ref) {
		scope.Reached.noteSource(hop.At)
	}
	scope.Reached.noteSource(r.Chain.End)
	return obj, r.Chain.End
}

// objectAt is Object with the table's entry for ref's Reference Object, or nil
// for an inline entry.
func objectAt[T, S any, R interface {
	*S
	Referenced[T]
}](b *typed.Bindings, ref R) (*T, *bind.Reference) {
	if ref == nil {
		return nil, nil
	}
	r, ok := b.Reference(ref)
	if !ok {
		return ref.GetObject(), nil
	}
	if r.Chain.Status != bind.Resolved {
		return nil, &r
	}
	end, ok := b.ObjectAt(r.Chain.End, r.Kind).(R)
	if !ok {
		return nil, &r
	}
	return end.GetObject(), &r
}
