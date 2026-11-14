// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package load

import (
	"context"
	"errors"
	"fmt"
	"slices"

	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi/internal/bind"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/doctree"
	"github.com/dexpace/morphic/compilers/openapi/internal/overlay"
	"github.com/dexpace/morphic/compilers/openapi/internal/provenance"
	"github.com/dexpace/morphic/compilers/openapi/internal/sourceindex"
	"github.com/dexpace/morphic/ir"
)

// readOverlay returns opts with its overlay's bytes in hand: those the caller
// handed over, or those of the local file its path names, read through
// opts.Reader and held to the byte budget as the source is. One past the
// budget is refused with the diagnostic an overlay handed in bytes gets
// (overlayRefusals). One that cannot be read is the caller's I/O failure, a Go
// error, as an unreadable spec is, and so are an empty path and a network URL:
// a caller naming the overlay names a file, and no setting may take a compile
// onto the network unasked.
func readOverlay(ctx context.Context, opts Options) (Options, []ir.Diagnostic, error) {
	if opts.Overlay == nil || len(opts.Overlay.Data) > 0 {
		return opts, nil, nil
	}
	if opts.Overlay.Path == "" {
		return opts, nil, errors.New("openapi: the overlay holds no bytes and names no file to read")
	}
	if bind.OverNetwork(opts.Overlay.Path) {
		return opts, nil, fmt.Errorf("openapi: overlay %s names a URL; an overlay is read from a local file",
			opts.Overlay.Path)
	}
	if opts.Reader == nil {
		return opts, nil, fmt.Errorf("openapi: overlay %s holds no bytes, and no reader was supplied to read it",
			opts.Overlay.Path)
	}
	data, err := opts.Reader.ReadDocument(ctx, compilers.DocumentRequest{
		Location: opts.Overlay.Path, Limit: ReadLimit(opts.MaxSourceBytes),
	})
	switch {
	case errors.Is(err, compilers.ErrDocumentTooLarge):
		return opts, []ir.Diagnostic{overlayPastBudget(opts)}, nil
	case err != nil:
		return opts, nil, fmt.Errorf("openapi: read overlay %s: %w", opts.Overlay.Path, err)
	}
	read := *opts.Overlay
	read.Data = data
	opts.Overlay = &read
	return opts, nil, nil
}

// overlayPastBudget refuses opts's overlay for holding more than the byte
// budget allows.
func overlayPastBudget(opts Options) ir.Diagnostic {
	return pastByteBudget(ir.Provenance{Source: opts.OverlaySrcIndex}, "overlay", opts.MaxSourceBytes)
}

// patch reads the caller's overlay (readOverlay) and applies it to the decoded
// tree, or does nothing when there is none. The overlay document is read once,
// and refused first if it is too large, too deep, or aliased past what its copy
// can follow (overlayRefusals); applyOverlay applies the tree that was read.
func patch(ctx context.Context, srcIndex int, root *yaml.Node, opts Options) (overlay.Origin, []ir.Diagnostic, error) {
	opts, read, err := readOverlay(ctx, opts)
	if err != nil || len(read) > 0 {
		return overlay.Origin{}, read, err
	}
	if opts.Overlay == nil {
		return overlay.Origin{}, nil, nil
	}
	doc, pre := overlayRefusals(ctx, opts)
	// Refusals a cancel cut short have screened none of the overlay, and a read
	// it stopped is no overlay that will not parse, so the cancel is seen before
	// anything is applied.
	if err := ctx.Err(); err != nil {
		return overlay.Origin{}, nil, err
	}
	origin, diags := applyOverlay(ctx, srcIndex, root, doc.Root, opts, pre)
	return origin, diags, nil
}

