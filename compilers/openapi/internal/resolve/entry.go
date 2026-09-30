package resolve

import (
	"encoding/json/jsontext"

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
// writes wins because it describes *this* use of the declaration, and one it
// omits leaves the declaration's value in place. OpenAPI says such a sibling has
// "no effect" where the referenced object type does not allow one, but the IR
// has a docs field at every position this is applied at and dropping a declared
// sibling in silence is the defect the fold exists to fix (ir-design §12.2).
//
// The getters are nil-receiver tolerant, so an inline entry — whose wrapper
// carries no siblings at all — needs no guard and reports nothing.
func RefDocs(ref SiblingDocs, docs ir.Docs) ir.Docs {
	if summary := ref.GetSummary(); summary != "" {
		docs.Summary = summary
	}
	if description := ref.GetDescription(); description != "" {
		docs.Description = description
	}
	return docs
}

// maxRefChain bounds how many $ref hops ObjectAt follows to a declaration
// (styleguide bounded-everything rule). A $ref cycle among the non-schema
// components this walks is refused before lowering by speakeasy's resolver
// rather than by internal/scan, whose pre-lowering refusal covers the cycles
// that resolver does not — and a refused chain resolves to nothing, so ObjectAt
// returns before the loop. The bound therefore only ever fires on an absurd
// alias chain.
//
// Nothing outside this package reads it, so it stays unexported. The test that
// holds the walk to the bound does need it — it has to say "one hop past"
// rather than restate the number — and reaches it through export_test.go.
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
		cand = target
		// obj != nil means the whole chain resolved, so info.Object is non-nil
		// at every hop this loop takes; a nil one would still be safe, since
		// the getters above are nil-receiver tolerant and end the walk.
		ref = R(info.Object)
	}
	return obj, pointer
}
