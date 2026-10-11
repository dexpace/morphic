package load

import (
	"strings"
	"testing"

	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi/internal/defs"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
	"github.com/dexpace/morphic/compilers/openapi/internal/scan"
	"github.com/dexpace/morphic/ir"
)

// buildDoc runs a source through the same steps build runs before the
// reference-cycle check: decode, release anchors, populate the model. It
// returns the populated document and the raw tree chainCycle reads alongside
// it, so a test can drive reach's own entry points directly instead of paying
// for the whole Load pipeline.
func buildDoc(t *testing.T, spec string) (*soa.OpenAPI, *yaml.Node) {
	t.Helper()
	parsed, err := parsedFor(compilers.Source{Path: "spec.yaml", Data: []byte(spec)})
	require.NoError(t, err)
	releaseAnchors(parsed.root)
	doc, _, err := unmarshal(t.Context(), []byte(spec), parsed.root)
	require.NoError(t, err)
	require.NotNil(t, doc)
	return doc, parsed.root
}

// assertRefused runs spec through the public Load entry point and requires
// that it come back refused as exactly one cyclic-reference error. It is used
// rather than a direct chainCycle call for fixtures where that matters: the
// pre-parse pointer-chain scan (scan.Cycles) already refuses a $defs pointer
// spelled as a full path from root, such as
// "#/components/schemas/A/$defs/n" — to that scan it is an ordinary pointer —
// before chainCycle ever runs, and which stage catches a given fixture is not
// a claim either GitHub issue makes. What must hold end to end is that the
// process never reaches the parser.
func assertRefused(t *testing.T, spec string) {
	t.Helper()
	doc, diags, err := Load(t.Context(), 0, compilers.Source{Path: "spec.yaml", Data: []byte(spec)}, Options{})
	require.NoError(t, err, "a reference cycle is a spec problem, not a Go error")
	assert.Nil(t, doc, "the document must be refused rather than lowered")
	assert.Equal(t, 1, countErrorsAt(diags, diag.CyclicRef))
}

// assertNotRefusedByReach drives chainCycle directly rather than through Load,
// isolating the one claim this file makes for a "left alone" fixture: the
// cycle refusal itself did not fire. Several of the suite-derived fixtures
// below carry an external $ref that AllowExternalRefs leaves unresolved by
// design (a separate, unrelated diagnostic); routing through Load would
// conflate that with what this helper checks.
func assertNotRefusedByReach(t *testing.T, spec string) {
	t.Helper()
	doc, root := buildDoc(t, spec)
	_, found := chainCycle(t.Context(), scan.InSource(0), root, doc)
	assert.False(t, found, "must not be refused as a reference cycle")
}

// The fixtures below were probed directly against speakeasy-api/openapi
// v1.25.2's own resolver: every one crashes it with a stack overflow before
// this package's reference-chain refusal can run ahead of the parser. They exercise $defs-relative resolution, which depends on
// the chain that reaches a $defs entry and on registry re-registration
// mid-resolution (GitHub #546), and are additional to the $anchor/$id shapes
// #526 was filed against.
const (
	// A $defs entry's own $ref names itself by a /$defs/ pointer.
	defsPointerSelfReference = `openapi: 3.1.0
info: {title: t, version: "1"}
paths: {}
components:
  schemas:
    A:
      $defs:
        n: {$ref: "#/$defs/n"}
`
	// Two $defs entries under one schema name each other.
	defsPointerMutualCycle = `openapi: 3.1.0
info: {title: t, version: "1"}
paths: {}
components:
  schemas:
    A:
      $defs:
        m: {$ref: "#/$defs/n"}
        n: {$ref: "#/$defs/m"}
`
	// The schema's own $ref reaches into its own $defs entry, which names
	// itself.
	defsPointerTopLevelSelfReference = `openapi: 3.1.0
info: {title: t, version: "1"}
paths: {}
components:
  schemas:
    A:
      $ref: "#/$defs/n"
      $defs:
        n: {$ref: "#/$defs/n"}
`
	// Same shape as defsPointerSelfReference, but the schema also declares its
	// own $id — re-registration under a fresh base changes nothing about the
	// $defs pointer's own resolution.
	defsPointerSelfReferenceUnderID = `openapi: 3.1.0
info: {title: t, version: "1"}
paths: {}
components:
  schemas:
    A:
      $id: "https://x.test/a"
      $defs:
        n: {$ref: "#/$defs/n"}
`
	// A $defs entry's plain pointer reaches a sibling component, whose own
	// pointer walks back into the $defs entry by a full path from root rather
	// than the "#/$defs/..." shorthand. Neither $anchor/$id nor a reference the
	// resolver reads as a /$defs/ pointer appears anywhere, so chainCycle's own gate declines
	// this one (see TestChainCycle_GateDefersPurePointerCyclesToThePreParseScan)
	// and the pre-parse pointer-chain scan carries the refusal instead.
	defsPointerCyclesThroughSibling = `openapi: 3.1.0
info: {title: t, version: "1"}
paths: {}
components:
  schemas:
    A:
      $defs:
        n: {$ref: "#/components/schemas/B"}
    B: {$ref: "#/components/schemas/A/$defs/n"}
`
	// B's $defs hold a self-contained cycle (y -> z -> y) that never involves
	// A; A's own $defs are legitimate (x -> y, a concrete string). Declared in
	// this order the library resolves A fully before ever registering B, and
	// crashes once it reaches B's cycle.
	defsPointerPoisonPrevents = `openapi: 3.1.0
info: {title: t, version: "1"}
paths: {}
components:
  schemas:
    A:
      $defs:
        y: {type: string}
        x: {$ref: "#/$defs/y"}
    B:
      $defs:
        y: {$ref: "#/$defs/z"}
        z: {$ref: "#/$defs/y"}
`
	// Same two schemas as defsPointerPoisonPrevents, B declared first. The
	// library still crashes — this pairing is the two-order test in
	// TestReachCycle_OrderInvariant, not just a second crash fixture.
	defsPointerPoisonPreventsReversed = `openapi: 3.1.0
info: {title: t, version: "1"}
paths: {}
components:
  schemas:
    B:
      $defs:
        y: {$ref: "#/$defs/z"}
        z: {$ref: "#/$defs/y"}
    A:
      $defs:
        y: {type: string}
        x: {$ref: "#/$defs/y"}
`
	// The $defs entry sits two levels down, inside a property rather than
	// directly under the schema.
	defsPointerNestedInProperty = `openapi: 3.1.0
info: {title: t, version: "1"}
paths: {}
components:
  schemas:
    A:
      type: object
      properties:
        p:
          $defs:
            n: {$ref: "#/$defs/n"}
`
	// The pointer names a path past the $defs entry itself (into its own
	// "properties/q"), not the entry's own position.
	defsPointerRestOfPathAfterDefs = `openapi: 3.1.0
info: {title: t, version: "1"}
paths: {}
components:
  schemas:
    A:
      $defs:
        n:
          properties:
            q: {$ref: "#/$defs/n/properties/q"}
`
	// The $defs entry sits inside a parameter's inline schema rather than a
	// component schema.
	defsPointerInsideParameterSchema = `openapi: 3.1.0
info: {title: t, version: "1"}
paths:
  /x:
    get:
      parameters:
        - name: q
          in: query
          schema:
            $defs:
              n: {$ref: "#/$defs/n"}
      responses: {"200": {description: ok}}
`
	// Two components each declare their own $defs/n; A's is legitimate (a
	// concrete string A's own property points at), B's names itself. The two
	// $defs blocks share the key "n", which is exactly what the over-
	// approximation's global-by-key union conflates (see TestReachCycle_
	// KnownFalseRefusals for the same mechanism catching a document neither
	// ordering of this one crashes on).
	defsPointerTwoSchemasShareDefsName = `openapi: 3.1.0
info: {title: t, version: "1"}
paths: {}
components:
  schemas:
    A:
      $defs:
        n: {type: string}
      properties:
        p: {$ref: "#/$defs/n"}
    B:
      $defs:
        n: {$ref: "#/$defs/n"}
`
	// Same two schemas as defsPointerTwoSchemasShareDefsName, B declared
	// first. Declared this way the library crashes; declared the other way it
	// does not — see TestReachCycle_OrderInvariant.
	defsPointerTwoSchemasShareDefsNameReversed = `openapi: 3.1.0
info: {title: t, version: "1"}
paths: {}
components:
  schemas:
    B:
      $defs:
        n: {$ref: "#/$defs/n"}
    A:
      $defs:
        n: {type: string}
      properties:
        p: {$ref: "#/$defs/n"}
`
)

