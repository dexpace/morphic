package resolve

import (
	"encoding/json/jsontext"
	"slices"

	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/speakeasy-api/openapi/references"

	"github.com/dexpace/morphic/compilers/openapi/internal/fragment"
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

// siblingDocs is what a speakeasy Reference wrapper exposes of the summary and
// description written beside its `$ref`. Populate reads the two only for an
// entry that holds a reference, so an inline entry reports both empty.
type siblingDocs interface {
	GetSummary() string
	GetDescription() string
}

// UseDocs folds the summary and description written beside each `$ref` of ref's
// chain over docs, those of the declaration the chain ends at. It folds from the
// declaration outward, field by field, so the use site's own siblings win, an
// alias's beat what it names, and a field a hop omits leaves the one beneath it
// standing (GitHub #610). OpenAPI gives such a sibling "no effect" where the
// object type allows none, but the IR has a docs field at each position this
// serves, so it is kept rather than dropped
// (docs/compilers/openapi.md#reference-object-siblings).
func UseDocs[T, S any, R interface {
	*S
	Referenced[T, S]
	siblingDocs
}](ref R, docs ir.Docs) ir.Docs {
	hops := make([]R, 0, 1)
	for range maxRefChain {
		if ref == nil {
			break
		}
		hops = append(hops, ref)
		info := ref.GetReferenceResolutionInfo()
		if info == nil {
			break
		}
		ref = R(info.Object)
	}
	for _, hop := range slices.Backward(hops) {
		if summary := hop.GetSummary(); summary != "" {
			docs.Summary = summary
		}
		if description := hop.GetDescription(); description != "" {
			docs.Description = description
		}
	}
	return docs
}

// maxRefChain bounds how many $ref hops ObjectAt and UseDocs follow, and how
// many a chain's end (endOf) and a pointer's scope (Scope.reached) are followed
// through. A $ref cycle among the non-schema components is reported by the
// resolver (docs/parser-library.md#reference-object-cycle), not refused by
// internal/scan, and a failed chain resolves to nothing, so the bound only
// fires on an absurd alias chain. Past it, ObjectAt keeps the use site, and
// what the chain reaches is read in no document's scope.
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
