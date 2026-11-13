// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package overlay_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/jsontext"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/overlay"
	"github.com/dexpace/morphic/ir"
)

// spec is the tree most cases below overlay. It carries one of each shape the
// attribution walk addresses differently — a nested mapping, a sequence, and a
// key needing RFC 6901 escaping — so a pointer built wrongly for any of them
// shows up as a misattribution rather than passing unexercised. Slot is a
// second schema for the copy case to land on, since copying a subtree onto
// itself changes nothing and would attribute nothing either.
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
    Slot: {type: string}
`

// applyTo overlays ov onto a fresh tree of spec and requires that it applied,
// returning the attribution and the diagnostics it reported along the way.
func applyTo(t *testing.T, ov string) (overlay.Origin, []ir.Diagnostic) {
	t.Helper()
	origin, _, diags := applied(t, spec, ov, false)
	require.False(t, diag.HasError(diags), "overlay did not apply: %+v", diags)
	require.True(t, origin.Applied())
	return origin, diags
}

// TestApply_AttributesIntroducedPositions pins the criterion the second Sources
// entry exists for: a position the overlay added names the overlay, and the
// positions beside it that the source declared do not.
//
// The four targets each address the walk differently: a new leaf in a mapping,
// a new sequence element, a whole new named schema (proving the set is closed
// downwards), and a new path whose key holds a `/` needing RFC 6901 escaping.
//
// Introduced positions are asserted as introduced, not declared: a declared
// one reads 0, as any pointer the attribution lacks does, so it passes on a
// wrongly built pointer too.
func TestApply_AttributesIntroducedPositions(t *testing.T) {
	t.Parallel()
	origin, _ := applyTo(t, v10+`  - target: $.components.schemas.Pet.properties
    update:
      tag: {type: string}
  - target: $.tags
    update: {name: added}
  - target: $.components.schemas
    update:
      Owner: {type: object, properties: {id: {type: string}}}
  - target: $.paths
    update:
      /owners: {get: {operationId: listOwners}}
