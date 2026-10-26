package load

import (
	"context"
	"errors"
	"fmt"

	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/overlay"
	"github.com/dexpace/morphic/compilers/openapi/internal/scan"
	"github.com/dexpace/morphic/compilers/openapi/internal/sourceindex"
	"github.com/dexpace/morphic/ir"
)

// readOverlay returns opts with its overlay's bytes in hand: those the caller
// handed over, or those of the local file its path names, read through
// opts.Reader and held to the byte budget as the source is. One past the
// budget is refused with the diagnostic an overlay handed in bytes gets
// (overlayRefusals). One that cannot be read is the caller's I/O failure, a Go
// error, as an unreadable spec is, and so is a path naming a document on the
// network: a caller naming the overlay names a file, and no setting may take a
// compile onto the network unasked.
func readOverlay(ctx context.Context, opts Options) (Options, []ir.Diagnostic, error) {
	if opts.Overlay == nil || len(opts.Overlay.Data) > 0 {
		return opts, nil, nil
	}
	if overNetwork(opts.Overlay.Path) {
		return opts, nil, fmt.Errorf("openapi: overlay %s names a URL; an overlay is read from a local file",
			opts.Overlay.Path)
	}
	if opts.Reader == nil {
		return opts, nil, fmt.Errorf("openapi: overlay %s holds no bytes, and no reader was supplied to read it",
			opts.Overlay.Path)
	}
	data, err := opts.Reader.ReadDocument(ctx, compilers.DocumentRequest{
		Location: opts.Overlay.Path, Limit: opts.MaxSourceBytes,
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
// tree, or does nothing when there is none. The overlay document is refused
// first if it is too large, too deep, or aliased past what the library can
// follow (overlayRefusals); applyOverlay does the rest.
func patch(ctx context.Context, srcIndex int, root *yaml.Node, opts Options) (overlay.Origin, []ir.Diagnostic, error) {
	opts, read, err := readOverlay(ctx, opts)
	if err != nil || len(read) > 0 {
		return overlay.Origin{}, read, err
	}
	if opts.Overlay == nil {
		return overlay.Origin{}, nil, nil
	}
	pre := overlayRefusals(ctx, opts)
	// The library parses the overlay reading no context, and refusals a cancel
	// cut short have screened none of it, so the cancel is seen before it is
	// handed over.
	if err := ctx.Err(); err != nil {
		return overlay.Origin{}, nil, err
	}
	origin, diags := applyOverlay(ctx, srcIndex, root, opts, pre)
	return origin, diags, nil
}

// applyOverlay applies the overlay given what its pre-apply refusals found,
// taken as data so each outcome is testable. An error there refuses before the
// library is handed anything. Anything less, only the scan's report that it
// faulted, is carried into what the compile reports: the overlay's protection
// from the library is incomplete.
//
// It re-runs the pre-parse refusals afterwards, because an overlay action can
// graft what they refuse, such as nesting past the bound. They locate through
// the attribution the application produced, so a refusal on a grafted node
// names the overlay, not a position the node lacks.
func applyOverlay(ctx context.Context, srcIndex int, root *yaml.Node, opts Options, pre []ir.Diagnostic,
) (overlay.Origin, []ir.Diagnostic) {
	if diag.HasError(pre) {
		return overlay.Origin{}, pre
	}
	// The overlay builds within the same node budget the patched tree is checked
	// against afterwards, so it can refuse an action before paying for it.
	ov := *opts.Overlay
	ov.MaxNodes = opts.MaxSourceNodes
	origin, diags := overlay.Apply(ctx, opts.OverlaySrcIndex, root, ov)
	diags = append(pre, diags...)
	if diag.HasError(diags) {
		return overlay.Origin{}, diags
	}
	return origin, append(diags, refusals(ctx, locator(srcIndex, origin, scan.Pointers(root)), root, opts)...)
}

// overlayRefusals holds an overlay to the source's refusals, under the same
// limits, before the library is handed it. The library clones each update by
// following aliases, into every node its target selects: an anchor naming its
// own ancestor overflows the stack, an alias bomb exhausts memory
// (docs/parser-library.md#overlay-alias-clone, GitHub #489), and a deep update
// costs more per node than a shallow one.
//
// The whole overlay is decoded, since a scan of each update alone would miss a
// cycle whose anchor sits outside it. Undecodable bytes are left to the
// library.
func overlayRefusals(ctx context.Context, opts Options) []ir.Diagnostic {
	// The byte budget is checked before anything reads the bytes, as the
	// source's is (GitHub #75): an overlay is an input document like the source,
	// read in full by the decode below and again by the library, and it had no
	// bound on its size at all (GitHub #491).
	if exceeds(len(opts.Overlay.Data), opts.MaxSourceBytes) {
		return []ir.Diagnostic{overlayPastBudget(opts)}
	}
	tree, err := parseTree(ctx, opts.Overlay.Data)
	if err != nil {
		return nil
	}
	build := opts.buildIndex
	if build == nil {
		build = defaultIndex
	}
	locate := scan.InTree(opts.OverlaySrcIndex, scan.Pointers(tree))
	idx := build(ctx, tree)
	if idx.Truncated() {
		return []ir.Diagnostic{diag.Newf(ir.SeverityError, diag.SourceTooLarge, locate(nil),
			"overlay document exceeds the %d-node bound the pre-parse scan indexes",
			sourceindex.MaxIndexedNodes)}
	}
	var diags []ir.Diagnostic
	if d, deep := scan.Nesting(locate, idx); deep {
		diags = append(diags, d)
	}
	return append(diags, scan.Aliases(ctx, locate, idx, int64(opts.MaxAliasSurplus))...)
}
