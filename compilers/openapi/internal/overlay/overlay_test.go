package overlay_test

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/overlay"
	"github.com/dexpace/morphic/ir"
)

// spec is the tree every case below overlays. It carries one of each shape the
// walk addresses differently — a nested mapping, a sequence, and a key needing
// RFC 6901 escaping — so a pointer built wrongly for any of them shows up as a
// misattribution rather than passing unexercised.
const spec = `openapi: 3.1.0
info:
  title: Original
  version: "1"
tags:
  - name: pets
paths:
  /pets:
    get:
      operationId: listPets
components:
  schemas:
    Pet:
      type: object
      properties:
        name: {type: string}
`

// overlayIndex is the index the tests hand Apply as the overlay's place in
// Document.Sources, and srcIndex the fallback they ask IndexAt for. They are
// distinct and non-zero so a result that happens to be either one cannot be the
// zero value arriving by accident.
const (
	overlayIndex = 1
	srcIndex     = 7
)

// treeOf decodes spec into a fresh node tree for an overlay to be applied to.
// Every case gets its own, because Apply mutates the tree it is handed.
func treeOf(t *testing.T) *yaml.Node {
	t.Helper()
	var root yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte(spec), &root))
	return &root
}

// applyTo overlays ov onto a fresh tree of spec and requires that it succeeded,
// returning the attribution and the diagnostics it reported along the way.
func applyTo(t *testing.T, ov string, lax bool) (overlay.Origin, []ir.Diagnostic) {
	t.Helper()
	origin, diags := overlay.Apply(overlayIndex, treeOf(t),
		overlay.Options{Path: "o.yaml", Data: []byte(ov), Lax: lax})
	require.False(t, diag.HasError(diags), "overlay did not apply: %+v", diags)
	require.True(t, origin.Applied())
	return origin, diags
}

// header is the preamble every overlay document below needs.
const header = "overlay: 1.0.0\ninfo: {title: O, version: \"1\"}\nactions:\n"

// TestApply_AttributesIntroducedPositions pins the acceptance criterion the
// second Sources entry exists for: a position the overlay added names the
// overlay, and the positions beside it that the source declared do not.
//
// The four targets differ on purpose, because each addresses the walk
// differently: a new leaf under an existing mapping, a new element appended to a
// sequence, a whole new named schema — the case that proves the set is closed
// downwards, since nothing marks its properties individually — and a new path,
// whose key holds the `/` that RFC 6901 escaping exists for.
//
// Every one of them is asserted as introduced rather than as declared. A
// declared assertion is satisfied by the fallback, so it would pass on a pointer
// built wrongly just as readily as on a right one; only a position the overlay
// must be found at can tell a correct pointer from an unrecognized one.
func TestApply_AttributesIntroducedPositions(t *testing.T) {
	t.Parallel()
	origin, _ := applyTo(t, header+`  - target: $.components.schemas.Pet.properties
    update:
      tag: {type: string}
  - target: $.tags
    update:
      - name: added
  - target: $.components.schemas
    update:
      Owner: {type: object, properties: {id: {type: string}}}
  - target: $.paths
    update:
      /owners: {get: {operationId: listOwners}}
`, false)

	introduced := []string{
		"/components/schemas/Pet/properties/tag",
		"/tags/1",
		"/components/schemas/Owner",
		"/components/schemas/Owner/properties/id",
		"/paths/~1owners",
		"/paths/~1owners/get/operationId",
	}
	for _, p := range introduced {
		assert.Equal(t, overlayIndex, origin.IndexAt(p, srcIndex), "%s came from the overlay", p)
	}

	declared := []string{
		"/components/schemas/Pet",
		"/components/schemas/Pet/properties/name",
		"/tags/0",
		"/info/title",
		"/paths/~1pets/get",
	}
	for _, p := range declared {
		assert.Equal(t, srcIndex, origin.IndexAt(p, srcIndex), "%s is the source's own", p)
	}
}

// TestApply_AttributesRewrittenScalars pins the half node identity alone would
// miss. The library overwrites a scalar in place rather than replacing its node,
// so a position whose value the overlay changed is indistinguishable from an
// untouched one unless the snapshot records what it held — and the IR would then
// carry the overlay's title while naming the file on disk as its source.
func TestApply_AttributesRewrittenScalars(t *testing.T) {
	t.Parallel()
	origin, _ := applyTo(t, header+`  - target: $.info
    update:
      title: Renamed
`, false)

	assert.Equal(t, overlayIndex, origin.IndexAt("/info/title", srcIndex),
		"the overlay wrote this value, even though the position existed")
	assert.Equal(t, srcIndex, origin.IndexAt("/info/version", srcIndex),
		"the sibling it did not touch is unaffected")
	assert.Equal(t, srcIndex, origin.IndexAt("/info", srcIndex),
		"and the mapping holding both still exists on disk")
}

// TestApply_RemovalLeavesTheRestAttributedToTheSource pins that a remove action
// attributes nothing. It deletes rather than introduces, so there is no position
// left for the overlay to answer for — and a walk that mistook the shortened
// content slice for new nodes would blame it for the survivors.
func TestApply_RemovalLeavesTheRestAttributedToTheSource(t *testing.T) {
	t.Parallel()
	origin, _ := applyTo(t, header+`  - target: $.components.schemas.Pet.properties.name
    remove: true
`, false)

	assert.Equal(t, srcIndex, origin.IndexAt("/components/schemas/Pet", srcIndex))
	assert.Equal(t, srcIndex, origin.IndexAt("/info/title", srcIndex))
}

