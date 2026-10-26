package engine

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/dexpace/morphic/barrier"
	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/pass"
)

// errNoDocument reports a pass that broke its contract by returning no
// document. Keeping the previous one would hide the bug, and stopping without
// one would report the spec as refused.
var errNoDocument = errors.New("returned no document")

// errVersionChanged reports a pass that broke its contract by returning a
// document not stamped with the IR version this build reads. Whatever consumes
// it next would read it as a document of another generation.
var errVersionChanged = errors.New("stamped the document with another IR version")

// errSourcesMoved reports a pass that broke its contract by dropping, moving or
// replacing an entry of Document.Sources. The findings the engine carries over,
// and the IR's own provenance, index that table, so the Result would send them
// to the wrong file.
var errSourcesMoved = errors.New("moved, replaced or dropped an entry of Document.Sources")

// errDiagnosticsChanged reports a pass that broke its contract by putting
// findings on Document.Diagnostics, which the engine owns and writes once the
// passes have run. Whatever the pass stored there would be dropped unreported.
var errDiagnosticsChanged = errors.New("changed Document.Diagnostics, which the engine owns")

// errSourceOutOfRange reports a pass that broke its contract with a diagnostic
// indexing no entry of Document.Sources, which the Result promises every one it
// carries does (Result.Sources).
var errSourceOutOfRange = errors.New("reported a diagnostic indexing no source")

// runPasses runs passes in order, each over the document the one before it
// returned, and returns the last document with every diagnostic they reported.
// A pass takes no context, so ctx is checked before each one: a cancelled run
// stops at the next pass rather than running the rest, and a pass that
// outlasts its context holds the run until it returns.
//
// sum is the fingerprint of doc when the plan verifies, which each pass's
// result is then held to.
func runPasses(ctx context.Context, doc *ir.Document, sum fingerprint, p plan) (*ir.Document, []ir.Diagnostic, error) {
	var found []ir.Diagnostic
	for _, np := range p.passes {
		if err := ctx.Err(); err != nil {
			return nil, nil, fmt.Errorf("engine: stop before pass %q: %w", np.name, err)
		}
		next, diags, err := np.run(doc)
		if err != nil {
			return nil, nil, err
		}
		if sum, err = p.afterPass(np, sum, doc, next); err != nil {
			return nil, nil, err
		}
		doc, found = next, append(found, diags...)
	}
	return doc, found, nil
}

// run calls the pass on doc behind a barrier and holds what it returns to the
// duties of pass.Pass that the two documents and the findings let the engine
// check. A breach is an error naming the pass and wrapping pass.ErrContract.
func (p namedPass) run(doc *ir.Document) (*ir.Document, []ir.Diagnostic, error) {
	var (
		next  *ir.Document
		diags []ir.Diagnostic
	)
	if perr := barrier.Catch(func() { next, diags = p.pass.Run(doc) }); perr != nil {
		return nil, nil, p.breach(panicked("Run", perr))
	}
	if err := checkOutput(doc, next, diags); err != nil {
		return nil, nil, p.breach(err)
	}
	return next, diags, nil
}

// panicked is a panic in one of a pass's methods, as the cause of a breach.
func panicked(method string, perr error) error {
	return fmt.Errorf("%s: %w", method, perr)
}

// breach is err, a duty the pass left unmet, as the error a run fails with.
func (p namedPass) breach(err error) error {
	return fmt.Errorf("engine: run pass %q: %w: %w", p.name, err, pass.ErrContract)
}

// checkOutput holds next, the document a pass returned for prev, and diags, its
// findings, to the duties the engine can read off them: a document, stamped as
// this build reads, whose source table keeps every entry of prev's and whose
// findings are the engine's own, and findings that index that table.
func checkOutput(prev, next *ir.Document, diags []ir.Diagnostic) error {
	if next == nil {
		return errNoDocument
	}
	if !ir.CompatibleVersion(next.IRVersion) {
		return fmt.Errorf("irVersion %q, not %q: %w", next.IRVersion, ir.IRVersion, errVersionChanged)
	}
	if !keepsSources(prev.Sources, next.Sources) {
		return errSourcesMoved
	}
	// A document built afresh leaves Diagnostics empty, which the engine fills;
	// one that carries any carries what it was given.
	if len(next.Diagnostics) > 0 && !slices.Equal(prev.Diagnostics, next.Diagnostics) {
		return errDiagnosticsChanged
	}
	if d, outside := ir.FirstOutsideTable(diags, len(next.Sources)); outside {
		return fmt.Errorf("%s at source %d of %d: %w", d.Code, d.Provenance.Source, len(next.Sources), errSourceOutOfRange)
	}
	return nil
}

// keepsSources reports whether next holds every entry of prev at the index prev
// gives it, which is all a Provenance.Source already made needs.
func keepsSources(prev, next []ir.SourceInfo) bool {
	return len(next) >= len(prev) && slices.Equal(prev, next[:len(prev)])
}

// withFindings returns a copy of doc whose Diagnostics are diags. The engine
// writes the one field it owns into a copy, never into the document a stage
// handed it: a compiler may keep the document it returned, and runs overlap.
func withFindings(doc *ir.Document, diags []ir.Diagnostic) *ir.Document {
	own := *doc
	own.Diagnostics = diags
	return &own
}