// applyOverlay applies the overlay document read to ov, given what its
// pre-apply refusals found, taken as data so each outcome is testable. An error
// there refuses before anything is applied. Anything less, only the scan's
// report that it faulted and warnings about how it was read, is carried into
// what the compile reports.
//
// It re-runs the pre-parse refusals afterwards, because an overlay action can
// graft what they refuse, such as nesting past the bound. They locate through
// the attribution the application produced, so a refusal on a grafted node
// names the overlay, not a position the node lacks (patchedRefusals).
func applyOverlay(ctx context.Context, srcIndex int, root, ov *yaml.Node, opts Options, pre []ir.Diagnostic,
) (overlay.Origin, []ir.Diagnostic) {
	if diag.HasError(pre) {
		return overlay.Origin{}, pre
	}
	// The overlay builds within the same node budget the patched tree is checked
	// against afterwards, so it can refuse an action before paying for it.
	o := *opts.Overlay
	o.MaxNodes = opts.MaxSourceNodes
	origin, diags := overlay.Apply(ctx, opts.OverlaySrcIndex, root, ov, o)
	diags = append(pre, diags...)
	if diag.HasError(diags) {
		return overlay.Origin{}, diags
	}
	tree := treeOf(ctx, provenance.Doc{Source: srcIndex, Overlay: origin.Attribution()}, root)
	return origin, append(diags, patchedRefusals(ctx, tree, root, opts)...)
}

// patchedRefusals is refusals of root, the tree the overlay patched, one at a
// node placed as tree places it. One of the whole tree, such as its pointers'
// bytes, is placed at the overlay, whose actions caused it, since the source
// passed the same refusals before the overlay applied (loading.preParse), and
// says it reads the tree the overlay leaves, as the overlay's own document
// draws the same refusal (overlayRefusals).
func patchedRefusals(ctx context.Context, tree provenance.Tree, root *yaml.Node, opts Options) []ir.Diagnostic {
	whole := ir.Provenance{Source: opts.OverlaySrcIndex}
	found := refusals(ctx, tree, whole, root, opts)
	for i := range found {
		if found[i].Provenance == whole {
			found[i].Message += ", once the overlay applies"
		}
	}
	return found
}

// overlayRefusals reads the overlay document and holds it to the source's
// refusals, under the same limits, before it is applied, returning what it read
// and what it found: bytes past the budget, unread, a read the parser refuses,
// a tree past the node budget, a stream past its first document, a YAML version
// it is not read as, and the shapes an update copies through its aliases
// (overlay.copyOf): an anchor naming its own ancestor gives the copy no end, an
// alias bomb exhausts memory (GitHub #489).
func overlayRefusals(ctx context.Context, opts Options) (doctree.Document, []ir.Diagnostic) {
	at := ir.Provenance{Source: opts.OverlaySrcIndex}
	// The byte budget is checked before anything reads the bytes, as the
	// source's is (GitHub #75, #491).
	if !opts.MaxSourceBytes.Admits(len(opts.Overlay.Data)) {
		return doctree.Document{}, []ir.Diagnostic{overlayPastBudget(opts)}
	}
	doc, err := doctree.Read(ctx, opts.Overlay.Data, opts.MaxSourceNodes)
	if d, past := pastNodes(at, "overlay", err); past {
		return doctree.Document{}, []ir.Diagnostic{d}
	}
	if err != nil {
		return doctree.Document{}, []ir.Diagnostic{diag.Newf(ir.SeverityError, diag.OverlayInvalid, at,
			"cannot parse overlay: %s", diag.OneLine(err))}
	}

	placed := treeOf(ctx, provenance.Doc{Source: opts.OverlaySrcIndex}, doc.Root)
	found := slices.Concat(streamDiagnostics(opts.OverlaySrcIndex, overlayStream, doc.Rest), versionDiagnostics(placed, doc))
	build := opts.buildIndex
	if build == nil {
		build = defaultIndex
	}
	idx := build(ctx, doc.Root)
	if idx.Truncated() {
		return doc, append(found, diag.Newf(ir.SeverityError, diag.SourceTooLarge, placed.Node(nil),
			"overlay document exceeds the %d-node bound the pre-parse scan indexes",
			sourceindex.MaxIndexedNodes))
	}
	return doc, append(found, shapeRefusals(ctx, placed, placed.Node(nil), idx, opts)...)
}
