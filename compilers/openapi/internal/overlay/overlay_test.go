package overlay_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/jsontext"
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
// misattribution rather than passing unexercised. Slot is a second schema for
// the copy case to land on, since copying a subtree onto itself changes nothing
// and would attribute nothing either.
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

	introduced := []jsontext.Pointer{
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

	declared := []jsontext.Pointer{
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

// TestApply_StrictReportsAnActionThatChangesNothingWithoutRefusing pins the
// distinction the strict switch turns on, which a test of the refusal alone
// reads as one rule instead of two.
//
// An action whose selector matched and whose update then changed nothing is
// redundant, not wrong: the fix it describes is already in the source. It is
// worth reporting and not worth refusing over, so the warning arrives without
// the error beside it and the document still compiles — unlike the selector that
// matched nothing, which is a typo and is fatal.
func TestApply_StrictReportsAnActionThatChangesNothingWithoutRefusing(t *testing.T) {
	t.Parallel()
	origin, diags := applyTo(t, header+"  - target: $.info\n    update: {title: Original}\n", false)

	assert.True(t, origin.Applied(), "a redundant action is not a refusal")
	require.Len(t, diags, 1, "and it is still reported: %+v", diags)
	assert.Equal(t, diag.OverlayAction, diags[0].Code)
	assert.Equal(t, ir.SeverityWarning, diags[0].Severity)
	assert.Contains(t, diags[0].Message, "$.info", "the action is named")

	assert.Equal(t, srcIndex, origin.IndexAt("/info/title", srcIndex),
		"and nothing is attributed to it, because it wrote nothing")
}

// TestApply_AttributesACopiedSubtree pins the third action type. A copy grafts
// nodes cloned from elsewhere in the same document, so the positions it fills
// are the overlay's doing even though every byte of their content was already in
// the source — which is the case an attribution keyed on content rather than on
// what the overlay did would get backwards.
func TestApply_AttributesACopiedSubtree(t *testing.T) {
	t.Parallel()
	origin, _ := applyTo(t, header+
		"  - target: $.components.schemas.Slot\n    copy: $.components.schemas.Pet\n", false)

	for _, p := range []jsontext.Pointer{
		"/components/schemas/Slot/properties",
		"/components/schemas/Slot/properties/name",
		"/components/schemas/Slot/properties/name/type",
		"/components/schemas/Slot/type", // string, overwritten in place by object
	} {
		assert.Equal(t, overlayIndex, origin.IndexAt(p, srcIndex), "the copy put %s here", p)
	}
	assert.Equal(t, srcIndex, origin.IndexAt("/components/schemas/Slot", srcIndex),
		"but the position it was copied onto is the source's own")
	assert.Equal(t, srcIndex, origin.IndexAt("/components/schemas/Pet/properties/name", srcIndex),
		"and so is everything it was copied from")
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
	_, found := zero.At(&yaml.Node{Kind: yaml.ScalarNode})
	assert.False(t, found, "no node is the overlay's when none was applied")
	_, found = zero.At(nil)
	assert.False(t, found)
}

// applyKeeping is applyTo for a case that needs the tree the overlay left
// behind, to hand At the nodes it will be asked about.
func applyKeeping(t *testing.T, ov string) (overlay.Origin, *yaml.Node) {
	t.Helper()
	root := treeOf(t)
	origin, diags := overlay.Apply(overlayIndex, root,
		overlay.Options{Path: "o.yaml", Data: []byte(ov), Lax: false})
	require.False(t, diag.HasError(diags), "overlay did not apply: %+v", diags)
	require.True(t, origin.Applied())
	return origin, root
}

// member returns the key and value nodes of a mapping member, reached from the
// document root along a path of mapping keys.
func member(t *testing.T, root *yaml.Node, keys ...string) (key, value *yaml.Node) {
	t.Helper()
	n := root.Content[0]
	for _, want := range keys {
		found := false
		for i := 0; i+1 < len(n.Content); i += 2 {
			if n.Content[i].Value == want {
				key, value, found = n.Content[i], n.Content[i+1], true
				break
			}
		}
		require.True(t, found, "no %q under the path %v", want, keys)
		n = value
	}
	return key, value
}

// TestAt_AnswersForTheNodesTheOverlayIntroduced pins the node-keyed half of
// the attribution: a grafted node, and every node beneath it, answers with the
// overlay's index and the pointer of the position it sits at — the answer
// IndexAt gives the lowering for that pointer — while a node the source
// declared answers nothing and is left to the caller's own reading of it.
//
// The pointer is asserted rather than the index alone for the reason
// TestApply_AttributesIntroducedPositions gives: a right index with a wrong
// pointer would send a reader to a position the finding is not about.
func TestAt_AnswersForTheNodesTheOverlayIntroduced(t *testing.T) {
	t.Parallel()
	origin, root := applyKeeping(t, header+`  - target: $.components.schemas
    update:
      Owner: {type: object, properties: {id: {type: string}}}
`)

	_, owner := member(t, root, "components", "schemas", "Owner")
	_, id := member(t, root, "components", "schemas", "Owner", "properties", "id")
	_, pet := member(t, root, "components", "schemas", "Pet")

	got, found := origin.At(owner)
	require.True(t, found)
	assert.Equal(t, ir.Provenance{Source: overlayIndex, Pointer: "/components/schemas/Owner"}, got)

	got, found = origin.At(id)
	require.True(t, found, "the set is closed downwards")
	assert.Equal(t, ir.Provenance{Source: overlayIndex, Pointer: "/components/schemas/Owner/properties/id"}, got)

	_, found = origin.At(pet)
	assert.False(t, found, "a node the source declared is the source's own")
}

// TestAt_AnswersForAKeyTheOverlayIntroduced pins the one node the pointer walk
// never addresses. The library appends a new key uncloned from the overlay
// document, so unlike the value beside it the key carries a line and column —
// the overlay file's. A finding anchored on it and read as the source's would
// name a real position in the wrong file, so the key answers as the overlay's.
func TestAt_AnswersForAKeyTheOverlayIntroduced(t *testing.T) {
	t.Parallel()
	origin, root := applyKeeping(t, header+`  - target: $.components.schemas.Pet.properties
    update:
      tag: {type: string}
`)

	key, value := member(t, root, "components", "schemas", "Pet", "properties", "tag")
	require.NotZero(t, key.Line, "the library keeps the overlay document's position on the key")
	require.Zero(t, value.Line, "and drops it from the cloned value")

	got, found := origin.At(key)
	require.True(t, found)
	assert.Equal(t, ir.Provenance{Source: overlayIndex, Pointer: "/components/schemas/Pet/properties/tag"}, got)

	name, _ := member(t, root, "components", "schemas", "Pet", "properties", "name")
	_, found = origin.At(name)
	assert.False(t, found, "a key the source declared is the source's own")
}

// TestAt_AnswersForARewrittenScalar pins that the node answer agrees with the
// pointer answer on the half node identity alone would miss: a scalar the
// library overwrote in place still has the source's line and column, but what
// it holds is the overlay's, and that is what a finding about it is about.
func TestAt_AnswersForARewrittenScalar(t *testing.T) {
	t.Parallel()
	origin, root := applyKeeping(t, header+`  - target: $.info
    update:
      title: Renamed
`)

	_, title := member(t, root, "info", "title")
	_, version := member(t, root, "info", "version")
	require.NotZero(t, title.Line, "the library overwrites the value in place; the node keeps the source's position")

	got, found := origin.At(title)
	require.True(t, found)
	assert.Equal(t, ir.Provenance{Source: overlayIndex, Pointer: "/info/title"}, got)

	_, found = origin.At(version)
	assert.False(t, found, "the sibling it did not touch is unaffected")
}

// detachedAliases returns how many aliases in the tree under root point at a
// node no Content walk from root reaches.
//
// It is the question every other reading in this compiler asks without knowing
// it asks: each walks Content and treats an alias as a leaf, so content hanging
// off an alias and nowhere else is content none of them can see.
func detachedAliases(root *yaml.Node) (detached int) {
	seen := map[*yaml.Node]bool{}
	var mark func(*yaml.Node)
	mark = func(n *yaml.Node) {
		if n == nil || seen[n] {
			return
		}
		seen[n] = true
		for _, child := range n.Content {
			mark(child)
		}
	}
	mark(root)
	for n := range seen {
		if n.Kind == yaml.AliasNode && n.Alias != nil && !seen[n.Alias] {
			detached++
		}
	}
	return detached
}

// TestApply_GraftsNothingThatOnlyAnAliasCanReach pins the repair GitHub #477 is
// about, at both doors the library grafts through.
//
// Its clone copies an alias by copying its target — `newNode.Alias =
// clone(node.Alias)` — so a graft arrives holding an alias that points at a
// node in no Content list anywhere. Every reading here walks Content and treats
// an alias as a leaf, so whatever the graft carried is invisible to the node
// budget, the cycle scan, the tagged-mapping refusal and this package's own
// attribution alike, while the parser follows the alias and reads it.
//
// An update is not the only graft: a copy action clones a subtree of the source
// through the same clone, which is why the repair works on the applied tree
// rather than on the overlay document.
func TestApply_GraftsNothingThatOnlyAnAliasCanReach(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct{ spec, ov string }{
		"an update whose value is an alias": {
			spec: "openapi: 3.1.0\npaths: {}\n",
			ov: "overlay: 1.0.0\ninfo: {title: O, version: \"1\", x-t: &t {a: 1}}\n" +
				"actions:\n  - target: $.paths\n    update: {p: *t}\n",
		},
		"a copy of a subtree holding an alias": {
			spec: "openapi: 3.1.0\nx-t: &s {description: d}\nx-shared: {inner: *s}\npaths: {}\n",
			ov:   header + "  - target: $.paths\n    copy: $[\"x-shared\"]\n",
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			var root yaml.Node
			require.NoError(t, yaml.Unmarshal([]byte(tc.spec), &root))

			_, diags := overlay.Apply(overlayIndex, &root,
				overlay.Options{Path: "o.yaml", Data: []byte(tc.ov)})
			require.False(t, diag.HasError(diags), "the overlay applies: %+v", diags)

			detached := detachedAliases(&root)
			assert.Zero(t, detached, "every node the overlay grafted is reachable the way this compiler reads")
		})
	}
}

// TestApply_LeavesTheSourcesOwnAliasesAlone is the control. An alias whose
// target the tree holds is ordinary YAML and the readings here cope with it by
// design — the content lives at the anchor's own position, which they reach
// there. Substituting it would expand a document the author wrote compactly and
// change what the node budget measures.
func TestApply_LeavesTheSourcesOwnAliasesAlone(t *testing.T) {
	t.Parallel()
	const spec = "openapi: 3.1.0\ninfo: {title: T, version: \"1\"}\nx-t: &s {description: d}\nx-use: *s\npaths: {}\n"
	var root yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte(spec), &root))
	before := detachedAliases(&root)
	require.Zero(t, before, "the source's own alias is reachable to begin with")
	_, use := member(t, &root, "x-use")
	require.Equal(t, yaml.AliasNode, use.Kind, "the fixture's alias lands where this test reads it")

	_, diags := overlay.Apply(overlayIndex, &root,
		overlay.Options{Path: "o.yaml", Data: []byte(header + "  - target: $.info\n    update: {description: d}\n")})
	require.False(t, diag.HasError(diags), "%+v", diags)

	detached := detachedAliases(&root)
	assert.Zero(t, detached)

	_, after := member(t, &root, "x-use")
	assert.Equal(t, yaml.AliasNode, after.Kind,
		"the alias is still an alias, not the subtree it names expanded in place")
	assert.Empty(t, after.Content, "and still a leaf")
}