// The three fixtures below are GitHub #546's own motivating shapes: mixing
// $anchor, $id and $defs so that a same-document pointer resolves only after
// an earlier reference re-registers the schema it targets under a fresh base
// (setupRemoteSchemaRegistry, jsonschema/oas3/resolution_registry.go).
// defsIDResolvesOnlyAfterReRegistration is the clearest single case:
// pre-resolution, a.json resolves against a.json and misses; once q's own
// $ref re-registers A against the source location, its nested $defs/n finds
// itself.
const (
	// $anchor "a" is declared three times at different scopes (the schema, a
	// nested $defs entry, and q's own copy), and q's $defs mix a relative $id
	// self-reference with an absolute one — every registry re-registration
	// this file's doc comment describes, stacked in one document.
	mixedAnchorIDDefsReRegistration = `openapi: 3.1.0
info: {title: t, version: "1"}
paths: {}
components:
  schemas:
    A: {"type": "object", "$anchor": "a", "$defs": {"m": {"type": "object", "$defs": {"n": {"type": "object", "$anchor": "a"}}}}, "properties": {"q": {"$ref": "#/components/schemas/A", "$anchor": "a", "$defs": {"n": {"$ref": "a.json", "$id": "a.json"}, "m": {"$ref": "https://x.test/a"}}}}}
`
	// A property's own $ref re-registers A under the source document's own
	// location; only after that does its nested $defs/n's relative $id
	// resolve to the same base its own $ref names, closing the cycle.
	defsIDResolvesOnlyAfterReRegistration = `openapi: 3.1.0
info: {title: t, version: "1"}
paths: {}
components:
  schemas:
    A: {"type": "object", "properties": {"q": {"$ref": "#/components/schemas/A", "$defs": {"n": {"$ref": "a.json", "$id": "a.json"}}}}}
`
	// The relative-$id self-reference of relativeIDSelfReference (below, among
	// #526's own shapes), with $ref written before $id.
	relativeIDSelfReferenceRefFirst = `openapi: 3.1.0
info: {title: t, version: "1"}
paths: {}
components:
  schemas:
    A: {"$ref": "a.json", "$id": "a.json"}
`
)

// relativeIDSelfReferenceDotSlash is relativeIDSelfReference's shape spelled
// with a leading "./": "./a.json" and "a.json" are the same URI after
// resolution (a no-op path segment), so the library still crashes. Only
// keyOfURI's reading of the last path segment, not a literal string compare,
// sees them as the same target; a literal comparison would let this crash through.
const relativeIDSelfReferenceDotSlash = `openapi: 3.1.0
info: {title: t, version: "1"}
paths: {}
components:
  schemas:
    A: {$id: "a.json", $ref: "./a.json"}
`

