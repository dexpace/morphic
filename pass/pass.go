package pass

import "github.com/dexpace/morphic/ir"

// Pass is one IR-to-IR step. The engine decides which passes run and in what
// order; a pass knows neither.
//
// Name is stable and unique among an engine's passes, since a caller disables a
// pass by it. Run never mutates doc: a transform returns a new document, which
// may share what it left unchanged, and an analysis returns doc itself. A nil
// result breaks the contract. Run reports only through its diagnostics, as the
// engine owns Document.Diagnostics. A Pass holds no mutable state, so one value
// serves concurrent runs.
//
// Run has no error result; architecture.md §2.2 says why.
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