// TestApply_StrictReportsAnActionThatMatchesNothing pins the strict half of the
// acceptance criteria: a selector matching nothing is named as a warning and
// refused as an error, so a typo in a JSONPath cannot ship an SDK missing the
// fix the overlay was written to make.
func TestApply_StrictReportsAnActionThatMatchesNothing(t *testing.T) {
	t.Parallel()
	origin, diags := overlay.Apply(overlayIndex, treeOf(t), overlay.Options{
		Data: []byte(header + "  - target: $.paths['/nope']\n    update: {description: x}\n"),
	})

	assert.False(t, origin.Applied(), "a refused overlay attributes nothing")
	assert.True(t, diag.HasError(diags), "the typo is fatal: %+v", diags)
	assert.Equal(t, diag.OverlayFailed, diags[len(diags)-1].Code)

	named := false
	for _, d := range diags {
		if d.Code == diag.OverlayAction {
			named = true
			assert.Equal(t, ir.SeverityWarning, d.Severity)
			assert.Contains(t, d.Message, "$.paths['/nope']", "the warning names the action's target")
		}
	}
	assert.True(t, named, "the refusal is accompanied by the action that caused it: %+v", diags)
}

// TestApply_LaxReportsNothingForAnActionThatMatchesNothing pins the other side
// of the same switch, on the same overlay: what strict refuses, lax passes over
// in silence. Asserting on the identical input is what makes this a test of the
// flag rather than of two unrelated documents.
func TestApply_LaxReportsNothingForAnActionThatMatchesNothing(t *testing.T) {
	t.Parallel()
	origin, diags := applyTo(t, header+"  - target: $.paths['/nope']\n    update: {description: x}\n", true)

	assert.Empty(t, diags, "lax reports nothing at all")
	assert.True(t, origin.Applied())
}

// TestApply_RejectsAnOverlayItCannotRead pins that a broken overlay leaves as a
// diagnostic rather than a Go error — it is a problem with an input document,
// which is the compiler's to report — and that nothing is attributed to an
// overlay that never applied.
func TestApply_RejectsAnOverlayItCannotRead(t *testing.T) {
	t.Parallel()
	for name, data := range map[string]string{
		"not yaml":       "\tnot: yaml\n",
		"not an overlay": "openapi: 3.1.0\ninfo: {title: T, version: \"1\"}\n",
		"no actions":     "overlay: 1.0.0\ninfo: {title: O, version: \"1\"}\nactions: []\n",
		"empty":          "",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			origin, diags := overlay.Apply(overlayIndex, treeOf(t), overlay.Options{Data: []byte(data)})

			assert.False(t, origin.Applied())
			require.True(t, diag.HasError(diags), "%q must be refused: %+v", data, diags)
			assert.Equal(t, diag.OverlayInvalid, diags[0].Code)
			assert.Equal(t, overlayIndex, diags[0].Provenance.Source,
				"the diagnostic is about the overlay, not the spec")
		})
	}
}

// TestApply_ReportsASelectorItCannotParse pins the failure that is neither a
// broken overlay document nor a selector that matched nothing: the target is not
// a JSONPath at all, which Validate does not check and only the application
// discovers. It must attribute nothing, because the actions that already landed
// are not undone.
//
// It is the one apply failure both modes share, which is what makes it the
// fixture for the lax path: lax refuses no-match and merges a mismatched shape
// rather than failing, so an unparsable selector is the only way lax reaches
// this branch at all.
func TestApply_ReportsASelectorItCannotParse(t *testing.T) {
	t.Parallel()
	for _, lax := range []bool{false, true} {
		origin, diags := overlay.Apply(overlayIndex, treeOf(t),
			overlay.Options{Data: []byte(header + "  - target: \"$[\"\n    update: {a: b}\n"), Lax: lax})

		assert.False(t, origin.Applied(), "lax=%v", lax)
		require.True(t, diag.HasError(diags), "lax=%v: %+v", lax, diags)
		assert.Equal(t, diag.OverlayFailed, diags[len(diags)-1].Code, "lax=%v", lax)
	}
}

// TestApply_RecordsTheOverlayAsAnInputDocument pins the SourceInfo that makes an
// attributed position resolvable: Document.Sources is what a Provenance.Source
// indexes into, so an index naming an entry that does not describe the overlay
// is a dangling reference dressed as a valid one.
func TestApply_RecordsTheOverlayAsAnInputDocument(t *testing.T) {
	t.Parallel()
	body := header + "  - target: $.info\n    update: {description: d}\n"
	origin, _ := applyTo(t, body, false)

	got := origin.Source()
	assert.Equal(t, "overlay@1.0.0", got.Format, "the dialect the overlay declares, not the spec's")
	assert.Equal(t, "o.yaml", got.Path)

	// Derived, not transcribed: the hash identifies the overlay by its content, so
	// a caching or snapshot consumer must see it change when a byte does. Asserting
	// only its shape would be satisfied by a hash of the path, which never does.
	want := sha256.Sum256([]byte(body))
	assert.Equal(t, hex.EncodeToString(want[:]), got.Hash, "a sha256 of the overlay's own bytes")
}

// TestOrigin_ZeroValueAttributesNothing pins the answer for every compile with
// no overlay. It is what lets ProvenanceAt ask the same question either way,
// rather than each caller first asking whether there was an overlay at all.
func TestOrigin_ZeroValueAttributesNothing(t *testing.T) {
	t.Parallel()
	var zero overlay.Origin

	assert.False(t, zero.Applied())
	assert.Equal(t, srcIndex, zero.IndexAt("/components/schemas/Pet", srcIndex))
	assert.Equal(t, ir.SourceInfo{}, zero.Source())
}