// craftedCrashFixtures pairs every fixture above with a short label; every one
// must come back refused end to end (TestReachCycle_RefusesCraftedCrashes).
var craftedCrashFixtures = []struct{ name, spec string }{
	{"a $defs entry names itself", defsPointerSelfReference},
	{"two $defs entries name each other", defsPointerMutualCycle},
	{"the schema's own $ref reaches a self-naming $defs entry", defsPointerTopLevelSelfReference},
	{"a $defs self-reference under a schema that also declares $id", defsPointerSelfReferenceUnderID},
	{"a $defs pointer cycles through a sibling component (gated to the pre-parse scan)", defsPointerCyclesThroughSibling},
	{"a poisoned $defs pair, poison declared second", defsPointerPoisonPreventsReversed},
	{"a $defs entry nested inside a property", defsPointerNestedInProperty},
	{"a $defs pointer names a path past the entry itself", defsPointerRestOfPathAfterDefs},
	{"a $defs entry inside a parameter's inline schema", defsPointerInsideParameterSchema},
	{"two schemas share a $defs name, the self-reference declared second", defsPointerTwoSchemasShareDefsNameReversed},
	{"$anchor/$id/$defs mixed and re-registered", mixedAnchorIDDefsReRegistration},
	{"a $defs $id self-reference resolves only after re-registration", defsIDResolvesOnlyAfterReRegistration},
	{"a relative $id names itself, $ref written first", relativeIDSelfReferenceRefFirst},
	{"a relative $id names itself through a leading ./", relativeIDSelfReferenceDotSlash},

	// The fixtures below are GitHub #526's own $anchor/$id shapes. Each was
	// run through speakeasy-api/openapi v1.25.2's own resolver directly, not
	// through this package, so the comment on each records what the resolver
	// does rather than what this package's model predicts.
	{"top-level anchor names itself", anchorSelfTop},
	{"nested property anchor names itself", anchorSelfNested},
	{"two nested anchors name each other", anchorMutualNested},
	{"a parameter's inline schema anchor names itself", anchorSelfParameter},
	{"a pointer hop and an anchor hop meet on one cycle", anchorPointerMixedCycle},
	{"$id names itself as its own $ref", idSelfReference},
	{"$id plus a pointer names itself", idPlusPointerSelfReference},
	{"a relative $id names itself", relativeIDSelfReference},
	{"$id-scoped anchor names itself by fragment", idScopedAnchorSelfReference},
	{"$id-scoped anchor names itself by absolute URI", absoluteURIAnchorSelfReference},
	{"a duplicate anchor's self-reference registers first", duplicateAnchorSelfRefRegistersFirst},
	{"an anchor inside $defs names itself", anchorSelfReferenceInsideDefs},
	{"the self-reference sits beside a sibling keyword", anchorSelfReferenceBesideSiblingKeyword},
	{"a pointer lands on a schema whose own anchor names itself", pointerIntoSelfReferencingAnchor},
	{"an outer anchor is reached through a cycling inner anchor", outerAnchorThroughCyclingInnerAnchor},
	{"an $id-scoped inner anchor names itself", idScopedInnerAnchorSelfReference},
	{"the self-reference sits beside not: true", anchorSelfReferenceBesideNot},
	{"a pointer lands on a fresh extension whose anchor names itself", pointerLandsOnFreshExtensionAnchorCycle},
	{"a pointer lands on a fresh example whose anchor names itself", pointerLandsOnFreshExampleAnchorCycle},
}

// TestReachCycle_RefusesCraftedCrashes drives every crafted crash fixture
// through the public Load entry point and requires that it come back refused
// rather than reaching the parser. See assertRefused for why Load rather than
// chainCycle directly.
func TestReachCycle_RefusesCraftedCrashes(t *testing.T) {
	t.Parallel()
	for _, tc := range craftedCrashFixtures {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assertRefused(t, tc.spec)
		})
	}
}

// GitHub #526's own anchor fixtures the raw library resolver ends without
// crashing, probed the same way as the crash block above, plus a handful of
// $defs and pointer shapes this file adds. Every one must be left alone.
const (
	crossComponentAnchorStaysUnresolved = `openapi: 3.1.0
info: {title: t, version: "1"}
paths: {}
components:
  schemas:
    A: {$anchor: a, $ref: "#b"}
    B: {$anchor: b, $ref: "#a"}
`
	anchorDeclaredRecursionIsLegal = `openapi: 3.1.0
info: {title: t, version: "1"}
paths: {}
components:
  schemas:
    A:
      $anchor: a
      type: object
      properties:
        next: {$ref: "#a"}
`
	dynamicAnchorIsNotRegistered = `openapi: 3.1.0
info: {title: t, version: "1"}
paths: {}
components:
  schemas:
    A: {$dynamicAnchor: a, $ref: "#a"}
`
	percentEncodedFragmentIsReadRaw = `openapi: 3.1.0
info: {title: t, version: "1"}
paths: {}
components:
  schemas:
    A: {$anchor: a, $ref: "#%61"}
`
	trailingSpaceFragmentIsNotTrimmed = `openapi: 3.1.0
info: {title: t, version: "1"}
paths: {}
components:
  schemas:
    A: {$anchor: a, $ref: "#a "}
`
	innerReferenceToAnOuterAnchor = `openapi: 3.1.0
info: {title: t, version: "1"}
paths: {}
components:
  schemas:
    A:
      $anchor: a
      $ref: "#/components/schemas/Z"
      properties:
        p: {$ref: "#a"}
    Z: {type: string}
`
	outerReferenceToAnInnerAnchor = `openapi: 3.1.0
info: {title: t, version: "1"}
paths: {}
components:
  schemas:
    A:
      $ref: "#i"
      properties:
        p: {$anchor: i, type: string}
`
	nestedPropertyReferencesASiblingAnchor = `openapi: 3.1.0
info: {title: t, version: "1"}
paths: {}
components:
  schemas:
    A:
      type: object
      properties:
        p: {$anchor: sib, type: string}
        q: {$ref: "#sib"}
`
	allOfBranchReferencesAnchorInSameSchema = `openapi: 3.1.0
info: {title: t, version: "1"}
paths: {}
components:
  schemas:
    A:
      allOf:
        - {$anchor: sa, type: string}
        - {$ref: "#sa"}
`
	referenceToAnchorNestedTwoLevelsDeep = `openapi: 3.1.0
info: {title: t, version: "1"}
paths: {}
components:
  schemas:
    A:
      type: object
      properties:
        x:
          type: object
          properties:
            y: {$anchor: deep, type: string}
        z: {$ref: "#deep"}
`
	referenceToAnchorDeclaredOnItemsSchema = `openapi: 3.1.0
info: {title: t, version: "1"}
paths: {}
components:
  schemas:
    A:
      type: object
      properties:
        list:
          type: array
          items: {$anchor: elem, type: string}
        first: {$ref: "#elem"}
`
	// A plain pointer chain with no $anchor/$id/$defs at all — the gate's own
	// fast path (TestChainCycle_GateSkipsPlainPointerDocuments).
	plainPointerChain = `openapi: 3.1.0
info: {title: t, version: "1"}
paths: {}
components:
  schemas:
    A: {$ref: "#/components/schemas/B"}
    B: {type: string}
`
	// A legitimate self-contained $defs pointer: the entry it names is a
	// concrete type, not another reference.
	defsLegitPointer = `openapi: 3.1.0
info: {title: t, version: "1"}
paths: {}
components:
  schemas:
    A:
      $ref: "#/$defs/n"
      $defs:
        n: {type: string}
`
	// A legitimate $defs pointer chain three hops deep (c -> b -> a), ending on
	// a concrete type.
	defsLegitChain = `openapi: 3.1.0
info: {title: t, version: "1"}
paths: {}
components:
  schemas:
    A:
      $ref: "#/$defs/c"
      $defs:
        a: {type: integer}
        b: {$ref: "#/$defs/a"}
        c: {$ref: "#/$defs/b"}
`
	// A $defs entry's $ref and $id are unrelated ("b.json" vs "a.json"), so no
	// self-reference is possible even though the shape otherwise matches
	// defsIDResolvesOnlyAfterReRegistration.
	defsIDMismatchedRefNeverResolves = `openapi: 3.1.0
info: {title: t, version: "1"}
paths: {}
components:
  schemas:
    A: {"type": "object", "properties": {"q": {"$ref": "#/components/schemas/A", "$defs": {"n": {"$ref": "b.json", "$id": "a.json"}}}}}
`
	// An external reference is never fetched with external refs disabled by
	// default, so it can be neither refused as a cycle nor resolved.
	uriRefUnresolved = `openapi: 3.1.0
info: {title: t, version: "1"}
paths: {}
components:
  schemas:
    A: {$ref: "https://nowhere.test/x"}
`
	// $id and its referencing $ref share one registry only when neither sits
	// at the top level of components.schemas on its own — every schema outside
	// another is its own document with its own registry (the same rule
	// crossComponentAnchorStaysUnresolved pins for $anchor) — so both live
	// nested inside one shared parent here.
	idRefWithNoFragmentResolves = `openapi: 3.1.0
info: {title: t, version: "1"}
paths: {}
components:
  schemas:
    Root:
      type: object
      properties:
        a: {$id: "https://x.test/a", type: string}
        b: {$ref: "https://x.test/a"}
`
)

