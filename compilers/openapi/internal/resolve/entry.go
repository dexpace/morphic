package resolve

import (
	"encoding/json/jsontext"

	oas3 "github.com/speakeasy-api/openapi/jsonschema/oas3"
	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/speakeasy-api/openapi/references"

	"github.com/dexpace/morphic/ir"
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

// SiblingDocs is the method set a speakeasy Reference wrapper exposes for the
// summary and description a document may write beside a `$ref`: every
// Referenced* alias is Reference[T, V, C], whose Populate reads these two only
// when the entry actually holds a reference, so an inline entry reports both
// empty whatever the object itself declares.
//
// It is exported because the sites that fold the pair over a declaration's docs
// are a layer up, and one of them passes the wrapper into a shared lowering
// rather than applying the fold itself.
type SiblingDocs interface {
	GetSummary() string
	GetDescription() string
}

// RefDocs folds the summary and description written beside a `$ref` over the
// docs the resolved declaration carries, field by field: a sibling the entry
// writes wins because it describes *this* use, and one it omits leaves the
// declaration's value. OpenAPI gives such a sibling "no effect" where the type
// disallows one, but the IR has a docs field at every position this applies
// to, and dropping it silently is the defect fixed here (ir-design §12.2).
//
// The getters are nil-receiver tolerant, so an inline entry needs no guard.
func RefDocs(ref SiblingDocs, docs ir.Docs) ir.Docs {
	if summary := ref.GetSummary(); summary != "" {
		docs.Summary = summary
	}
	if description := ref.GetDescription(); description != "" {
		docs.Description = description
	}
	return docs
}

// maxRefChain bounds how many $ref hops ObjectAt follows to a declaration, and
// how many a chain's end (endOf) and a pointer's scope (Scope.reached) are
// followed through. A $ref cycle among the non-schema components it walks is
// refused before lowering by speakeasy's resolver, not by internal/scan, and a
// refused chain resolves to nothing, so the bound only fires on an absurd alias
// chain. Past it, ObjectAt keeps the use site, and what the chain reaches is
// read in no document's scope.
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
		// The resolver reads the next hop against the document this one
		// resolved against, the source, even where this one was written in
		// another document's content or its pointer passed a $ref into one.
		cand, scope = target, scope.InSource()
		// obj != nil means the whole chain resolved, so info.Object is non-nil
		// at every hop this loop takes; a nil one would still be safe, since
		// the getters above are nil-receiver tolerant and end the walk.
		ref = R(info.Object)
	}
	return obj, pointer
}

// ScopeOf returns the scope what the entry ref resolves to is read in, which is
// where the last hop of its chain found it (see reached): a Foreign scope over
// the document the resolver read it by, or, when that hop resolved against the
// source, the scope the position it names there is read in (Scope.At). An
// inline entry, and one that resolved nothing, are read in scope itself.
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

// chained is what endOf reads of a reference: the one it is written as, and
// what its resolution recorded. A schema holds both, though it is no Referenced.
type chained[S any] interface {
	GetReference() references.Reference
	GetReferenceResolutionInfo() *references.ResolveResult[S]
}

// endOf is EndOf for any reference, a schema's included. A chain still going
// after maxRefChain hops ends nowhere it read, so it is cut, not taken to end at
// the last hop read: that hop's document need not be the one holding its end.
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
		Pointer: jsontext.Pointer(written.GetJSONPointer())}, true
}

// ReferenceEnd is EndOf for a reference of each of the library's kinds, a
// schema's included, which a walk through the document meets as values of any
// type, and false for any other value. It is the one place this package lists
// them, for the readers of a chain that cannot name its kind: the load phase
// and the lowering each ask where the walk to a pointer passes into another
// document.
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
	case *oas3.JSONSchema[oas3.Referenceable]:
		return endOf[oas3.JSONSchema[oas3.Referenceable]](r)
	default:
		return End{}, false
	}
}
