package pass

import (
	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/ir/irfacts"
)

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

// Validate checks a Document's referential integrity and the well-formedness
// rules the IR states, and returns the diagnostics it finds, most-structural
// first. It is pure: it never mutates doc and holds no package-level state. An
// empty result means the document is internally consistent for every rule this
// pass enforces.
//
// Purity makes concurrent calls safe, including several over one document — but
// Validate takes no lock, so nothing may be mutating that document meanwhile.
// engine.Run and RunSources satisfy this by validating a document no other run
// can reach; RunDocument leaves it to its caller.
func Validate(doc *ir.Document) []ir.Diagnostic {
	if doc == nil {
		return nil
	}
	// Read once: the dangling walk resolves against these, and checkParams
	// leaves to it exactly what it reports.
	decls, declTruncated := ir.DeclaredIDs(doc)
	// One index for the run: a discriminator's routes and tag, and a part's
	// encoding key, are walked while that is cheap and answered from the document
	// indexed once after that (#850).
	facts := irfacts.New(doc)
	diags := make([]ir.Diagnostic, 0, 8)
	diags = append(diags, checkNilTypes(doc)...)
	diags = append(diags, checkDanglingRefs(doc, decls, declTruncated)...)
	diags = append(diags, checkGroupWalkTruncated(doc)...)
	diags = append(diags, checkServerIndices(doc)...)
	diags = append(diags, checkResponseIndices(doc)...)
	diags = append(diags, checkEncodingKeys(doc, facts)...)
	diags = append(diags, checkPayloadRequired(doc)...)
	diags = append(diags, checkPropIDRefs(doc)...)
	diags = append(diags, checkDiscriminators(doc, facts)...)
	diags = append(diags, checkDuplicateWireNames(doc)...)
	diags = append(diags, checkAmbiguousAliases(doc)...)
	diags = append(diags, checkDuplicateMemberIDs(doc)...)
	diags = append(diags, checkParams(doc, decls)...)
	diags = append(diags, checkIdempotency(doc)...)
	diags = append(diags, checkMessageBindings(doc)...)
	diags = append(diags, checkOneWay(doc)...)
	diags = append(diags, checkDefaultResponses(doc)...)
	diags = append(diags, checkArgsOutsideGraphQL(doc)...)
	return diags
}