var leftAloneFixtures = []struct{ name, spec string }{
	{"two components reuse the same $defs names for unrelated definitions", defsSameNamesAcrossComponents},
	{"a $defs pointer does not reach an extension's $defs", defsPointerDoesNotReachAnExtension},
	{"an alias in a $defs target reads its pointer from the root", defsTargetHoldsAnAlias},
	{"a merge key in a $defs target reads its pointer from the root", defsTargetHoldsAMergeKey},
	{"a cross-component anchor reference stays unresolved", crossComponentAnchorStaysUnresolved},
	{"anchor-declared recursion is legal", anchorDeclaredRecursionIsLegal},
	{"a $dynamicAnchor is never registered", dynamicAnchorIsNotRegistered},
	{"a percent-encoded fragment is read raw", percentEncodedFragmentIsReadRaw},
	{"a trailing-space fragment is not trimmed", trailingSpaceFragmentIsNotTrimmed},
	{"an inner reference reaches an outer anchor", innerReferenceToAnOuterAnchor},
	{"an outer reference reaches an inner anchor", outerReferenceToAnInnerAnchor},
	{"a nested property references a sibling anchor", nestedPropertyReferencesASiblingAnchor},
	{"an allOf branch references an anchor in the same schema", allOfBranchReferencesAnchorInSameSchema},
	{"a reference reaches an anchor nested two levels deep", referenceToAnchorNestedTwoLevelsDeep},
	{"a reference reaches an anchor declared on an items schema", referenceToAnchorDeclaredOnItemsSchema},
	{"a plain pointer chain carries no registry keys at all", plainPointerChain},
	{"a legitimate $defs pointer names a concrete type", defsLegitPointer},
	{"a legitimate three-hop $defs pointer chain", defsLegitChain},
	{"a mismatched $defs $id/$ref pair never resolves", defsIDMismatchedRefNeverResolves},
	{"an external ref is left unresolved, not refused", uriRefUnresolved},
	{"an $id reference with no fragment resolves to the whole schema", idRefWithNoFragmentResolves},
	{"a YAML alias reaches an anchored schema", aliasedSchemaAndItsAlias},
}

// TestReachCycle_LeavesALegitimateChain drives every "left alone" fixture
// through chainCycle directly (see assertNotRefusedByReach) and requires that
// none of them be refused.
func TestReachCycle_LeavesALegitimateChain(t *testing.T) {
	t.Parallel()
	for _, tc := range leftAloneFixtures {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assertNotRefusedByReach(t, tc.spec)
		})
	}
}

