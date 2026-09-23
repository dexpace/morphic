package load

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/compilers/openapi/internal/overlay"
	"github.com/dexpace/morphic/compilers/openapi/internal/sourceindex"
	"github.com/dexpace/morphic/ir"
)

// overlayOf wraps a whole overlay document as the loader's options, at the
// index Compile gives an overlay. overlayOptions prepends a header; the cases
// here write their own, because where an anchor sits in the document is what
// several of them are about.
func overlayOf(doc string) Options {
	return Options{Overlay: &overlay.Options{Path: "patch.yaml", Data: []byte(doc)}, OverlaySrcIndex: 1}
}

// updateBomb writes an overlay whose update value defines anchors a0..aL, each
// listing eight aliases to the one before: a few hundred bytes that stand for
// 8^L nodes once the aliases are followed.
func updateBomb(levels int) string {
	var b strings.Builder
	b.WriteString("overlay: 1.0.0\ninfo: {title: o, version: \"1\"}\nactions:\n  - target: $.paths\n    update:\n")
	b.WriteString("      a0: &a0 [x]\n")
	for k := 1; k <= levels; k++ {
		aliases := make([]string, 8)
		for i := range aliases {
			aliases[i] = fmt.Sprintf("*a%d", k-1)
		}
		fmt.Fprintf(&b, "      a%d: &a%d [%s]\n", k, k, strings.Join(aliases, ", "))
	}
	return b.String()
}

// TestLoad_AnOverlayAnchorThatContainsItselfIsRefused pins GitHub #489. The
// overlay library copies an update by cloning it, and its clone recurses
// through an alias into what the alias names; an anchor naming one of its own
// ancestors gives that recursion no base case, and the process ended with a
// stack overflow — a fatal error rather than a panic, so no barrier in this
// compiler could turn it into a diagnostic.
//
// The source has been protected from this shape since GitHub #12. The overlay
// is now given the same refusal, over the same tree shape, before the library
// is handed it. The second case is why it reads the whole document rather than
// each update: its anchor sits on the action list, outside the update that
// names it, so the cycle closes through a node no update holds.
func TestLoad_AnOverlayAnchorThatContainsItselfIsRefused(t *testing.T) {
	t.Parallel()
	const head = "overlay: 1.0.0\ninfo: {title: o, version: \"1\"}\n"
	for name, doc := range map[string]string{
		"inside the update":          head + "actions:\n  - target: $.paths\n    update: {p: &a [*a]}\n",
		"on the list of actions":     head + "actions: &x\n  - target: $.paths\n    update: {p: *x}\n",
		"on the action that uses it": head + "actions:\n  - &x\n    target: $.paths\n    update: {p: *x}\n",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			doc, diags, err := Load(t.Context(), 0, openapitest.SourceOf(minimal31), overlayOf(doc))

			require.NoError(t, err, "a cyclic overlay is a problem with the overlay, not a Go error")
			assert.Nil(t, doc, "nothing is lowered from a source the overlay could not be applied to")
			require.Equal(t, 1, countErrorsAt(diags, diag.CyclicRef), "diagnostics: %+v", diags)
		})
	}
}

// TestLoad_AnOverlayWhoseAliasesExpandPastTheAllowanceIsRefused pins the
// acyclic half of the same mechanism. The clone that never ends on a cycle
// ends on an alias bomb only when memory does: measured, a 393-byte overlay
// cost 355 MB at six levels and compiled without a word, so eight levels is an
// out-of-memory kill — as unrecoverable as the overflow. The source's alias
// allowance, calibrated against real documents, now bounds the overlay too.
func TestLoad_AnOverlayWhoseAliasesExpandPastTheAllowanceIsRefused(t *testing.T) {
	t.Parallel()
	bomb := updateBomb(6)
	require.Less(t, len(bomb), 512, "the point is how little it takes")

	doc, diags, err := Load(t.Context(), 0, openapitest.SourceOf(minimal31), overlayOf(bomb))

	require.NoError(t, err)
	assert.Nil(t, doc)
	require.Equal(t, 1, countErrorsAt(diags, diag.AliasAmplification), "diagnostics: %+v", diags)
}

// TestLoad_AnOverlayRefusalNamesTheOverlay pins where these refusals point. The
// finding is about the overlay document, so it names the overlay's index and a
// position in the overlay's own text — never the source's, which holds nothing
// the finding is about.
func TestLoad_AnOverlayRefusalNamesTheOverlay(t *testing.T) {
	t.Parallel()
	const doc = "overlay: 1.0.0\ninfo: {title: o, version: \"1\"}\nactions:\n  - target: $.paths\n    update: {p: &a [*a]}\n"

	// The position is derived from the text, not written down: it is the alias
	// on the fifth line, and a counted column is the number that drifts.
	line := strings.Split(doc, "\n")[4]
	want := fmt.Sprintf("5:%d", strings.Index(line, "*a")+1)

	_, diags, err := Load(t.Context(), 0, openapitest.SourceOf(minimal31), overlayOf(doc))

	require.NoError(t, err)
	require.Len(t, diags, 1)
	assert.Equal(t, ir.Provenance{Source: 1, Pointer: want}, diags[0].Provenance,
		"the alias that closes the cycle, in the overlay's own text")
}