// TestApply_ResolvesAnAliasInsideWhatItGrafts pins that substituting a graft
// finishes the job. The content an alias stands for may name another anchor,
// and that one is no more reachable than the first was — so a substitution that
// copied the target as it found it would put a fresh detached alias exactly
// where it had just removed one.
func TestApply_ResolvesAnAliasInsideWhatItGrafts(t *testing.T) {
	t.Parallel()
	var root yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte("openapi: 3.1.0\npaths: {}\n"), &root))
	const ov = "overlay: 1.0.0\ninfo: {title: O, version: \"1\", x-in: &in {deep: 1}, x-out: &out {a: *in}}\n" +
		"actions:\n  - target: $.paths\n    update: {p: *out}\n"

	_, diags := overlay.Apply(overlayIndex, &root, overlay.Options{Path: "o.yaml", Data: []byte(ov)})
	require.False(t, diag.HasError(diags), "the overlay applies: %+v", diags)

	detached := detachedAliases(&root)
	assert.Zero(t, detached, "the alias inside the grafted content is resolved too")
	assert.Zero(t, aliasNodes(&root), "so the graft holds no alias at all")

	_, deep := member(t, &root, "paths", "p", "a", "deep")
	assert.Equal(t, "1", deep.Value, "and what it stood for is where it was grafted")
}