// The $anchor/$id fixtures below (GitHub #526) were each run through
// speakeasy-api/openapi v1.25.2's own resolver directly, not through this
// package; the comment on each records what the resolver does.
// Every one crashes the raw library with a stack overflow.
const (
	// A top-level schema's $ref names the $anchor it declares itself.
	anchorSelfTop = `openapi: 3.1.0
info: {title: t, version: "1"}
paths: {}
components:
  schemas:
    A: {$anchor: a, $ref: "#a"}
`
	// A nested property's $ref names its own $anchor.
	anchorSelfNested = `openapi: 3.1.0
info: {title: t, version: "1"}
paths: {}
components:
  schemas:
    A:
      type: object
      properties:
        p: {$anchor: p, $ref: "#p"}
`
	// Two nested anchors name each other.
	anchorMutualNested = `openapi: 3.1.0
info: {title: t, version: "1"}
paths: {}
components:
  schemas:
    A:
      type: object
      properties:
        p: {$anchor: p, $ref: "#q"}
        q: {$anchor: q, $ref: "#p"}
`
	// A parameter's inline schema anchors and names itself.
	anchorSelfParameter = `openapi: 3.1.0
info: {title: t, version: "1"}
paths:
  /x:
    get:
      parameters:
        - name: q
          in: query
          schema: {$anchor: a, $ref: "#a"}
      responses: {"200": {description: ok}}
`
	// One property's plain pointer names a sibling whose own anchor lookup
	// points back at the pointer: an anchor lookup and a pointer hop meet on
	// the same cycle.
	anchorPointerMixedCycle = `openapi: 3.1.0
info: {title: t, version: "1"}
paths: {}
components:
  schemas:
    A:
      type: object
      properties:
        x: {$ref: "#y"}
        y: {$anchor: y, $ref: "#/components/schemas/A/properties/x"}
`
	// A schema's $id names itself as its own $ref.
	idSelfReference = `openapi: 3.1.0
info: {title: t, version: "1"}
paths: {}
components:
  schemas:
    A: {$id: "https://x.test/a", $ref: "https://x.test/a"}
`
	// A $ref combines the schema's own $id with a pointer back into it.
	idPlusPointerSelfReference = `openapi: 3.1.0
info: {title: t, version: "1"}
paths: {}
components:
  schemas:
    A:
      $id: "https://x.test/a"
      type: object
      properties:
        p: {$ref: "https://x.test/a#/properties/p"}
`
	// A relative $id names itself as its own $ref.
	relativeIDSelfReference = `openapi: 3.1.0
info: {title: t, version: "1"}
paths: {}
components:
  schemas:
    A: {$id: "a.json", $ref: "a.json"}
`
	// A schema carries both $id and $anchor, and its $ref names its own anchor
	// by fragment alone.
	idScopedAnchorSelfReference = `openapi: 3.1.0
info: {title: t, version: "1"}
paths: {}
components:
  schemas:
    A: {$id: "https://x.test/a", $anchor: a, $ref: "#a"}
`
	// Same shape as idScopedAnchorSelfReference, but the $ref spells the
	// anchor as an absolute URI plus fragment rather than a bare fragment.
	absoluteURIAnchorSelfReference = `openapi: 3.1.0
info: {title: t, version: "1"}
paths: {}
components:
  schemas:
    A: {$id: "https://x.test/a", $anchor: a, $ref: "https://x.test/a#a"}
`
	// Two properties declare the same $anchor; the second — the one that
	// names itself — is declared first, so it is the one the registry keeps.
	duplicateAnchorSelfRefRegistersFirst = `openapi: 3.1.0
info: {title: t, version: "1"}
paths: {}
components:
  schemas:
    A:
      type: object
      properties:
        q: {$anchor: d, $ref: "#d"}
        p: {$anchor: d, type: string}
`
	// The anchor and its self-reference both sit inside a $defs entry: an
	// anchor lookup, not a $defs pointer.
	anchorSelfReferenceInsideDefs = `openapi: 3.1.0
info: {title: t, version: "1"}
paths: {}
components:
  schemas:
    A:
      $defs:
        n: {$anchor: n, $ref: "#n"}
`
	// The self-referencing schema also carries an ordinary keyword beside
	// $anchor and $ref, which OpenAPI 3.1 permits.
	anchorSelfReferenceBesideSiblingKeyword = `openapi: 3.1.0
info: {title: t, version: "1"}
paths: {}
components:
  schemas:
    A: {$anchor: a, $ref: "#a", type: object}
`
	// A plain pointer lands on a schema whose own anchor names itself: the
	// pointer hop and the anchor hop are on one chain.
	pointerIntoSelfReferencingAnchor = `openapi: 3.1.0
info: {title: t, version: "1"}
paths: {}
components:
  schemas:
    A: {$ref: "#/components/schemas/B"}
    B: {$anchor: b, $ref: "#b"}
`
	// The outer schema anchors itself as "top" and points at an inner anchor
	// "i"; the inner one points back at "top".
	outerAnchorThroughCyclingInnerAnchor = `openapi: 3.1.0
info: {title: t, version: "1"}
paths: {}
components:
  schemas:
    A:
      $anchor: top
      $ref: "#i"
      properties:
        p: {$anchor: i, $ref: "#top"}
`
	// The inner anchor sits on a schema that also declares its own $id,
	// starting a new resource, and names itself.
	idScopedInnerAnchorSelfReference = `openapi: 3.1.0
info: {title: t, version: "1"}
paths: {}
components:
  schemas:
    A:
      $ref: "#i"
      properties:
        p: {$id: "https://y.test/p", $anchor: i, $ref: "#i"}
`
	// The self-referencing schema sits beside a "not: true" keyword.
	anchorSelfReferenceBesideNot = `openapi: 3.1.0
info: {title: t, version: "1"}
paths: {}
components:
  schemas:
    A: {$anchor: a, $ref: "#a", not: true}
`
	// A same-document pointer lands on an extension value the library
	// unmarshals as a standalone schema document of its own, and that
	// standalone schema's anchor names itself.
	pointerLandsOnFreshExtensionAnchorCycle = `openapi: 3.1.0
info: {title: t, version: "1"}
paths: {}
x-s: {$anchor: z, $ref: "#z"}
components:
  schemas:
    A: {$ref: "#/x-s"}
`
	// Same shape as pointerLandsOnFreshExtensionAnchorCycle, but the raw node
	// is reached through a schema's "example" rather than an extension.
	pointerLandsOnFreshExampleAnchorCycle = `openapi: 3.1.0
info: {title: t, version: "1"}
paths: {}
components:
  schemas:
    A:
      type: object
      example: {$anchor: z, $ref: "#z"}
    B: {$ref: "#/components/schemas/A/example"}
`
)

// TestReachCycle_KnownFalseRefusals pins the price of order-invariance: each
// document does not crash the raw library, yet reach's over-approximation
// refuses it. Each comment names the over-approximation. Accepting any of them
// would need the model to track resolver state (registration order, which base
// a relative $id resolves against), which is what makes an exact model
// order-dependent.
func TestReachCycle_KnownFalseRefusals(t *testing.T) {
	t.Parallel()

	t.Run("a duplicate anchor's own declaration is one of its own candidate targets", func(t *testing.T) {
		t.Parallel()
		// Two properties declare the same $anchor; the plain one (p) is
		// declared first, so the library's registry keeps it and q's "#d"
		// resolves to a concrete schema rather than cycling
		// (duplicateAnchorPlainSchemaRegistersFirst's own shape, probed the same
		// way as the crash block above). An anchor lookup does not model
		// registration order — it lands on every node in scope declaring the
		// anchor — so q's
		// own declaration is itself one of the candidates for q's own lookup,
		// and the model treats "the registry kept q's entry" as a live
		// possibility, closing a self-loop the library never actually takes.
		const spec = `openapi: 3.1.0
info: {title: t, version: "1"}
paths: {}
components:
  schemas:
    A:
      type: object
      properties:
        p: {$anchor: d, type: string}
        q: {$anchor: d, $ref: "#d"}
`
		assertRefusedByReach(t, spec)
	})

	t.Run("a relative $id with no $id-scoped ancestor searches the whole tree", func(t *testing.T) {
		t.Parallel()
		// n has no $id-scoped ancestor of its own — A declares no $id — so n's
		// scope is never recorded in reach.scope, so decls.identified looks it up under
		// the nil scope, the whole-tree bucket every out-of-model node shares.
		// That search always turns up n's own $id declaration alongside its
		// matching last path segment, closing a self-loop through n's own $ref. The library's
		// own per-call base resolution does not take this path for real: this
		// is the same relative-$id shape as relativeIDSelfReference above, but
		// nested inside $defs rather than declared at the top level.
		const spec = `openapi: 3.1.0
info: {title: t, version: "1"}
paths: {}
components:
  schemas:
    A: {"$defs": {"n": {"$ref": "a.json", "$id": "a.json"}}}
`
		assertRefusedByReach(t, spec)
	})

	t.Run("a query-only $ref takes any $id in its document", func(t *testing.T) {
		t.Parallel()
		// A reference with no path of its own resolves against its base's, which
		// the model does not know, so it can name any $id in scope. Here the
		// $id's own last segment is "a", but the library's lookup of "?x=1"
		// needs an $id that is itself "?x=1", so it does not crash.
		const spec = `openapi: 3.1.0
info: {title: t, version: "1"}
paths: {}
components:
  schemas:
    A: {$id: "http://x.test/y/a", $ref: "?x=1"}
`
		assertRefusedByReach(t, spec)
	})

	t.Run("a pointer names the extension that holds it", func(t *testing.T) {
		t.Parallel()
		// x-s's own "#/x-s" names x-s itself, read from the root as every plain
		// pointer is, so the chain A -> x-s -> x-s closes. The library does not
		// crash: it unmarshals x-s as a standalone document when A resolves into
		// it, and the second hop finds the first's cached copy under the same
		// absolute reference, which its own cycle check reports. The pre-parse
		// scan refuses this shape too; reach refuses it here because the $anchor
		// key sends the document through reach at all.
		const spec = `openapi: 3.1.0
info: {title: t, version: "1"}
paths: {}
x-s: {$anchor: z, $ref: "#/x-s"}
components:
  schemas:
    A: {$ref: "#/x-s"}
`
		assertRefusedByReach(t, spec)
	})
}

