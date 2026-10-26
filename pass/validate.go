package pass

import (
	"context"

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

// Run returns doc unchanged, with what [Validate] reports about it, or with
// nothing once ctx is done.
func (Validator) Run(ctx context.Context, doc *ir.Document) (*ir.Document, []ir.Diagnostic) {
	diags, err := Validate(ctx, doc)
	if err != nil {
		return doc, nil
	}
	return doc, diags
}

// Validate checks a Document's referential integrity and the well-formedness
// rules the IR states, and returns the diagnostics it finds, most-structural
// first, or ctx's error alone once ctx is done. It is pure: it never mutates doc
// and holds no package-level state. An empty result means the document is
// consistent for every rule this pass enforces.
//
// Purity makes concurrent calls safe, including several over one document — but
// Validate takes no lock, so nothing may be mutating that document meanwhile.
// An engine run that compiles satisfies this by validating a document no other
// run can reach; RunDocument leaves it to its caller.
func Validate(ctx context.Context, doc *ir.Document) ([]ir.Diagnostic, error) {
	if doc == nil {
		return nil, nil
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// Read once: the dangling walk resolves against these, and checkParams
	// leaves to it exactly what it reports.
	decls, declTruncated := ir.DeclaredIDs(doc)
	// One index for the run: a discriminator's routes and tag, and a part's
	// encoding key, are walked while that is cheap and answered from the document
	// indexed once after that (#850).
	facts := irfacts.New(doc)
	diags := make([]ir.Diagnostic, 0, 8)
	for _, family := range families(doc, decls, declTruncated, facts) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		diags = append(diags, family()...)
	}
	// The last family may have been cut short too.
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return diags, nil
}

// families returns validate's rule families over doc, in the order they
// report. Validate asks its context between two, so a cancel waits out one
// family at most.
func families(doc *ir.Document, decls []ir.IDDeclaration, declTruncated bool, facts *irfacts.Index,
) []func() []ir.Diagnostic {
	return []func() []ir.Diagnostic{
		func() []ir.Diagnostic { return checkNilTypes(doc) },
		func() []ir.Diagnostic { return checkDanglingRefs(doc, decls, declTruncated) },
		func() []ir.Diagnostic { return checkGroupWalkTruncated(doc) },
		func() []ir.Diagnostic { return checkServerIndices(doc) },
		func() []ir.Diagnostic { return checkResponseIndices(doc) },
		func() []ir.Diagnostic { return checkEncodingKeys(doc, facts) },
		func() []ir.Diagnostic { return checkPayloadRequired(doc) },
		func() []ir.Diagnostic { return checkPropIDRefs(doc) },
		func() []ir.Diagnostic { return checkDiscriminators(doc, facts) },
		func() []ir.Diagnostic { return checkDuplicateWireNames(doc) },
		func() []ir.Diagnostic { return checkAmbiguousAliases(doc) },
		func() []ir.Diagnostic { return checkDuplicateMemberIDs(doc) },
		func() []ir.Diagnostic { return checkParams(doc, decls) },
		func() []ir.Diagnostic { return checkIdempotency(doc) },
		func() []ir.Diagnostic { return checkMessageBindings(doc) },
		func() []ir.Diagnostic { return checkOneWay(doc) },
		func() []ir.Diagnostic { return checkDefaultResponses(doc) },
		func() []ir.Diagnostic { return checkArgsOutsideGraphQL(doc) },
	}
}
