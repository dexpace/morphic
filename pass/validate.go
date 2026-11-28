// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package pass

import (
	"context"

	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/ir/irfacts"
)

var _ Pass = Validator{}

// ValidateName is the stable name of the [Validator] pass.
const ValidateName = "validate"

// Validator is [Validate] as a [Pass]. It is an analysis, so Run hands back the
// document it was given, and it holds no state, so it is safe for
// concurrent use.
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
	return validate(ctx, doc, ir.WalkValuesContext)
}

// walkFunc is how validate walks the document: ir.WalkValuesContext, unless a
// test is counting the walks or cutting one short.
type walkFunc func(ctx context.Context, root any, path string, visitors ...ir.Visitor) (bool, error)

// validate is Validate walking the document with walk.
func validate(ctx context.Context, doc *ir.Document, walk walkFunc) ([]ir.Diagnostic, error) {
	if doc == nil {
		return nil, nil
	}
	doc, refused := withoutEmptyKeys(doc)
	// One walk reads what the reference, PropertyID and parameter checks need,
	// and asks ctx as it goes. checkParams leaves to the dangling check exactly
	// what it reports.
	read, err := walkDocument(ctx, doc, walk)
	if err != nil {
		return nil, err
	}
	// One index for the run: a discriminator's routes and tag, and a part's
	// encoding key, are walked while that is cheap and answered from the document
	// indexed once after that (#850).
	facts := irfacts.New(doc)
	diags := append(make([]ir.Diagnostic, 0, 8), refused...)
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

// withoutEmptyKeys returns doc, or, where a registry files a node under the
// empty key, an edit of it filing nothing there, with what the edit refused,
// which names a bug here. The empty key is no node's ID, so the entry is
// irverify's to report (ir/empty-<noun>-id), and read here an empty reference,
// which names nothing, would resolve to it.
func withoutEmptyKeys(doc *ir.Document) (*ir.Document, []ir.Diagnostic) {
	_, types := doc.Types[""]
	_, channels := doc.Channels[""]
	_, messages := doc.Messages[""]
	_, auth := doc.Auth[""]
	_, servers := doc.Servers[""]
	if !types && !channels && !messages && !auth && !servers {
		return doc, nil
	}
	edit := ir.NewEditor(doc)
	if types {
		edit.RemoveType("")
	}
	if channels {
		edit.RemoveChannel("")
	}
	if messages {
		edit.RemoveMessage("")
	}
	if auth {
		edit.RemoveAuthScheme("")
	}
	if servers {
		edit.RemoveServer("")
	}
	return edit.Finish()
}

// families returns validate's rule families over doc and what its walk read,
// in the order they report. Validate asks its context between two, so a cancel
// waits out one family at most.
//
// A family allocates only for a diagnostic, bar the model arranged flattens.
// It sorts and keys collections in a list and a map it keeps for the run, so a
// value with nothing wrong costs it nothing but a step those or a log may grow
// by. The index it asks, facts, allocates per index it builds and not per type
// it indexes (GitHub #1150).
func families(doc *ir.Document, read walked, facts *irfacts.DocumentFacts) []func() []ir.Diagnostic {
	// Sorted once for the run, and written by no family.
	live := liveTypeIDs(doc)
	// A family that ranges a map sorts its keys first. A new family takes a row of
	// twoFindings in validate_determinism_test.go, which holds it to one order.
	return []func() []ir.Diagnostic{
		func() []ir.Diagnostic { return checkNilTypes(doc) },
		func() []ir.Diagnostic { return checkDanglingRefs(read.refs, read.truncated) },
		func() []ir.Diagnostic { return checkEmptyRefs(read.empty) },
		func() []ir.Diagnostic { return checkGroupWalkTruncated(facts, read.truncated) },
		func() []ir.Diagnostic { return checkResponseIndices(doc) },
		func() []ir.Diagnostic { return checkEncodingKeys(doc, facts) },
		func() []ir.Diagnostic { return checkCatchAllEncodingKeys(doc) },
		func() []ir.Diagnostic { return checkPayloadRequired(doc) },
		func() []ir.Diagnostic { return checkPropertyIDRefs(read.properties, read.refs, read.truncated) },
		func() []ir.Diagnostic { return checkDiscriminators(doc, live, facts) },
		func() []ir.Diagnostic { return checkRepeatedVariants(doc, live) },
		func() []ir.Diagnostic { return checkDuplicateWireNames(doc, live) },
		func() []ir.Diagnostic { return checkAmbiguousAliases(doc, live) },
		func() []ir.Diagnostic { return checkDuplicateMemberIDs(doc, live) },
		func() []ir.Diagnostic { return checkParams(doc, read.refs) },
		func() []ir.Diagnostic { return checkIdempotency(doc) },
		func() []ir.Diagnostic { return checkMessageBindings(doc) },
		func() []ir.Diagnostic { return checkOneWay(doc) },
		func() []ir.Diagnostic { return checkDefaultResponses(doc) },
		func() []ir.Diagnostic { return checkArgsOutsideGraphQL(doc, live) },
		func() []ir.Diagnostic { return checkBounds(doc, live) },
		func() []ir.Diagnostic { return checkValues(doc, live) },
		func() []ir.Diagnostic { return checkCompositionCyclesAndInfiniteTypes(doc, live) },
	}
}

// walked is what one walk of a document reads for the checks that need it: the
// references that may dangle with the identities they resolve against, its
// PropIDs, the references it holds empty, and whether the walk was cut short.
type walked struct {
	refs       *ir.IDReferences
	properties *propertyIDSites
	empty      *ir.EmptyReferences
	truncated  bool
}

// walkDocument reads doc in one walk, taken with walk under ctx, and returns
// ctx's error alone if the walk stopped at it.
func walkDocument(ctx context.Context, doc *ir.Document, walk walkFunc) (walked, error) {
	refs := ir.NewIDReferences(doc)
	walkedValue := walked{refs: refs, properties: &propertyIDSites{refs: refs}, empty: &ir.EmptyReferences{}}
	truncated, err := walk(ctx, doc, ir.DocumentPath, walkedValue.refs, walkedValue.properties, walkedValue.empty)
	if err != nil {
		return walked{}, err
	}
	walkedValue.truncated = truncated
	return walkedValue, nil
}
