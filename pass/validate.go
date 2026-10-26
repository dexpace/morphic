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
	return validate(ctx, doc, ir.WalkValues)
}

// walkFunc is how validate walks the document: ir.WalkValues, unless a test is
// counting the walks.
type walkFunc func(root any, path string, visitors ...ir.Visitor) bool

// validate is Validate walking the document with walk.
func validate(ctx context.Context, doc *ir.Document, walk walkFunc) ([]ir.Diagnostic, error) {
	if doc == nil {
		return nil, nil
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// One walk reads what the reference, PropID and parameter checks need.
	// checkParams leaves to the dangling check exactly what it reports.
	read := walkDocument(doc, walk)
	// One index for the run: a discriminator's routes and tag, and a part's
	// encoding key, are walked while that is cheap and answered from the document
	// indexed once after that (#850).
	facts := irfacts.New(doc)
	diags := make([]ir.Diagnostic, 0, 8)
	for _, family := range families(doc, read, facts) {
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

// families returns validate's rule families over doc and what its walk read,
// in the order they report. Validate asks its context between two, so a cancel
// waits out one family at most.
func families(doc *ir.Document, read walked, facts *irfacts.Index) []func() []ir.Diagnostic {
	return []func() []ir.Diagnostic{
		func() []ir.Diagnostic { return checkNilTypes(doc) },
		func() []ir.Diagnostic { return checkDanglingRefs(read.refs, read.decls, read.truncated) },
		func() []ir.Diagnostic { return checkGroupWalkTruncated(doc) },
		func() []ir.Diagnostic { return checkServerIndices(doc) },
		func() []ir.Diagnostic { return checkResponseIndices(doc) },
		func() []ir.Diagnostic { return checkEncodingKeys(doc, facts) },
		func() []ir.Diagnostic { return checkPayloadRequired(doc) },
		func() []ir.Diagnostic { return checkPropIDRefs(read.props, read.decls) },
		func() []ir.Diagnostic { return checkDiscriminators(doc, facts) },
		func() []ir.Diagnostic { return checkDuplicateWireNames(doc) },
		func() []ir.Diagnostic { return checkAmbiguousAliases(doc) },
		func() []ir.Diagnostic { return checkDuplicateMemberIDs(doc) },
		func() []ir.Diagnostic { return checkParams(doc, read.decls) },
		func() []ir.Diagnostic { return checkIdempotency(doc) },
		func() []ir.Diagnostic { return checkMessageBindings(doc) },
		func() []ir.Diagnostic { return checkOneWay(doc) },
		func() []ir.Diagnostic { return checkDefaultResponses(doc) },
		func() []ir.Diagnostic { return checkArgsOutsideGraphQL(doc) },
	}
}

// walked is what one walk of a document reads for the checks that need it: the
// identities it declares, the references that may dangle, its PropIDs, and
// whether the walk was cut short.
type walked struct {
	decls     ir.IDDeclarations
	refs      *refSites
	props     *propIDSites
	truncated bool
}

// walkDocument reads doc in one walk, taken with walk.
func walkDocument(doc *ir.Document, walk walkFunc) walked {
	w := walked{refs: newRefSites(doc), props: &propIDSites{}}
	w.truncated = walk(doc, ir.DocumentPath, &w.decls, w.refs, w.props)
	return w
}
