package ir

// TypeRef references a TypeDef by ID and records whether this particular usage
// admits null on the wire (ir-design §3.3). Nullability lives on the reference,
// not the target type, because the same type is nullable in one position and not
// another; combined with Property.Required it yields the four distinct
// required/optional × nullable/non-null states.
//
// A TypeRef carries no fact of the target's down with it. What a use site can
// read without resolving Target is only what a compiler already merged onto the
// carrier — a Property's or Parameter's Docs, Deprecation and Default, taken
// from a $ref's target with use-site precedence. Everything else the target
// declares, Constraints above all, is read from the target node itself
// (ir-design §12.2).
type TypeRef struct {
	// Target identifies the referenced TypeDef in Document.Types.
	Target TypeID `json:"target"`
	// Nullable reports that this usage admits null on the wire. Compilers
	// normalize every source spelling to this one bit: OAS 3.0 nullable: true,
	// OAS 3.1 type: [T, "null"], TypeSpec T | null, GraphQL absence-of-!.
	Nullable bool `json:"nullable"`
}