// assertRefusedByReach drives chainCycle directly and requires that it refuse
// spec as a cyclic reference. It is the mirror of assertNotRefusedByReach, for
// the false-refusal table above where the whole point is that reach's own
// verdict — not the pre-parse scan's, which cannot see a $defs-relative
// pointer at all — is what fires.
func assertRefusedByReach(t *testing.T, spec string) {
	t.Helper()
	doc, root := buildDoc(t, spec)
	d, found := chainCycle(t.Context(), scan.InSource(0), root, doc)
	require.True(t, found, "must be refused as a reference-chain cycle")
	assert.Equal(t, ir.SeverityError, d.Severity)
	assert.Equal(t, diag.CyclicRef, d.Code)
}

// TestReachCycle_OrderInvariant proves the property an exact model of the
// resolver's own state cannot have (see reach's own doc comment): the same
// two schemas, declared in either order, get the SAME verdict, even though
// the raw library crashes on only one of the two orderings.
// defsPointerPoisonPrevents(Reversed) and
// defsPointerTwoSchemasShareDefsName(Reversed) are declared in the order that
// crashes the library above; here each is paired with the declaration order
// that does not, and both members of a pair must still be refused.
func TestReachCycle_OrderInvariant(t *testing.T) {
	t.Parallel()
	pairs := []struct{ name, orderA, orderB string }{
		{
			name:   "a poisoned $defs pair, either component declared first",
			orderA: defsPointerPoisonPrevents,
			orderB: defsPointerPoisonPreventsReversed,
		},
		{
			name:   "two schemas sharing a $defs name, either one declared first",
			orderA: defsPointerTwoSchemasShareDefsName,
			orderB: defsPointerTwoSchemasShareDefsNameReversed,
		},
	}
	for _, tc := range pairs {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, foundA := reachDiagFor(t, tc.orderA)
			_, foundB := reachDiagFor(t, tc.orderB)
			assert.True(t, foundA, "order A must be refused")
			assert.True(t, foundB, "order B must be refused")
			assert.Equal(t, foundA, foundB,
				"the over-approximation must reach the same verdict regardless of declaration order")
		})
	}
}

// reachDiagFor runs chainCycle over spec directly, for tests that compare two
// specs' verdicts against each other rather than asserting one fixed outcome.
func reachDiagFor(t *testing.T, spec string) (ir.Diagnostic, bool) {
	t.Helper()
	doc, root := buildDoc(t, spec)
	return chainCycle(t.Context(), scan.InSource(0), root, doc)
}

// TestChainCycle_GateSkipsPlainPointerDocuments drives chainCycle's own fast
// path: a tree with no $anchor/$id and no reference the resolver reads as a
// /$defs/ pointer is every chain the pre-parse pointer-chain scan already
// covers, so chainCycle must decline without running reach at all.
func TestChainCycle_GateSkipsPlainPointerDocuments(t *testing.T) {
	t.Parallel()
	_, found := reachDiagFor(t, plainPointerChain)
	assert.False(t, found, "no $anchor/$id/$defs pointer anywhere; the gate must skip reach entirely")
}

// TestChainCycle_GateDefersPurePointerCyclesToThePreParseScan drives the other
// half of the same gate on a document that DOES cycle: defsPointerCyclesThrough
// Sibling's $ref values are ordinary pointers from root — one of them merely
// happens to pass through a path segment spelled "$defs" — so neither gate
// condition fires and chainCycle must decline, even though the document is
// still refused end to end by the pre-parse scan underneath it.
func TestChainCycle_GateDefersPurePointerCyclesToThePreParseScan(t *testing.T) {
	t.Parallel()
	_, found := reachDiagFor(t, defsPointerCyclesThroughSibling)
	assert.False(t, found, "a full root path that merely passes through a \"$defs\" segment is not a $defs-relative pointer")
	assertRefused(t, defsPointerCyclesThroughSibling)
}

// TestNeedsChainModel pins needsChainModel (chains.go), chainCycle's gate: it
// fires for any scalar that spells $anchor or $id, wherever it sits and
// however a key reaches its mapping, and for a reference the resolver reads as
// a /$defs/ pointer, however it is spelled. It does not fire for a full path
// from root that merely passes through a "$defs" segment (that shape is
// defsPointerCyclesThroughSibling's, left to the pre-parse scan), nor when a
// second '#' puts the $defs pointer past the one the resolver reads.
func TestNeedsChainModel(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		src  string
		want bool
	}{
		{"an anchor in a schema position", "components:\n  schemas:\n    A: {$anchor: a, type: string}\n", true},
		{"an id under an extension", "paths: {}\nx-foo: {$id: \"https://x.test/a\"}\n", true},
		{"an anchor inside an example value", "components:\n  schemas:\n    A:\n      type: object\n      example: {$anchor: z, type: string}\n", true},
		{"an anchor on a node also reached by a YAML alias", "components:\n  schemas:\n    A: &shared {$anchor: z, type: string}\n    B: *shared\n", true},
		{"a key an alias supplies, spelled only as a value", "x-k: &k $anchor\ncomponents:\n  schemas:\n    A: {*k : a, $ref: \"#a\"}\n", true},
		{"a literal #/$defs/ pointer", "components:\n  schemas:\n    A: {$ref: \"#/$defs/n\"}\n", true},
		{"a percent-encoded $ in the pointer", "components:\n  schemas:\n    A: {$ref: \"#/%24defs/n\"}\n", true},
		{"a percent-encoded slash after $defs", "components:\n  schemas:\n    A: {$ref: \"#/$defs%2Fn\"}\n", true},
		{"a second # after the pointer", "components:\n  schemas:\n    A: {$ref: \"#/$defs/n#x\"}\n", true},
		{"a $defs pointer past a second #", "components:\n  schemas:\n    A: {$ref: \"#/components/schemas/B#/$defs/n\"}\n", false},
		{"a full path from root through a $defs segment", "components:\n  schemas:\n    A:\n      $defs:\n        n: {$ref: \"#/components/schemas/B\"}\n    B: {$ref: \"#/components/schemas/A/$defs/n\"}\n", false},
		{"no registry key and no $defs pointer", "openapi: 3.1.0\npaths: {}\ncomponents:\n  schemas:\n    A: {type: object, properties: {p: {type: string}}}\n", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var root yaml.Node
			require.NoError(t, yaml.Unmarshal([]byte(tc.src), &root))
			assert.Equal(t, tc.want, needsChainModel(&root))
		})
	}
}

