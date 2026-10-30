// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package resolve

import (
	"encoding/json/jsontext"

	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/speakeasy-api/openapi/references"

	"github.com/dexpace/morphic/compilers/openapi/internal/fragment"
)

// ScopeOf returns the scope what the entry ref resolves to is read in, which is
// where the last hop of its chain found it (see reached): a Foreign scope over
// the document the resolver read it by, or, when that hop resolved against the
// source, the scope the position it names there is read in, past any $ref the
// walk to it passes. An inline entry, and one that resolved nothing, are read
// in scope itself.
func ScopeOf[T, S any, R interface {
	*S
	Referenced[T, S]
}](scope Scope, ref R) Scope {
	end, ok := EndOf[T, S](ref)
	if !ok {
		return scope
	}
	return scope.reached(end)
}

// End is where the last hop of a reference's chain resolved: the document it
// resolved against, the path the resolver read that document by, and the
// position its pointer names there. One with no Document is a chain cut at
// maxRefChain, whose end was never read.
type End struct {
	Document any
	Path     string
	Pointer  jsontext.Pointer
}

// EndOf returns where the chain of the entry ref ends, and false for an inline
// entry and for one that resolved nothing.
func EndOf[T, S any, R interface {
	*S
	Referenced[T, S]
}](ref R) (End, bool) {
	return endOf[S](ref)
}

// chained is what endOf reads of a reference object: the one it is written as,
// and what its resolution recorded. A schema has no object of its own to name
// (GetObjectAny), so none is chained: the compiler binds a schema's $ref
// itself, and the library's resolution of one is never read.
type chained[S any] interface {
	GetReference() references.Reference
	GetReferenceResolutionInfo() *references.ResolveResult[S]
	GetObjectAny() any
}

// endOf is EndOf for a reference object of any kind. A chain still going after
// maxRefChain hops ends nowhere it read, so it is cut, not taken to end at the
// last hop read: that hop's document need not be the one holding its end.
func endOf[S any, R interface {
	*S
	chained[S]
}](ref R) (End, bool) {
	var last *references.ResolveResult[S]
	var written references.Reference
	for range maxRefChain {
		info := ref.GetReferenceResolutionInfo()
		if info == nil || info.Object == nil {
			break
		}
		last, written, ref = info, ref.GetReference(), R(info.Object)
	}
	if last == nil {
		return End{}, false
	}
	if info := ref.GetReferenceResolutionInfo(); info != nil && info.Object != nil {
		return End{}, true
	}
	return End{Document: last.ResolvedDocument, Path: last.AbsoluteDocumentPath,
		Pointer: fragment.Pointer(string(written))}, true
}

// ReferenceEnd is EndOf for a reference object of each of the library's kinds,
// which a walk through the document meets as values of any type, and false for
// any other value, a schema included. It is the one place this package lists
// them, for the readers of a chain that cannot name its kind: the lowering
// asks where the walk to a pointer passes into another document.
func ReferenceEnd(node any) (End, bool) {
	switch r := node.(type) {
	case *soa.ReferencedPathItem:
		return endOf[soa.ReferencedPathItem](r)
	case *soa.ReferencedParameter:
		return endOf[soa.ReferencedParameter](r)
	case *soa.ReferencedHeader:
		return endOf[soa.ReferencedHeader](r)
	case *soa.ReferencedRequestBody:
		return endOf[soa.ReferencedRequestBody](r)
	case *soa.ReferencedResponse:
		return endOf[soa.ReferencedResponse](r)
	case *soa.ReferencedExample:
		return endOf[soa.ReferencedExample](r)
	case *soa.ReferencedLink:
		return endOf[soa.ReferencedLink](r)
	case *soa.ReferencedCallback:
		return endOf[soa.ReferencedCallback](r)
	case *soa.ReferencedSecurityScheme:
		return endOf[soa.ReferencedSecurityScheme](r)
	default:
		return End{}, false
	}
}
