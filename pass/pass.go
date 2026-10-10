package pass

import "github.com/dexpace/morphic/ir"

// Pass is one IR-to-IR step the engine runs, in an order the engine owns.
//
// Name is stable and unique among the passes an engine composes: it is what a
// caller disables a pass by. Run is pure and never mutates its input. A
// transform returns a new document; an analysis returns its input unchanged
// alongside the diagnostics it found. A nil document is a programmer error in
// the pass, which the engine reports rather than papering over.
type Pass interface {
	Name() string
	Run(doc *ir.Document) (*ir.Document, []ir.Diagnostic)
}

// ValidateName is the stable name of the validate pass.
const ValidateName = "validate"

// validatePass adapts [Validate] to [Pass]. It is analysis-only, so it hands
// its input back untouched.
type validatePass struct{}

// NewValidate returns the validate pass as a [Pass].
func NewValidate() Pass { return validatePass{} }

// Name returns [ValidateName].
func (validatePass) Name() string { return ValidateName }

// Run delegates to [Validate] and returns doc unchanged.
func (validatePass) Run(doc *ir.Document) (*ir.Document, []ir.Diagnostic) {
	return doc, Validate(doc)
}
