package load

import (
	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/overlay"
	"github.com/dexpace/morphic/compilers/openapi/internal/scan"
	"github.com/dexpace/morphic/compilers/openapi/internal/sourceindex"
	"github.com/dexpace/morphic/ir"
)

// patch applies the caller's overlay to the decoded tree, or does nothing when
// there is none. The overlay document is refused first if it is too large, too
// deep, or aliased past what the library can follow (overlayRefusals);
// applyOverlay does the rest.
func patch(srcIndex int, root *yaml.Node, opts Options) (overlay.Origin, []ir.Diagnostic) {
	if opts.Overlay == nil {
		return overlay.Origin{}, nil
	}
	return applyOverlay(srcIndex, root, opts, overlayRefusals(opts))
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
func applyOverlay(srcIndex int, root *yaml.Node, opts Options, pre []ir.Diagnostic) (overlay.Origin, []ir.Diagnostic) {
	if diag.HasError(pre) {
		return overlay.Origin{}, pre
	}
	// The overlay builds within the same node budget the patched tree is checked
	// against afterwards, so it can refuse an action before paying for it.
	ov := *opts.Overlay
	ov.MaxNodes = opts.MaxSourceNodes
	origin, diags := overlay.Apply(opts.OverlaySrcIndex, root, ov)
	diags = append(pre, diags...)
	if diag.HasError(diags) {
		return overlay.Origin{}, diags
	}
	return origin, append(diags, refusals(locator(srcIndex, origin, scan.Pointers(root)), root, opts)...)
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
func overlayRefusals(opts Options) []ir.Diagnostic {
	// The byte budget is checked before anything reads the bytes, as the
	// source's is (GitHub #75): an overlay is an input document like the source,
	// read in full by the decode below and again by the library, and it had no
	// bound on its size at all (GitHub #491).
	if exceeds(len(opts.Overlay.Data), opts.MaxSourceBytes) {
		return []ir.Diagnostic{budgetRefusal(opts.OverlaySrcIndex,
			"overlay document is %d bytes, past the %d-byte budget",
			len(opts.Overlay.Data), opts.MaxSourceBytes)}
	}
	var tree yaml.Node
	if err := yaml.Unmarshal(opts.Overlay.Data, &tree); err != nil {
		return nil
	}
	build := opts.buildIndex
	if build == nil {
		build = defaultIndex
	}
	locate := scan.InTree(opts.OverlaySrcIndex, scan.Pointers(&tree))
	idx := build(&tree)
	if idx.Truncated() {
		return []ir.Diagnostic{diag.Newf(ir.SeverityError, diag.SourceTooLarge, locate(nil),
			"overlay document exceeds the %d-node bound the pre-parse scan indexes",
			sourceindex.MaxIndexedNodes)}
	}
	var diags []ir.Diagnostic
	if d, deep := scan.Nesting(locate, idx); deep {
		diags = append(diags, d)
	}
	return append(diags, scan.Aliases(locate, idx, int64(opts.MaxAliasSurplus))...)
}
