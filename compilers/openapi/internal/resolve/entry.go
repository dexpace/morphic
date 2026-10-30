// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package resolve

import (
	"encoding/json/jsontext"

	"github.com/speakeasy-api/openapi/references"
)

// Referenced is the method set every soa "Referenced*" alias exposes: it is
// the generic form of speakeasy's Reference[T, V, C], which underlies each of
// them, ReferencedPathItem and ReferencedSecurityScheme alike; ReferenceEnd
// lists them all. Naming the shape once here lets Object stand in for what
// would otherwise be one resolver per aliased type. S is the reference's own
// type, Reference[T, V, C]; ObjectAt walks it through
// GetReferenceResolutionInfo to follow a chain of component aliases.
type Referenced[T, S any] interface {
	GetObject() *T
	GetObjectAny() any
	GetResolvedObject() *T
	GetReference() references.Reference
	GetReferenceResolutionInfo() *references.ResolveResult[S]
}

// Object returns the concrete value of a reference-or-inline entry,
// preferring the inline object and falling back to the resolved target. The
// *S term constrains R to a pointer type so `ref == nil` is legal in generic
// code; interfaces.Validator[T] is unavailable here (it lives in the
// library's internal/ tree), which is why R is expressed via *S rather than
// V directly.
func Object[T, S any, R interface {
	*S
	Referenced[T, S]
}](ref R) *T {
	if ref == nil {
		return nil
	}
	if obj := ref.GetObject(); obj != nil {
		return obj
	}
	// GetResolvedObject is itself nil-safe and delegates to GetObject, but the
	// fallback stays explicit rather than coupling this compiler to that
	// undocumented nil-tolerance.
	return ref.GetResolvedObject()
}

// ObjectAt returns a reference-or-inline entry's concrete value together
// with the pointer of the declaration it resolves to, walking through any
// chained component aliases to the last one written in this document (issue
// #107). usePtr — the entry's own position — stands whenever the chain has no
// declaration addressable here: an inline entry, a reference that leaves this
// document, or one that outruns maxRefChain. An alias pointer is never
// returned on its own, since a one-key $ref object has no children to hoist,
// but scope.Reached records every hop in the source. A hop scope.refuses leaves
// the chain unresolved and unrecorded, as a failure does.
func ObjectAt[T, S any, R interface {
	*S
	Referenced[T, S]
}](scope Scope, ref R, usePtr jsontext.Pointer) (*T, jsontext.Pointer) {
	obj := Object[T, S, R](ref)
	if obj == nil {
		return nil, usePtr
	}
	// cand advances one hop at a time but is adopted only once the chain ends
	// at a declaration written here, which is what keeps a chain that exits the
	// document from returning the last alias it passed through.
	pointer, cand := usePtr, usePtr
	// hops are noted in scope.Reached only once no hop is refused: a refused
	// chain lowers nothing, so a component only it passes through stays
	// unreached, for the compiler to keep verbatim.
	var hops [maxRefChain]jsontext.Pointer
	n := 0
	for range maxRefChain {
		// GetReferenceResolutionInfo is nil once the chain reaches a
		// non-reference entry — the terminator, and why an inline entry
		// exits on the first pass (couples this to that library contract).
		info := ref.GetReferenceResolutionInfo()
		if info == nil {
			pointer = cand
			break
		}
		target, ok := scope.InternalPointer(ref.GetReference().String())
		if !ok {
			break // another document: nothing addressable here, so usePtr stands
		}
		if scope.refuses(target, found(info.Object)) {
			return nil, usePtr // the resolver read past a field written there; load reported it
		}
		hops[n], n = target, n+1
		// The resolver reads the next hop against the document this one
		// resolved against, the source, even where this one was written in
		// another document's content or its pointer passed a $ref into one.
		cand, scope = target, scope.InSource()
		// obj != nil means the whole chain resolved, so info.Object is non-nil
		// at every hop this loop takes; a nil one would still be safe, since
		// the getters above are nil-receiver tolerant and end the walk.
		ref = R(info.Object)
	}
	for _, hop := range hops[:n] {
		scope.Reached.note(hop)
	}
	return obj, pointer
}

// found returns obj as the answer Scope.refuses reads, and nil for a nil one,
// which as a typed nil would reach a getter that does not tolerate it.
func found[S any](obj *S) any {
	if obj == nil {
		return nil
	}
	return obj
}
