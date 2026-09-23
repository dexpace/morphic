package load

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/compilers/openapi/internal/overlay"
	"github.com/dexpace/morphic/ir"
)

// graftBase is the source every grafted-node case below overlays: one response
// and one schema the source declared, so an overlay can add a sibling to
// either that the source did not.
const graftBase = "openapi: 3.1.0\ninfo: {title: T, version: \"1\"}\npaths:\n  /b:\n    get:\n" +
	"      responses: {\"200\": {description: ok}}\ncomponents:\n  schemas:\n    A: {type: string}\n"

// TestLoad_ADiagnosticOnAGraftedNodeNamesTheOverlay pins the fix for
// GitHub #476 at every site that anchors a diagnostic on a raw node. A node an
// overlay grafts has no line and column — the library's clone keeps neither —
// so each of these used to name the source at 0:0. Now each names the overlay
// and the JSON pointer of the position the node sits at, the answer the
// lowering gives for that pointer.
//
// One case per site rather than one for the mechanism: the sites take their
// provenance through different paths (two pre-parse refusals off the index, a
// library validation finding), and a fix that reached one and not another
// would pass a single case. A resolver failure is not among them because the
// library reports one with no node at all, grafted or not (GitHub #235).
func TestLoad_ADiagnosticOnAGraftedNodeNamesTheOverlay(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		overlay string
		code    string
		pointer string
	}{
		"a cycle refusal": {
			overlay: "  - target: $.components.schemas\n    update: {C: {$ref: '#/components/schemas/C'}}\n",
			code:    diag.CyclicRef,
			pointer: "/components/schemas/C",
		},
		"a tagged-mapping refusal": {
			overlay: "  - target: $.paths['/b'].get.responses\n    update: {\"404\": !x {description: missing}}\n",
			code:    diag.TaggedMapping,
			pointer: "/paths/~1b/get/responses/404",
		},
		"a validation finding": {
			overlay: "  - target: $.paths['/b'].get.responses\n    update: {\"404\": {content: {}}}\n",
			code:    diag.Validation + "/validation-required-field",
			pointer: "/paths/~1b/get/responses/404",
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			clean, diags, err := Load(t.Context(), 0, openapitest.SourceOf(graftBase), Options{})
			require.NoError(t, err)
			require.NotNil(t, clean)
			require.False(t, diag.HasError(diags), "the source alone is clean: %+v", diags)

			_, diags, err = Load(t.Context(), 0, openapitest.SourceOf(graftBase), overlayOptions(tc.overlay))
			require.NoError(t, err)

			found := 0
			for _, d := range diags {
				if d.Code != tc.code {
					continue
				}
				found++
				assert.Equal(t, ir.Provenance{Source: 1, Pointer: tc.pointer}, d.Provenance,
					"the overlay put the node there, and the pointer says where")
			}
			assert.Equal(t, 1, found, "diagnostics: %+v", diags)
		})
	}
}

// TestLoad_ADiagnosticOnASourceNodeStillNamesTheSource is the control: with
// the same overlay applied, a finding on a node the source declared keeps the
// source and its line and column. The locator falls through to the source for
// every node the overlay does not answer for, so a fix that attributed too
// widely would move this one.
func TestLoad_ADiagnosticOnASourceNodeStillNamesTheSource(t *testing.T) {
	t.Parallel()
	const flawed = "openapi: 3.1.0\ninfo: {title: T, version: \"1\"}\npaths:\n  /b:\n    get:\n" +
		"      responses: {\"200\": {content: {}}}\n" // the source's own response lacks its description

	_, diags, err := Load(t.Context(), 0, openapitest.SourceOf(flawed),
		overlayOptions("  - target: $.paths['/b'].get.responses\n    update: {\"404\": {description: missing}}\n"))
	require.NoError(t, err)

	found := 0
	for _, d := range diags {
		if d.Code != diag.Validation+"/validation-required-field" {
			continue
		}
		found++
		assert.Equal(t, ir.Provenance{Source: 0, Pointer: "6:26"}, d.Provenance,
			"the source declared this node, at this position")
	}
	assert.Equal(t, 1, found, "diagnostics: %+v", diags)
}

