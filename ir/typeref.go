package ir

// TypeRef references a TypeDef by ID and records whether this usage admits null
// on the wire (ir-design §3.3). Nullability lives on the reference, not the
// target, since one type may be nullable in one position and not another; with
// Property.Required it yields the four required/optional × nullable/non-null
// states.
//
// A TypeRef carries nothing of its target. Without resolving Target, a use site
// can read only what a compiler merged onto the carrier from a $ref's target,
// with use-site precedence: a Property's or Parameter's Docs, Deprecation and
// Default, and a Property's Visibility. Everything else, Constraints above all,
// is read from the target node (ir-design §12.2).
type TypeRef struct {
	// Target identifies the referenced TypeDef in Document.Types. It is never
	// empty: a position that admits no type holds a nil *TypeRef, so a TypeRef
	// naming nothing is one a lowering left unfilled, and irverify reports it as
	// ir/type-ref-no-target.
	Target TypeID `json:"target"`
	// Nullable reports that this usage admits null on the wire. Compilers
	// normalize every source spelling to this one bit: OAS 3.0 nullable: true,
	// OAS 3.1 type: [T, "null"], TypeSpec T | null, GraphQL absence-of-!.
	Nullable bool `json:"nullable"`
}