`)

	introduced := []jsontext.Pointer{
		"/components/schemas/Pet/properties/tag",
		"/components/schemas/Pet/properties/tag/type",
		"/tags/1",
		"/tags/1/name",
		"/components/schemas/Owner",
		"/components/schemas/Owner/properties/id",
		"/paths/~1owners",
		"/paths/~1owners/get/operationId",
	}
	for _, p := range introduced {
		assert.Equal(t, overlayIndex, origin.Attribution().WriterAt(p), "%s came from the overlay", p)
	}

	declared := []jsontext.Pointer{
		"",
		"/components/schemas/Pet",
		"/components/schemas/Pet/properties/name",
		"/tags",
		"/tags/0",
		"/info/title",
		"/paths/~1pets/get",
		"/paths/~1owners/get/summary", // beneath what the overlay wrote, and nothing at all
	}
	for _, p := range declared {
		assert.Equal(t, 0, origin.Attribution().WriterAt(p), "%s is the source's own", p)
	}
}

// TestApply_AttributesRewrittenScalars pins the half node identity alone would
// miss. A scalar is overwritten in place rather than its node replaced, so a
// position whose value the overlay changed keeps the source's node, and the
// IR would carry the overlay's title while naming the file on disk as its
// source unless the rewrite is recorded.
func TestApply_AttributesRewrittenScalars(t *testing.T) {
	t.Parallel()
	origin, _ := applyTo(t, v10+"  - target: $.info\n    update:\n      title: Renamed\n")

	assert.Equal(t, overlayIndex, origin.Attribution().WriterAt("/info/title"),
		"the overlay wrote this value, even though the position existed")
	assert.Equal(t, 0, origin.Attribution().WriterAt("/info/version"),
		"the sibling it did not touch is unaffected")
	assert.Equal(t, 0, origin.Attribution().WriterAt("/info"),
		"and the mapping holding both still exists on disk")
}

// TestApply_AttributesARewriteOfTheTagAlone pins a rewrite that keeps a
// scalar's text and changes its type: the string "1" becomes the number 1,
// which the IR reads otherwise, so the overlay is its writer.
func TestApply_AttributesARewriteOfTheTagAlone(t *testing.T) {
	t.Parallel()
	origin, _ := applyTo(t, v10+"  - target: $.info\n    update: {version: 1}\n")

	assert.Equal(t, overlayIndex, origin.Attribution().WriterAt("/info/version"))
}

// TestApply_RemovalLeavesTheRestAttributedToTheSource pins that a remove action
// attributes nothing. It deletes rather than introduces, so there is no position
// left for the overlay to answer for — and a walk that mistook the shortened
// content slice for new nodes would blame it for the survivors.
func TestApply_RemovalLeavesTheRestAttributedToTheSource(t *testing.T) {
	t.Parallel()
	origin, _ := applyTo(t, v10+"  - target: $.components.schemas.Pet.properties.name\n    remove: true\n")

	assert.Equal(t, 0, origin.Attribution().WriterAt("/components/schemas/Pet"))
	assert.Equal(t, 0, origin.Attribution().WriterAt("/info/title"))
}

// TestApply_AttributesACopiedSubtree pins the third action type. A copy grafts
// nodes cloned from elsewhere in the same document, so the positions it fills
// are the overlay's doing even though every byte of their content was already in
// the source — which is the case an attribution keyed on content rather than on
// what the overlay did would get backwards.
func TestApply_AttributesACopiedSubtree(t *testing.T) {
	t.Parallel()
	origin, _ := applyTo(t, v11+"  - target: $.components.schemas.Slot\n    copy: $.components.schemas.Pet\n")

	for _, p := range []jsontext.Pointer{
		"/components/schemas/Slot/properties",
		"/components/schemas/Slot/properties/name",
		"/components/schemas/Slot/properties/name/type",
		"/components/schemas/Slot/type", // string, overwritten in place by object
	} {
		assert.Equal(t, overlayIndex, origin.Attribution().WriterAt(p), "the copy put %s here", p)
	}
	assert.Equal(t, 0, origin.Attribution().WriterAt("/components/schemas/Slot"),
		"but the position it was copied onto is the source's own")
	assert.Equal(t, 0, origin.Attribution().WriterAt("/components/schemas/Pet/properties/name"),
		"and so is everything it was copied from")
}

// TestApply_StrictRefusesAnActionThatSelectsNothing pins the strict half of the
// switch: a selector matching nothing is the specification's no-op, and a
// likely typo in a JSONPath, so it refuses the overlay with an error naming
// the action, at the action, and an overlay that ships an SDK missing the fix
// it was written to make cannot pass.
func TestApply_StrictRefusesAnActionThatSelectsNothing(t *testing.T) {
	t.Parallel()
	origin, _, diags := applied(t, spec, v10+
		"  - target: $.info\n    update: {description: d}\n"+
		"  - target: $.paths['/nope']\n    update: {description: x}\n"+
		"  - target: $.elsewhere\n    remove: true\n", false)

	assert.False(t, origin.Applied(), "a refused overlay attributes nothing")
	sameCodes(t, []string{"error " + diag.OverlayFailed, "error " + diag.OverlayFailed}, diags)
	require.Len(t, diags, 2)
	assert.Equal(t, ir.Provenance{Source: overlayIndex, Pointer: "/actions/1"}, diags[0].Provenance, "each at its action")
	assert.Contains(t, diags[0].Message, "$.paths['/nope']", "naming the action's target")
	assert.Equal(t, ir.Provenance{Source: overlayIndex, Pointer: "/actions/2"}, diags[1].Provenance,
		"and every action that selects nothing is named, not the first alone")
}

// TestApply_LaxWarnsOfAnActionThatSelectsNothing pins the lax half on the same
// overlay: what strict refuses, lax applies around, and still names each
// action that changed nothing as a warning, since the point of lax is to let
// the document through, not to hide what did not apply. The action after the
// one that missed still applies.
func TestApply_LaxWarnsOfAnActionThatSelectsNothing(t *testing.T) {
	t.Parallel()
	origin, root, diags := applied(t, spec, v11+
		"  - target: $.paths['/nope']\n    update: {description: x}\n"+
		"  - target: $.components.schemas.Pet\n    copy: $.nowhere\n"+
		"  - target: $.info\n    update: {description: d}\n", true)

	require.True(t, origin.Applied())
	sameCodes(t, []string{"warning " + diag.OverlayAction, "warning " + diag.OverlayAction}, diags)
	assert.Contains(t, diags[0].Message, "its target selects nothing")
	assert.Contains(t, diags[1].Message, "its copy selector selects nothing")
	_, description := member(t, root, "info", "description")
	assert.Equal(t, "d", description.Value, "the rest of the overlay applied")
}

// TestApply_WarnsOfAnActionThatChangesNothingWithoutRefusing pins the
// distinction the strict switch turns on, which a test of the refusal alone
// reads as one rule instead of two. An action whose selector matched and whose
// update then changed nothing is redundant, not wrong: the fix it describes is
// already in the source. It is worth reporting and not worth refusing over, in
// either mode.
func TestApply_WarnsOfAnActionThatChangesNothingWithoutRefusing(t *testing.T) {
	t.Parallel()
	for _, lax := range []bool{false, true} {
		origin, _, diags := applied(t, spec, v10+"  - target: $.info\n    update: {title: Original}\n", lax)

		assert.True(t, origin.Applied(), "lax=%v: a redundant action is not a refusal", lax)
		sameCodes(t, []string{"warning " + diag.OverlayAction}, diags)
		assert.Contains(t, diags[0].Message, "$.info", "lax=%v: the action is named", lax)
		assert.Equal(t, 0, origin.Attribution().WriterAt("/info/title"),
			"lax=%v: and nothing is attributed to it, because it wrote nothing", lax)
	}
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
			origin, _, diags := applied(t, spec, data, false)

			assert.False(t, origin.Applied())
			require.True(t, diag.HasError(diags), "%q must be refused: %+v", data, diags)
			assert.Equal(t, diag.OverlayInvalid, diags[0].Code)
			assert.Equal(t, overlayIndex, diags[0].Provenance.Source,
				"the diagnostic is about the overlay, not the spec")
		})
	}
}

// TestApply_RefusesASelectorBeforeApplyingAnything pins where a selector that
// does not parse is refused: as the overlay is read, in either mode, so no
// action has landed when it is, and the tree is the source's.
func TestApply_RefusesASelectorBeforeApplyingAnything(t *testing.T) {
	t.Parallel()
	for _, lax := range []bool{false, true} {
		origin, root, diags := applied(t, spec, v10+
			"  - target: $.info\n    update: {description: d}\n"+
			"  - target: \"$[\"\n    update: {a: b}\n", lax)

		assert.False(t, origin.Applied(), "lax=%v", lax)
		sameCodes(t, []string{"error " + diag.OverlayInvalid}, diags)
		assert.Equal(t, jsontext.Pointer("/actions/1"), diags[0].Provenance.Pointer)
		sameValue(t, spec, root)
	}
}

// TestApply_RecordsTheOverlayAsAnInputDocument pins the SourceInfo that makes an
// attributed position resolvable: Document.Sources is what a Provenance.Source
// indexes into, so an index naming an entry that does not describe the overlay
// is a dangling reference dressed as a valid one.
func TestApply_RecordsTheOverlayAsAnInputDocument(t *testing.T) {
	t.Parallel()
	body := v10 + "  - target: $.info\n    update: {description: d}\n"
	origin, _ := applyTo(t, body)

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
// no overlay. It is what lets provenance.Doc ask the same question either way,
// rather than each caller first asking whether there was an overlay at all.
func TestOrigin_ZeroValueAttributesNothing(t *testing.T) {
	t.Parallel()
	var zero overlay.Origin

	assert.False(t, zero.Applied())
	assert.Equal(t, 0, zero.Attribution().WriterAt("/components/schemas/Pet"))
	assert.Equal(t, 0, zero.Attribution().WriterOf("/components/schemas/Pet",
		[]jsontext.Pointer{"/components/schemas/Pet/if", "/components/schemas/Pet/then"}))
	assert.Equal(t, ir.SourceInfo{}, zero.Source())
	_, found := zero.Attribution().At(&yaml.Node{Kind: yaml.ScalarNode})
	assert.False(t, found, "no node is the overlay's when none was applied")
	_, found = zero.Attribution().At(nil)
	assert.False(t, found)
}

// applyKeeping is applyTo for a case that needs the tree the overlay left
// behind, to hand At the nodes it will be asked about.
func applyKeeping(t *testing.T, ov string) (overlay.Origin, *yaml.Node) {
	t.Helper()
	origin, root, diags := applied(t, spec, ov, false)
	require.False(t, diag.HasError(diags), "overlay did not apply: %+v", diags)
	require.True(t, origin.Applied())
	return origin, root
}

// TestAt_AnswersForTheNodesTheOverlayIntroduced pins the node-keyed half of
// the attribution: a grafted node, and every node beneath it, answers with the
// pointer of the position it sits at, one WriterAt names the overlay the writer
// of, while a node the source declared answers nothing and is left to the
// caller's own reading of it.
//
// The pointer is asserted for the reason TestApply_AttributesIntroducedPositions
// gives: a right writer with a wrong pointer would send a reader to a position
// the finding is not about.
func TestAt_AnswersForTheNodesTheOverlayIntroduced(t *testing.T) {
	t.Parallel()
	origin, root := applyKeeping(t, v10+`  - target: $.components.schemas
    update:
      Owner: {type: object, properties: {id: {type: string}}}
  - target: $.tags
    update: {name: added}