// TestLoad_ATaggedMappingGraftedThroughAnAliasIsRefused pins GitHub #477: the
// shape that reopened the tagged-mapping crash after #475 closed it.
//
// The overlay library clones an alias by cloning its target, so an update whose
// value is an alias to a tagged anchor grafted a mapping that sat in no Content
// list — invisible to the pre-parse refusal that exists to catch exactly that
// tag, and visible to the parser, which followed the alias and faulted on a
// goroutine no recover reaches. The document is now normalized before the
// refusals read it, so the tag is found where it was grafted.
//
// The anchor is declared in three places because it is the overlay document's
// own structure, not the update's, that decides where a caller may put one.
func TestLoad_ATaggedMappingGraftedThroughAnAliasIsRefused(t *testing.T) {
	t.Parallel()
	for name, ov := range map[string]string{
		"anchored in the overlay's info": "overlay: 1.0.0\ninfo: {title: o, version: \"1\", x-a: &a !x {description: nf}}\n" +
			"actions:\n  - target: $.paths['/b'].get.responses\n    update: {\"404\": *a}\n",
		"anchored in a top-level extension": "overlay: 1.0.0\ninfo: {title: o, version: \"1\"}\n" +
			"x-anchors: {a: &a !x {description: nf}}\n" +
			"actions:\n  - target: $.paths['/b'].get.responses\n    update: {\"404\": *a}\n",
		"anchored on the action": "overlay: 1.0.0\ninfo: {title: o, version: \"1\"}\nactions:\n" +
			"  - target: $.paths['/b'].get.responses\n    x-a: &a !x {description: nf}\n    update: {\"404\": *a}\n",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			opts := Options{Overlay: &overlay.Options{Path: "patch.yaml", Data: []byte(ov)}, OverlaySrcIndex: 1}

			doc, diags, err := Load(t.Context(), 0, openapitest.SourceOf(graftBase), opts)

			require.NoError(t, err, "a tagged mapping is a spec problem, not a Go error")
			assert.Nil(t, doc, "nothing is lowered")
			require.Equal(t, 1, countErrorsAt(diags, diag.TaggedMapping), "diagnostics: %+v", diags)
			for _, d := range diags {
				if d.Code == diag.TaggedMapping {
					assert.Equal(t, ir.Provenance{Source: 1, Pointer: "/paths/~1b/get/responses/404"}, d.Provenance,
						"and the overlay owns the position it was grafted at")
				}
			}
		})
	}
}

// TestLoad_AnAliasGraftedFromTheSourceIsCountedWhereItLands is the other half
// of the same repair, on a graft that crashes nothing. A copy action clones a
// subtree of the source through the same clone, so an alias inside it was
// detached too — no tag, no fault, but a subtree the node budget did not count
// and the overlay's attribution could not name. Counting it is what the repair
// restores.
func TestLoad_AnAliasGraftedFromTheSourceIsCountedWhereItLands(t *testing.T) {
	t.Parallel()
	const spec = "openapi: 3.1.0\ninfo: {title: T, version: \"1\"}\n" +
		"x-t: &s {description: d}\nx-shared: {inner: *s}\npaths: {}\n"
	const ov = "overlay: 1.0.0\ninfo: {title: o, version: \"1\"}\n" +
		"actions:\n  - target: $.paths\n    copy: $[\"x-shared\"]\n"
	opts := Options{Overlay: &overlay.Options{Path: "patch.yaml", Data: []byte(ov)}, OverlaySrcIndex: 1}

	doc, diags, err := Load(t.Context(), 0, openapitest.SourceOf(spec), opts)

	require.NoError(t, err)
	require.NotNil(t, doc, "the copy is legal; only its shape was unreadable: %+v", diags)
	assert.False(t, diag.HasError(diags), "unexpected refusal: %+v", diags)
}