// TestLoad_AnOverlayUsingAnchorsModestlyStillApplies is the control. Anchors are
// ordinary YAML and an overlay that reuses a fragment instead of repeating it is
// written well; the allowance exists to refuse a bomb, not a document with a
// few aliases in it.
func TestLoad_AnOverlayUsingAnchorsModestlyStillApplies(t *testing.T) {
	t.Parallel()
	const doc = "overlay: 1.0.0\ninfo: {title: o, version: \"1\"}\nactions:\n" +
		"  - target: $.info\n    update: {description: &d \"shared words\"}\n" +
		"  - target: $.paths\n    update: {x-note: *d}\n"

	got, diags, err := Load(t.Context(), 0, openapitest.SourceOf(minimal31), overlayOf(doc))

	require.NoError(t, err)
	require.NotNil(t, got, "a modest anchor is not a refusal: %+v", diags)
	assert.False(t, diag.HasError(diags), "unexpected refusal: %+v", diags)
	assert.Equal(t, "shared words", got.Doc.Info.GetDescription())
}

// TestLoad_AnOverlayTooLargeToIndexIsRefused pins the same bound the source has
// on its pre-parse index, for the same reason: an index that stopped early gives
// a partial node count, and the alias allowance derived from it would refuse
// on a bound the document never crossed. The seam is narrowed to the overlay
// by the one key only the overlay writes.
func TestLoad_AnOverlayTooLargeToIndexIsRefused(t *testing.T) {
	t.Parallel()
	opts := overlayOf("overlay: 1.0.0\ninfo: {title: o, version: \"1\"}\nactions:\n  - target: $.info\n    update: {description: d}\n")
	opts.buildIndex = func(root *yaml.Node) sourceindex.Index {
		if content := root.Content; len(content) == 1 && len(content[0].Content) > 0 && content[0].Content[0].Value == "overlay" {
			return sourceindex.Build(root, 1)
		}
		return defaultIndex(root)
	}

	got, diags, err := Load(t.Context(), 0, openapitest.SourceOf(minimal31), opts)

	require.NoError(t, err)
	assert.Nil(t, got)
	require.Equal(t, 1, countErrorsAt(diags, diag.SourceTooLarge), "diagnostics: %+v", diags)
	assert.Equal(t, 1, diags[0].Provenance.Source, "it is the overlay that is too large, not the source")
}

// TestApplyOverlay_CarriesAWarningFromItsRefusals pins what happens to a
// pre-apply finding short of a refusal. The only one there is the scan's report
// that it faulted, which says the overlay's protection from the library is
// incomplete; dropping it would let a compile that went on regardless look as
// trustworthy as one that did not. It cannot be provoked through Load — the
// scan it reports on handles every shape this package can build — so it is
// handed in directly.
func TestApplyOverlay_CarriesAWarningFromItsRefusals(t *testing.T) {
	t.Parallel()
	var root yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte(minimal31), &root))
	incomplete := diag.Newf(ir.SeverityWarning, diag.CycleScanFailed, ir.Provenance{Source: 1},
		"cycle pre-scan aborted; reference-cycle protection is incomplete for this source")
	opts := overlayOptions("  - target: $.info\n    update: {description: d}\n")

	origin, diags := applyOverlay(0, &root, opts, []ir.Diagnostic{incomplete})

	assert.True(t, origin.Applied(), "a warning is not a refusal; the overlay applies")
	assert.Contains(t, diags, incomplete, "and the caller is told the protection was incomplete")
}

// TestLoad_AnOverlayThatWillNotDecodeIsTheLibrarysToRefuse pins the edge of
// what the pre-apply refusals answer for. They read the overlay as YAML to find
// its aliases, and bytes that are not YAML have none to find; the library parses
// the same bytes a moment later and refuses them with a reason about the
// overlay. Reporting a second, vaguer finding about the same bytes would only
// bury that one.
func TestLoad_AnOverlayThatWillNotDecodeIsTheLibrarysToRefuse(t *testing.T) {
	t.Parallel()
	doc, diags, err := Load(t.Context(), 0, openapitest.SourceOf(minimal31), overlayOf("\tnot: yaml\n"))

	require.NoError(t, err)
	assert.Nil(t, doc)
	require.Len(t, diags, 1, "one finding about the overlay, not two: %+v", diags)
	assert.Equal(t, diag.OverlayInvalid, diags[0].Code, "and it is the library's, which says what is wrong")
}
