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

// runPasses runs the plan's passes in order, each over the document the one before it
// returned, and returns the last document with every diagnostic they reported.
// ctx is asked before each pass and after the last, so a run cancelled during
// a pass or a verification, which reads no context, starts nothing more and
// returns no document. A pass that outlasts its context holds the run until it
// returns.
//
// sum is the fingerprint of doc when the plan verifies, which each pass's
// result is then held to.
func (p plan) runPasses(ctx context.Context, doc *ir.Document, sum fingerprint) (*ir.Document, []ir.Diagnostic, error) {
	var found []ir.Diagnostic
	for _, np := range p.passes {
		if err := ctx.Err(); err != nil {
			return nil, nil, fmt.Errorf("engine: stop before pass %q: %w", np.name, err)
		}
		next, diags, err := np.run(ctx, doc)
		if err != nil {
			return nil, nil, err
		}
		if sum, err = p.afterPass(np, sum, doc, next); err != nil {
			return nil, nil, err
		}
		doc, found = next, append(found, diags...)
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, fmt.Errorf("engine: stop after the passes: %w", err)
	}
	return doc, found, nil
}

// run calls the pass on doc behind a barrier and holds what it returns to the
// duties of pass.Pass that the two documents and the findings let the engine
// check. A breach is an error naming the pass and wrapping pass.ErrContract,
// and a return once ctx is done is ctx's error, the output unjudged.
func (p namedPass) run(ctx context.Context, doc *ir.Document) (*ir.Document, []ir.Diagnostic, error) {
	var (
		next  *ir.Document
		diags []ir.Diagnostic
	)
	if err := caught("Run", func() { next, diags = p.pass.Run(ctx, doc) }); err != nil {
		return nil, nil, fmt.Errorf("engine: run pass %q: %w", p.name, err)
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, fmt.Errorf("engine: stop in pass %q: %w", p.name, err)
	}
	if err := checkOutput(doc, next, diags); err != nil {
		return nil, nil, p.breach(err)
	}
	return next, diags, nil
}

// caught runs f, a call of one of a pass's methods, behind a barrier. A panic in
// it is a breach of that method: the method, what the barrier recovered, then
// pass.ErrContract.
func caught(method string, f func()) error {
	if perr := barrier.Catch(f); perr != nil {
		return fmt.Errorf("%s: %w: %w", method, perr, pass.ErrContract)
	}
	return nil
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