// TestBuild_ChainCheckWarningIsCarriedNotRefused drives the branch build takes
// when the reference-chain check reports its own protection incomplete rather
// than a cycle: merge keys nested past the view's bound stop it short, the
// document still lowers, and the warning rides along. The pre-parse scan warns
// about the same chain, so the check's own message is what is looked for.
func TestBuild_ChainCheckWarningIsCarriedNotRefused(t *testing.T) {
	t.Parallel()
	spec := mergeChain(nodeview.MergeDepthLimit + 6)
	doc, diags, err := Load(t.Context(), 0, compilers.Source{Path: "spec.yaml", Data: []byte(spec)}, Options{})
	require.NoError(t, err)
	require.NotNil(t, doc, "a warning carries forward; it does not refuse the document")

	var carried []ir.Diagnostic
	for _, d := range diags {
		if d.Code == diag.CycleScanFailed && strings.Contains(d.Message, "reference-chain scan stopped") {
			carried = append(carried, d)
		}
	}
	require.Len(t, carried, 1, "the check's warning rides along in the diagnostic list: %v", diags)
	assert.Equal(t, ir.SeverityWarning, carried[0].Severity)
}

// TestRecoverChains_PanicYieldsWarning pins recoverChains' panic path
// (chains.go): a panicking walk degrades to a warning rather than crashing the
// compile it guards.
func TestRecoverChains_PanicYieldsWarning(t *testing.T) {
	t.Parallel()
	d, found := recoverChains(scan.InSource(3), func() (ir.Diagnostic, bool) {
		panic("detector bug")
	})
	require.True(t, found, "a panicking walk still reports something")
	assert.Equal(t, diag.CycleScanFailed, d.Code)
	assert.Equal(t, ir.SeverityWarning, d.Severity, "the scan failure must not refuse a spec")
	assert.Equal(t, 3, d.Provenance.Source, "the diagnostic carries the source index")
}

// TestRecoverChains_PassesThroughResult pins recoverChains' non-panicking
// path: whatever the walk returns passes straight through.
func TestRecoverChains_PassesThroughResult(t *testing.T) {
	t.Parallel()
	want := ir.Diagnostic{Code: diag.CyclicRef, Severity: ir.SeverityError}
	d, found := recoverChains(scan.InSource(0), func() (ir.Diagnostic, bool) {
		return want, true
	})
	assert.True(t, found)
	assert.Equal(t, want, d)
}

// TestDrift_MarksTheDefsTargetAndPropagatesThroughItsOwnPointer drives drift's
// fixpoint (reach.go) from a "#/$defs/..." reference load cannot hold, one in an
// extension: it lands on n directly — no fixpoint needed for that hop — but n's
// own $ref is a PLAIN pointer to B, not itself spelled "#/$defs/...". Only
// re-scanning n's own reference after marking it drifted discovers that this
// plain pointer must also be read as resolvable from any node, which is what
// lets it reach B. Without the drift pass, n is marked as a reference but never
// revisited, so B is never marked drifted at all.
func TestDrift_MarksTheDefsTargetAndPropagatesThroughItsOwnPointer(t *testing.T) {
	t.Parallel()
	const spec = `openapi: 3.1.0
info: {title: t, version: "1"}
paths: {}
x-a: {$ref: "#/$defs/n"}
components:
  schemas:
    A:
      $defs:
        n: {$ref: "#/components/schemas/B"}
    B: {type: string}
`
	doc, root := buildDoc(t, spec)
	r, _ := newReach(t.Context(), maxReachWork, root, doc)

	bNode := r.tree.walk(r.tree.root, []string{"components", "schemas", "B"})
	xNode := r.tree.walk(r.tree.root, []string{"x-a"})
	require.NotNil(t, bNode)
	require.NotNil(t, xNode)

	assert.True(t, r.drifted[bNode],
		"B is reached only through n's own pointer, discovered by revisiting n once it drifted")
	assert.False(t, r.drifted[xNode],
		"the referring node itself is never marked drifted, only what a /$defs/ target's subtree reaches")
}

// TestDrift_BareFragmentSelfReferenceNeedsTheNodeItselfMarkedFirst drives the
// specific shape where the fixpoint is load-bearing rather than merely
// observable: n's own $ref is a bare "#", and an empty fragment, unlike a
// pointer, lands nowhere from an undrifted node (there is no root fallback for
// it). The cycle through n is visible only once markDrifted's own return value
// re-queues n for a second look at its own reference.
func TestDrift_BareFragmentSelfReferenceNeedsTheNodeItselfMarkedFirst(t *testing.T) {
	t.Parallel()
	const spec = `openapi: 3.1.0
info: {title: t, version: "1"}
paths: {}
x-a: {$ref: "#/$defs/n"}
components:
  schemas:
    A:
      $defs:
        n: {$ref: "#"}
`
	assertRefusedByReach(t, spec)
}

// landings is every node a reference at n can land on, by its class's lookups.
func landings(r *reach, n *yaml.Node) []*yaml.Node {
	var out []*yaml.Node
	for _, s := range r.lookups(r.classOf(n)) {
		out = append(out, s.members...)
	}
	return out
}

