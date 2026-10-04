package resolve

import (
	"encoding/json/jsontext"

	"github.com/speakeasy-api/openapi/references"
)

// Referenced is the method set every soa "Referenced*" alias exposes: it
// is the generic form of speakeasy's Reference[T, V, C], which underlies
// ReferencedPathItem, ReferencedResponse, ReferencedHeader, ReferencedCallback,
// ReferencedParameter, ReferencedRequestBody, ReferencedExample, and
// ReferencedSecurityScheme alike. Naming the shape once here lets Object stand
// in for what would otherwise be one resolver per aliased type. S is the
// reference's own type, Reference[T, V, C]; ObjectAt walks it through
// GetReferenceResolutionInfo to follow a chain of component aliases.
type Referenced[T, S any] interface {
	GetObject() *T
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

// maxRefChain bounds how many $ref hops ObjectAt follows to a declaration. A
// $ref cycle among the non-schema components it walks is refused before
// lowering by speakeasy's resolver, not by internal/scan, and a refused chain
// resolves to nothing, so the bound only fires on an absurd alias chain.
//
// It stays unexported: only the test that holds the walk to the bound needs it,
// and reaches it through export_test.go.
const maxRefChain = 32

// ObjectAt returns a reference-or-inline entry's concrete value together
// with the pointer of the declaration it resolves to, walking through any
// chained component aliases to the last one written in this document (issue
// #107). usePtr — the entry's own position — stands whenever the chain has no
// declaration addressable here: an inline entry, a reference that leaves this
// document, or one that outruns maxRefChain. An alias pointer is never
// returned on its own, since a one-key $ref object has no children to hoist.
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
		// The next hop is written at target, which is the source's own content
		// even when this one was written in another document's.
		cand, scope = target, scope.InSource()
		// obj != nil means the whole chain resolved, so info.Object is non-nil
		// at every hop this loop takes; a nil one would still be safe, since
		// the getters above are nil-receiver tolerant and end the walk.
		ref = R(info.Object)
	}
	return obj, pointer
}

// ScopeOf returns the scope what the entry ref resolves to is read in, which is
// where the last hop of its chain found it: the source's own (Scope.InSource)
// when that is the source, and otherwise a Foreign scope over the document the
// resolver read it by. An inline entry, and one that resolved nothing, are read
// in scope itself.
func ScopeOf[T, S any, R interface {
	*S
	Referenced[T, S]
}](scope Scope, ref R) Scope {
	var last *references.ResolveResult[S]
	for range maxRefChain {
		info := ref.GetReferenceResolutionInfo()
		if info == nil || info.Object == nil {
			break
		}
		last, ref = info, R(info.Object)
	}
	switch {
	case last == nil:
		return scope
	case last.ResolvedDocument == any(scope.Doc):
		return scope.InSource()
	default:
		scope.Foreign, scope.Holder = true, last.AbsoluteDocumentPath
		return scope
	}
}
