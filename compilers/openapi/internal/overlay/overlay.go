// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package overlay

import (
	"context"

	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/budget"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/provenance"
	"github.com/dexpace/morphic/ir"
)

// maxNodes bounds the walk that attributes the positions an overlay wrote, and
// the copies that repair the aliases a remove left; the application spends the
// caller's Options.MaxNodes. It counts nodes, not depth, because the walk is
// iterative, and a document this large is pathological. A walk that exhausts
// it abandons the attribution whole rather than return a partial one, and
// every position keeps the source as its origin; copies past it refuse the
// overlay.
const maxNodes = 1 << 21

// Options is one pre-read overlay document and how strictly to apply it.
type Options struct {
	// Path names the overlay document. It is recorded as the overlay's SourceInfo
	// path and never opened.
	Path string
	// Data is the overlay document's bytes. The caller reads them; nothing here
	// performs I/O, so the compiler stays pure (compilers.Source's contract).
	Data []byte
	// Lax turns off strict application.
	//
	// Strict, the zero value, is the default because an action whose selector
	// matches nothing is nearly always a typo in a JSONPath, and an overlay that
	// silently does nothing ships an SDK missing the very fix it was written to
	// make. Under strict, such an action refuses the overlay; under lax it is
	// reported as a warning, and the rest of the overlay applies.
	Lax bool
	// MaxNodes bounds the document the overlay may grow the source to, checked
	// before each action builds anything; unset, it is unbounded. It is the
	// compile's node budget: the loader checks the patched tree against the
	// same limit afterwards, and this is what stops an overlay spending memory
	// to build a tree that check would refuse (GitHub #491).
	MaxNodes budget.Limit
}

// Origin answers which input document supplied a lowered position.
//
// The zero value is the answer for a compile with no overlay: nothing was
// applied, and every position belongs to whichever source the caller names.
type Origin struct {
	// source is the overlay's identity as an input document.
	source ir.SourceInfo
	// attribution holds the positions the overlay introduced or rewrote. It
	// attributes none when the overlay did not apply, or applied past the
	// attribution walk's bound.
	attribution provenance.Attribution
	// applied reports that the overlay was applied, whether or not it changed
	// anything and whether or not its positions could be attributed to it.
	applied bool
}

// Applied reports whether an overlay was applied to the document at all. It
// does not say its positions were attributed: past the attribution walk's
// bound the overlay still applies and every position keeps the source as its
// origin, but the overlay remains an input the document was built from, and
// the warning that says so names it.
func (o Origin) Applied() bool { return o.applied }

// Source is the overlay's identity as an input document, for Document.Sources.
// It is meaningful only when Applied reports true.
func (o Origin) Source() ir.SourceInfo { return o.source }

// Attribution returns the positions the overlay supplied, which name the
// overlay as the writer of a pointer or a node of the patched document, placed
// in that document's own entry of Document.Sources (provenance.Doc,
// provenance.Tree).
func (o Origin) Attribution() provenance.Attribution { return o.attribution }

// Apply applies opts to root in place and returns the attribution of what it
// changed, with index as the overlay's place in Document.Sources.
//
// An error-severity diagnostic means root was not usefully overlaid and the
// caller must refuse to lower: actions apply in order and the ones that landed
// are not undone, so a tree left behind by a failed application is neither the
// source nor what the overlay asked for.
func Apply(ctx context.Context, index int, root *yaml.Node, opts Options) (Origin, []ir.Diagnostic) {
	return applyWithin(ctx, index, root, opts, budget.AtMost(maxNodes))
}

// applyWithin is Apply with the attribution walk's node bound as a parameter.
//
// The bound is the walk's, not the caller's: Apply is the only production
// entry and always passes maxNodes, so nothing outside this package can weaken
// it. It is a parameter so the degradation at the bound is reachable from a
// test with a small tree.
func applyWithin(ctx context.Context, index int, root *yaml.Node, opts Options, walks budget.Limit) (Origin, []ir.Diagnostic) {
	at := ir.Provenance{Source: index}
	p, diags := read(at, opts.Data)
	if diag.HasError(diags) {
		return Origin{}, diags
	}

	var g *grafter
	reported, applied := applyRecovered(at, func() ([]ir.Diagnostic, bool) {
		var (
			out []ir.Diagnostic
			ok  bool
		)
		g, out, ok = runSequence(ctx, p, root, opts.Lax, opts.MaxNodes)
		return out, ok
	})
	diags = append(diags, reported...)
	if !applied {
		return Origin{}, diags
	}

	// A repair a cancel cut short refuses as a cancelled sequence does, with
	// nothing reported, since the caller reads its context next.
	switch done, safe := repairAliases(ctx, root, g, walks); {
	case !done:
		return Origin{}, diags
	case !safe:
		return Origin{}, append(diags, diag.Newf(ir.SeverityError, diag.OverlayFailed, at,
			"overlay applied, but the aliases a remove left naming what it removed expand past %d nodes", walks.Cap()))
	}
	source := ir.SourceInfo{Format: ir.OverlayFormat + "@" + p.declared, Path: opts.Path, Hash: ir.SourceHash(opts.Data)}
	positions, attributed := attribute(ctx, root, g.written, walks)
	if !attributed {
		// Applied without attribution: the source keeps every position, but the
		// overlay stays in Document.Sources, since the warning below names it.
		return Origin{source: source, applied: true}, append(diags, diag.Newf(ir.SeverityWarning, diag.OverlayOriginIncomplete, at,
			"overlay applied, but the document exceeds %d nodes; every position keeps the source as its origin", walks.Cap()))
	}
	return Origin{source: source, attribution: provenance.Attributed(index, positions), applied: true}, diags
}

// applyRecovered runs apply, the overlay's application, behind diag.Catch and
// turns a panic in it into a refusal quoting what it panicked with, so the
// no-panics-escape invariant holds. The application runs the parser library's
// JSONPath evaluator, whose faults are not this package's to bound. A
// half-applied tree is never reported as applied.
func applyRecovered(at ir.Provenance, apply func() ([]ir.Diagnostic, bool)) ([]ir.Diagnostic, bool) {
	var (
		diags   []ir.Diagnostic
		applied bool
	)
	if text, panicked := diag.Catch(func() { diags, applied = apply() }); panicked {
		return []ir.Diagnostic{diag.Newf(ir.SeverityError, diag.OverlayFailed, at,
			"cannot apply overlay: applying it panicked (%s)", text)}, false
	}
	return diags, applied
}