// TestApply_AGraftedCopyCarriesNoAnchor pins what the substitution drops. An
// anchor names a node, and the node it named is not the one being written; a
// copy keeping the name would spell one anchor at two positions, which is not a
// document yaml.v3 would have produced and not one this compiler should invent.
func TestApply_AGraftedCopyCarriesNoAnchor(t *testing.T) {
	t.Parallel()
	var root yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte("openapi: 3.1.0\npaths: {}\n"), &root))
	const ov = "overlay: 1.0.0\ninfo: {title: O, version: \"1\", x-t: &t {a: 1}}\n" +
		"actions:\n  - target: $.paths\n    update: {p: *t}\n"

	_, diags := overlay.Apply(overlayIndex, &root, overlay.Options{Path: "o.yaml", Data: []byte(ov)})
	require.False(t, diag.HasError(diags), "%+v", diags)

	_, grafted := member(t, &root, "paths", "p")
	require.Equal(t, yaml.MappingNode, grafted.Kind, "the alias was replaced by what it stood for")
	assert.Empty(t, grafted.Anchor, "and the copy does not answer to the name of the node it came from")
}

// aliasNodes counts the alias nodes a Content walk from root reaches.
func aliasNodes(root *yaml.Node) int {
	count := 0
	seen := map[*yaml.Node]bool{}
	var mark func(*yaml.Node)
	mark = func(n *yaml.Node) {
		if n == nil || seen[n] {
			return
		}
		seen[n] = true
		if n.Kind == yaml.AliasNode {
			count++
		}
		for _, child := range n.Content {
			mark(child)
		}
	}
	mark(root)
	return count
}