`)

	_, owner := member(t, root, "components", "schemas", "Owner")
	_, id := member(t, root, "components", "schemas", "Owner", "properties", "id")
	_, pet := member(t, root, "components", "schemas", "Pet")
	_, tags := member(t, root, "tags")

	for node, want := range map[*yaml.Node]jsontext.Pointer{
		owner:           "/components/schemas/Owner",
		id:              "/components/schemas/Owner/properties/id",
		tags.Content[1]: "/tags/1",
	} {
		got, found := origin.Attribution().At(node)
		require.True(t, found, "the set is closed downwards: %s", want)
		assert.Equal(t, want, got)
	}
	_, found := origin.Attribution().At(pet)
	assert.False(t, found, "a node the source declared is the source's own")
}

// TestAt_AnswersForAKeyTheOverlayIntroduced pins the one node the pointer walk
// never addresses. A new member's key is a node too, and a finding anchored on
// it is about what the overlay wrote, so the key answers as the overlay's, at
// its member's pointer.
func TestAt_AnswersForAKeyTheOverlayIntroduced(t *testing.T) {
	t.Parallel()
	origin, root := applyKeeping(t, v10+"  - target: $.components.schemas.Pet.properties\n    update:\n      tag: {type: string}\n")

	key, _ := member(t, root, "components", "schemas", "Pet", "properties", "tag")
	got, found := origin.Attribution().At(key)
	require.True(t, found)
	assert.Equal(t, jsontext.Pointer("/components/schemas/Pet/properties/tag"), got)
	assert.Zero(t, key.Line, "the key keeps no line of the overlay document, which is not the one it lands in")

	name, _ := member(t, root, "components", "schemas", "Pet", "properties", "name")
	_, found = origin.Attribution().At(name)
	assert.False(t, found, "a key the source declared is the source's own")
}

// TestAt_AnswersForARewrittenScalar pins that the node answer agrees with the
// pointer answer on the half node identity alone would miss: a scalar
// overwritten in place still has the source's line and column, but what it
// holds is the overlay's, and that is what a finding about it is about.
func TestAt_AnswersForARewrittenScalar(t *testing.T) {
	t.Parallel()
	origin, root := applyKeeping(t, v10+"  - target: $.info\n    update:\n      title: Renamed\n")

	_, title := member(t, root, "info", "title")
	_, version := member(t, root, "info", "version")
	require.NotZero(t, title.Line, "the value is overwritten in place; the node keeps the source's position")

	got, found := origin.Attribution().At(title)
	require.True(t, found)
	assert.Equal(t, jsontext.Pointer("/info/title"), got)

	_, found = origin.Attribution().At(version)
	assert.False(t, found, "the sibling it did not touch is unaffected")
}