// TestReach_DefsEdgeIsTheRulesTarget pins that a held "#/$defs/..." reference's
// edge is the one definition the resolver's rule names for it (defs.Reader.Target),
// the edge resolveHeld later hands the resolver — not every node holding the
// path. A nested reference reaches its schema's own definition; the schema's
// own sibling $ref does not, because the rule starts at the reference's parent
// (GitHub #557).
func TestReach_DefsEdgeIsTheRulesTarget(t *testing.T) {
	t.Parallel()
	const spec = `openapi: 3.1.0
info: {title: t, version: "1"}
paths: {}
components:
  schemas:
    A:
      $ref: "#/$defs/n"
      properties:
        p: {$ref: "#/$defs/n"}
      $defs:
        n: {type: string}
    B:
      $defs:
        n: {type: integer}
`
	doc, root := buildDoc(t, spec)
	r, _ := newReach(t.Context(), maxReachWork, root, doc)
	at := func(tokens ...string) *yaml.Node {
		return r.tree.walk(r.tree.root, append([]string{"components", "schemas"}, tokens...))
	}
	a, p, aDef := at("A"), at("A", "properties", "p"), at("A", "$defs", "n")
	require.NotNil(t, a)
	require.NotNil(t, p)
	require.NotNil(t, aDef)

	held, ok := r.held[p]
	require.True(t, ok, "p's pointer is held out of the resolver's own pass")
	assert.Equal(t, []*yaml.Node{aDef}, held.members, "p lands on its own schema's definition, never B's")

	held, ok = r.held[a]
	require.True(t, ok)
	assert.Empty(t, held.members, "a schema's own sibling $ref does not see its own $defs")
	assert.Equal(t, []*yaml.Node{aDef}, landings(r, p))
	assert.Empty(t, landings(r, a), "held with no definition is no pointer read either")
}

// TestReach_ReadsHeldDefinitionsThroughOneReader pins the same bound for the
// reach check: its held references are read through the one reader it was
// given, so a reference at every level of a schema nested d deep costs a
// constant number of navigations per level rather than a walk of the chain
// above it.
func TestReach_ReadsHeldDefinitionsThroughOneReader(t *testing.T) {
	t.Parallel()
	const depth = 60
	doc, root := buildDoc(t, deepDefsDoc(depth))
	r := emptyReach(root, &budget{limit: maxReachWork})
	rule := defs.NewReader(doc)
	r.rule = rule

	r.collect(t.Context(), doc)

	require.Len(t, r.held, depth)
	assert.Same(t, rule, r.rule, "no reference swapped the reader for one with no memory")
	assert.GreaterOrEqual(t, rule.Reads(), depth, "every level was read, through the one reader")
	assert.LessOrEqual(t, rule.Reads(), 9*depth, "a constant number of navigations per level")
}

// TestReach_EmptyFragmentLandsNowhere pins that "#" and "#/" name the document
// the resolver holds, which is always the root now that no $defs lookup hands it
// another: no schema, so no edge, even under a $defs target.
func TestReach_EmptyFragmentLandsNowhere(t *testing.T) {
	t.Parallel()
	const spec = `openapi: 3.1.0
info: {title: t, version: "1"}
paths: {}
components:
  schemas:
    A:
      properties:
        p: {$ref: "#/$defs/n"}
      $defs:
        n: {$ref: "#"}
        m: {$ref: "#/"}
`
	doc, root := buildDoc(t, spec)
	r, _ := newReach(t.Context(), maxReachWork, root, doc)
	defsNode := r.tree.walk(r.tree.root, []string{"components", "schemas", "A", "$defs"})
	require.NotNil(t, defsNode)
	assert.Empty(t, landings(r, r.tree.child(defsNode, "n")))
	assert.Empty(t, landings(r, r.tree.child(defsNode, "m")))
	assertNotRefusedByReach(t, spec)
}

// aliasedSchemaAndItsAlias declares a real YAML alias (as opposed to every
// other fixture's $anchor/$id, which are JSON Schema keywords the library
// registers — an unrelated mechanism): B's value is a YAML alias to A's node.
// It carries $anchor so the gate runs reach at all, exercising the tree
// walk's own alias-dereferencing (tree.index) alongside needsChainModel's own
// alias case in TestNeedsChainModel. Neither schema carries a $ref, so nothing
// cycles.
const aliasedSchemaAndItsAlias = `openapi: 3.1.0
info: {title: t, version: "1"}
paths: {}
components:
  schemas:
    A: &shared {$anchor: a, type: string}
    B: *shared
`

// Each schema's own "#/$defs/..." pointer lands on that schema's own
// definition (the resolver's rule, GitHub #557): A.x reaches A.y and B.y
// reaches B.x, and both chains end there. Taking every schema's $defs as a
// possible target would close a cycle A.x -> B.y -> A.x that neither resolver
// order takes.
const defsSameNamesAcrossComponents = `openapi: 3.1.0
info: {title: t, version: "1"}
paths: {}
components:
  schemas:
    A:
      $defs:
        x: {$ref: "#/$defs/y"}
        y: {type: string}
    B:
      $defs:
        y: {$ref: "#/$defs/x"}
        x: {type: integer}
`

// A's "#/$defs/n" names nothing: the resolver's rule searches A's ancestors,
// never the document root, and never a sibling extension. So the extension's
// n, which points back at A, is on no chain from A, and a check that took any
// node holding the path, x-holder's included, would refuse the round trip.
const defsPointerDoesNotReachAnExtension = `openapi: 3.1.0
info: {title: t, version: "1"}
paths: {}
components:
  schemas:
    A: {$ref: "#/$defs/n"}
x-holder:
  $defs:
    n: {$ref: "#/components/schemas/A"}
`

// d's subtree holds q, an alias of Shared's own node. Shared's
// "#/properties/y" is a plain pointer, read from the root, where there is no
// such property, so it lands nowhere. A check that marked the aliased node
// drifted once a $defs pointer reached d would read that pointer from any node.
const defsTargetHoldsAnAlias = `openapi: 3.1.0
info: {title: t, version: "1"}
paths: {}
components:
  schemas:
    Shared: &s {$ref: "#/properties/y"}
    Top:
      $defs: {d: {properties: {q: *s}}}
      properties:
        x: {$ref: "#/$defs/d"}
        y: {$ref: "#/components/schemas/Top/$defs/d/properties/q"}
`

// The same for a merge key: the merged-in properties belong to d as far as the
// library's model is concerned, and their plain pointer is still read from the
// root.
const defsTargetHoldsAMergeKey = `openapi: 3.1.0
info: {title: t, version: "1"}
paths: {}
components:
  schemas:
    Base: &b {properties: {z: {$ref: "#/properties/y"}}}
    Top:
      $defs: {d: {<<: *b}}
      properties:
        x: {$ref: "#/$defs/d"}
        y: {$ref: "#/components/schemas/Top/$defs/d/properties/z"}
`
