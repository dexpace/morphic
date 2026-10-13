package pass

import "github.com/dexpace/morphic/ir"

// Pass is one IR-to-IR step, which the engine alone schedules.
//
// Name is stable and unique among an engine's passes, since a caller disables a
// pass by it. Run never mutates doc and never returns nil: a transform returns a
// new document, which may share what it left unchanged, and an analysis returns
// doc itself. A transform may append to Document.Sources but keeps each entry at
// its index. Run reports only through its diagnostics, as the engine owns
// Document.Diagnostics. A Pass holds no mutable state, so one value serves
// concurrent runs.
//
// architecture.md §2.2 says why Run has no context or error.
type Pass interface {
	Name() string
	Run(doc *ir.Document) (*ir.Document, []ir.Diagnostic)
}

// ValidateName is the stable name of the [Validator] pass.
const ValidateName = "validate"

// Validator is [Validate] as a [Pass]. It is an analysis, so Run hands back the
// document it was given.
type Validator struct{}

// Name returns [ValidateName].
func (Validator) Name() string { return ValidateName }

// Run returns doc unchanged, with what [Validate] reports about it.
func (Validator) Run(doc *ir.Document) (*ir.Document, []ir.Diagnostic) {
	return doc, Validate(doc)
}
