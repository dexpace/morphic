package lowering

import "github.com/dexpace/morphic/ir"

// NullableWideningHeuristic is the name ir.TypeRef.Inferred carries on a
// reference whose null the nullable-widening policy read in where the document's
// version does not. It is a constant for the reason StreamingMediaTypeHeuristic
// is: an auditor greps for it.
const NullableWideningHeuristic ir.Inferred = "nullable-widening"

// NullableWidening is the nullable-widening policy: whether a null a schema
// spells widens what it is written on rather than conjoining with it, so that
// `nullable: true` admits null for its whole schema wherever it is written, and
// keywords beside a $ref that admit null admit it for the whole reference.
//
// It is a policy because the wider reading guesses at intent (architecture
// principle 6): OpenAPI 3.0 makes `nullable` a member of the `type` beside it,
// 3.1 removed it, and JSON Schema conjoins a $ref with its siblings, yet
// documents written to 3.0.0's wording, or carried to 3.1, mean null is sent.
type NullableWidening struct {
	// Disabled turns the wider reading off: a null then admits only what the
	// document's version says it does, and `nullable` in a schema read as JSON
	// Schema 2020-12 is a keyword that dialect does not define.
	Disabled bool `json:"disabled,omitzero"`
}

// Honours reports whether the policy is on, so that a null a schema spells
// widens what it is written on.
func (p NullableWidening) Honours() bool { return !p.Disabled }
