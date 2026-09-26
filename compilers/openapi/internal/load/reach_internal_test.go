package load

import (
	"context"
	"testing"

	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
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

// The fixtures below (d* and bis/b* names in their comments refer to the
// files they were mined as) were probed directly against
// speakeasy-api/openapi v1.25.2's own resolver: every one crashes it with a
// stack overflow before this package's reference-chain refusal can run ahead
// of the parser. They exercise $defs-relative resolution, which depends on
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
	// than the "#/$defs/..." shorthand. Neither $anchor/$id nor a literal
	// "#/$defs/" substring appears anywhere, so chainCycle's own gate declines
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
	// The same relative-$id self-reference as relativeIDSelfReference (below,
	// among #526's own shapes), generated independently as part of #546's own
	// probing.
	relativeIDSelfReferenceBis = `openapi: 3.1.0
info: {title: t, version: "1"}
paths: {}
components:
  schemas:
    A: {"$ref": "a.json", "$id": "a.json"}
`
)

// relativeIDSelfReferenceDotSlash is p10/relativeIDSelfReferenceBis's shape
// spelled with a leading "./": "./a.json" and "a.json" are the same URI after
// resolution (a no-op path segment), so the library still crashes, but only
// idTargets' last-path-segment comparison — not a literal string compare —
// sees them as the same target. Used by the M3 mutation below.
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
	{"a relative $id names itself (bis)", relativeIDSelfReferenceBis},
	{"a relative $id names itself through a leading ./", relativeIDSelfReferenceDotSlash},

	// The fixtures below are GitHub #526's own $anchor/$id shapes, reused
	// unchanged from the probing that first found them: every one was mined by
	// running speakeasy-api/openapi v1.25.2's own resolver directly (not this
	// package) over the shape, so the comment on each records what the probe
	// found rather than a claim about this package's own (unsound) earlier
	// model.
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
// crashing — mined the same way as the crash block above — plus a handful of
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

// The $anchor/$id fixtures below (GitHub #526) were mined by probing
// speakeasy-api/openapi v1.25.2 directly (its own resolver, not this
// package) over each shape; the comment on each records what the probe found.
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
// of these documents does NOT crash the raw library (mined the same way as
// the fixtures above), yet reach's sound over-approximation refuses it
// anyway. Each comment explains which over-approximation the refusal comes
// from. Removing any of these from "refused" would need the model to encode a
// piece of the library's actual resolution state (registration order, or
// which document a pointer's first token is scoped to) that
// docs/prior-art.md and this file's own package doc explain the model
// deliberately does not track, because that state is what makes the exact
// model order-dependent (see reach's own doc comment).
func TestReachCycle_KnownFalseRefusals(t *testing.T) {
	t.Parallel()

	t.Run("two components reuse the same $defs key names for unrelated definitions", func(t *testing.T) {
		t.Parallel()
		// A $defs pointer's first token ("$defs") is looked up across the WHOLE
		// document (reach.byKey), not scoped to the component that declares it,
		// because the library re-registers a $defs entry's own base from
		// whichever chain reached it — a chain no static read reproduces. A's
		// "#/$defs/y" and B's "#/$defs/x" therefore each see both components'
		// $defs blocks, and the union closes A.x -> B.y -> A.x even though each
		// component's own pointer, resolved for real, only ever reaches its own
		// sibling.
		const spec = `openapi: 3.1.0
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
		assertRefusedByReach(t, spec)
	})

	t.Run("a $defs pointer round-trips through an extension", func(t *testing.T) {
		t.Parallel()
		// A's "#/$defs/n" pointer lands on x-holder's $defs.n (the global $defs
		// union again). Once a node is reached through a /$defs/ pointer this
		// way it is marked "drifted", and a drifted node's OWN pointer — even
		// one that does not spell "/$defs/" — resolves from any node sharing
		// its first path segment, so n's "#/components/schemas/A" is treated as
		// reaching A and closing a cycle. The library itself does not crash:
		// resolving into an extension unmarshals it as a standalone document
		// (pointerLandsOnFreshExtensionAnchorCycle's own mechanism), and that
		// standalone document has no "components" key of its own for the
		// pointer to navigate, so the reference is left unresolved instead.
		const spec = `openapi: 3.1.0
info: {title: t, version: "1"}
paths: {}
components:
  schemas:
    A: {$ref: "#/$defs/n"}
x-holder:
  $defs:
    n: {$ref: "#/components/schemas/A"}
`
		assertRefusedByReach(t, spec)
	})

	t.Run("a duplicate anchor's own declaration is one of its own candidate targets", func(t *testing.T) {
		t.Parallel()
		// Two properties declare the same $anchor; the plain one (p) is
		// declared first, so the library's registry keeps it and q's "#d"
		// resolves to a concrete schema rather than cycling
		// (duplicateAnchorPlainSchemaRegistersFirst's own shape, mined the same
		// way as the crash block above). declaring does not model registration
		// order — it returns every node in scope declaring the anchor — so q's
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
		// scope is never recorded in reach.scope, and declaring's scope lookup
		// (r.decls[r.scope[n]]) reads the zero value nil, which is the
		// whole-tree bucket every out-of-model node also shares. That search
		// always turns up n's own $id declaration alongside its matching last
		// path segment, closing a self-loop through n's own $ref. The library's
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

	t.Run("bonus: a pointer round-trips through an extension without an anchor", func(t *testing.T) {
		t.Parallel()
		// An additional false refusal found alongside the four above, kept
		// because it is the same "drifted node's own plain pointer resolves
		// from any node" mechanism as the extension case above, spelled with a
		// pointer instead of an anchor: x-s's own "#/x-s" is a literal
		// self-pointer, but the library unmarshals x-s as a standalone document
		// only once something resolves into it, and that fresh copy's own
		// "#/x-s" is read against the standalone document (which has no such
		// key), not against the shared tree — so it does not crash for real.
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
// path: a tree with no $anchor/$id and no literal "#/$defs/" pointer is every
// chain the pre-parse pointer-chain scan already covers, so chainCycle must
// decline without running reach at all.
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

// TestDeclaresRegistryKeys pins declaresRegistryKeys (chains.go), the
// $anchor/$id half of chainCycle's gate.
func TestDeclaresRegistryKeys(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		src  string
		want bool
	}{
		{
			name: "an anchor in a schema position",
			src: `components:
  schemas:
    A: {$anchor: a, type: string}
`,
			want: true,
		},
		{
			name: "an id under an extension",
			src: `paths: {}
x-foo: {$id: "https://x.test/a"}
`,
			want: true,
		},
		{
			name: "an anchor inside an example value",
			src: `components:
  schemas:
    A:
      type: object
      example: {$anchor: z, type: string}
`,
			want: true,
		},
		{
			name: "an anchor on a node also reached by a YAML alias",
			src: `components:
  schemas:
    A: &shared {$anchor: z, type: string}
    B: *shared
`,
			want: true,
		},
		{
			name: "neither key anywhere in the tree",
			src: `openapi: 3.1.0
info: {title: t, version: "1"}
paths: {}
components:
  schemas:
    A: {type: object, properties: {p: {type: string}}}
`,
			want: false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var root yaml.Node
			require.NoError(t, yaml.Unmarshal([]byte(tc.src), &root))
			assert.Equal(t, tc.want, declaresRegistryKeys(&root))
		})
	}
}

// TestSpellsDefsPointer pins spellsDefsPointer (reach.go), the $defs half of
// chainCycle's gate: it must fire only for a pointer literally spelled
// "#/$defs/...", not for a full path from root that merely passes through a
// key named "$defs" partway through — that shape is
// defsPointerCyclesThroughSibling's, gated to the pre-parse scan instead (see
// TestChainCycle_GateDefersPurePointerCyclesToThePreParseScan).
func TestSpellsDefsPointer(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		src  string
		want bool
	}{
		{
			name: "a literal #/$defs/ pointer",
			src: `components:
  schemas:
    A: {$ref: "#/$defs/n"}
`,
			want: true,
		},
		{
			name: "a full path from root that merely passes through a $defs segment",
			src: `components:
  schemas:
    A:
      $defs:
        n: {$ref: "#/components/schemas/B"}
    B: {$ref: "#/components/schemas/A/$defs/n"}
`,
			want: false,
		},
		{
			name: "no $defs pointer anywhere",
			src: `components:
  schemas:
    A: {type: string}
`,
			want: false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var root yaml.Node
			require.NoError(t, yaml.Unmarshal([]byte(tc.src), &root))
			assert.Equal(t, tc.want, spellsDefsPointer(&root))
		})
	}
}

// TestBuild_ChainCheckWarningIsCarriedNotRefused drives the branch build takes
// when the reference-chain check reports its own protection incomplete rather
// than finding a cycle: the document still lowers, and the warning rides along
// as a diagnostic. Options.chainCheck substitutes the fault directly because
// chainCycle only ever answers this way when recoverChains catches a panic
// reading the library's model, which no document that already survived
// unmarshal can still provoke.
func TestBuild_ChainCheckWarningIsCarriedNotRefused(t *testing.T) {
	t.Parallel()
	want := diag.Newf(ir.SeverityWarning, diag.CycleScanFailed, ir.Provenance{Source: 0}, "forced for a test")
	opts := Options{chainCheck: func(context.Context, scan.Locator, *yaml.Node, *soa.OpenAPI) (ir.Diagnostic, bool) {
		return want, true
	}}
	doc, diags, err := Load(t.Context(), 0, compilers.Source{Path: "spec.yaml", Data: []byte(minimal31)}, opts)
	require.NoError(t, err)
	require.NotNil(t, doc, "a warning carries forward; it does not refuse the document")
	require.Contains(t, diags, want, "the forced warning rides along in the diagnostic list")
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
// fixpoint (reach.go): A's $ref is a /$defs/ pointer landing on n directly —
// no fixpoint needed for that hop — but n's own $ref is a PLAIN pointer to B,
// not itself spelled "#/$defs/...". Only re-scanning n's own reference after
// marking it drifted discovers that this plain pointer must also be read as
// resolvable from any node, which is what lets it reach B. Mutating drift
// away (see the M2 mutation in the PR this test guards) leaves n marked as a
// reference but never revisits it, so B is never marked drifted at all.
func TestDrift_MarksTheDefsTargetAndPropagatesThroughItsOwnPointer(t *testing.T) {
	t.Parallel()
	const spec = `openapi: 3.1.0
info: {title: t, version: "1"}
paths: {}
components:
  schemas:
    A:
      $ref: "#/$defs/n"
      $defs:
        n: {$ref: "#/components/schemas/B"}
    B: {type: string}
`
	doc, root := buildDoc(t, spec)
	r, _ := newReach(t.Context(), root, doc)

	require.Len(t, r.byKey["B"], 1, "B is a unique top-level component")
	require.Len(t, r.byKey["A"], 1, "A is a unique top-level component")
	bNode, aNode := r.byKey["B"][0], r.byKey["A"][0]

	assert.True(t, r.drifted[bNode],
		"B is reached only through n's own pointer, discovered by revisiting n once it drifted")
	assert.False(t, r.drifted[aNode],
		"the referring node itself is never marked drifted, only what a /$defs/ target's subtree reaches")
}

// TestDrift_BareFragmentSelfReferenceNeedsTheNodeItselfMarkedFirst drives the
// specific shape where the fixpoint is load-bearing rather than merely
// observable: n's own $ref is a bare "#", and documentTargets — unlike
// pointerTargets — returns no target at all for an undrifted node (there is
// no root fallback for an empty fragment). The cycle through n is visible only
// once markDrifted's own return value re-queues n for a second look at its
// own reference.
func TestDrift_BareFragmentSelfReferenceNeedsTheNodeItselfMarkedFirst(t *testing.T) {
	t.Parallel()
	const spec = `openapi: 3.1.0
info: {title: t, version: "1"}
paths: {}
components:
  schemas:
    A:
      $ref: "#/$defs/n"
      $defs:
        n: {$ref: "#"}
`
	assertRefusedByReach(t, spec)
}

// TestContentRoot_NonDocumentNodeIsReturnedAsIs drives contentRoot's fallback
// arm: a node that is not a yaml.DocumentNode (every real source's decoded
// root is one, wrapping exactly one document) is returned unchanged rather
// than indexed into.
func TestContentRoot_NonDocumentNodeIsReturnedAsIs(t *testing.T) {
	t.Parallel()
	n := &yaml.Node{Kind: yaml.MappingNode}
	assert.Same(t, n, contentRoot(n))
	assert.Nil(t, contentRoot(nil))
}

// TestChild_SequenceIndexesByPosition drives child's sequence arm: a token
// spelling a valid index returns that element, and one past the end (or
// non-numeric) finds nothing.
func TestChild_SequenceIndexesByPosition(t *testing.T) {
	t.Parallel()
	first := &yaml.Node{Kind: yaml.ScalarNode, Value: "first"}
	second := &yaml.Node{Kind: yaml.ScalarNode, Value: "second"}
	seq := &yaml.Node{Kind: yaml.SequenceNode, Content: []*yaml.Node{first, second}}

	assert.Same(t, first, child(seq, "0"))
	assert.Same(t, second, child(seq, "1"))
	assert.Nil(t, child(seq, "2"), "past the end of the sequence")
	assert.Nil(t, child(seq, "not-a-number"))
}

// TestLastSegment_StripsFragmentAndQueryBeforeTheFinalSlash drives lastSegment
// with both a "#" fragment and a "?" query present, in either order, pinning
// that both are cut before the last path segment is taken.
func TestLastSegment_StripsFragmentAndQueryBeforeTheFinalSlash(t *testing.T) {
	t.Parallel()
	tests := []struct{ name, uri, want string }{
		{"query only", "https://x.test/dir/a?x=1", "a"},
		{"fragment only", "https://x.test/dir/a#frag", "a"},
		{"query then fragment", "https://x.test/dir/a?x=1#frag", "a"},
		{"no slash at all", "a.json", "a.json"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, lastSegment(tc.uri))
		})
	}
}

// TestDocumentTargets_UndriftedNodeHasNoDocumentToLandOn drives documentTargets'
// first arm: a bare "#"/"#/" reference has no root fallback the way a pointer
// does, so an undrifted node yields no target at all.
func TestDocumentTargets_UndriftedNodeHasNoDocumentToLandOn(t *testing.T) {
	t.Parallel()
	r := &reach{drifted: map[*yaml.Node]bool{}}
	assert.Nil(t, r.documentTargets(&yaml.Node{}))
}

// TestDocumentTargets_DriftedNodeCanLandOnAnyReferenceOrSearchedAncestor
// drives documentTargets' second arm directly against a hand-built reach,
// isolating its filter (refValue(n) != "" || r.searched[n]) from index and
// drift construction.
func TestDocumentTargets_DriftedNodeCanLandOnAnyReferenceOrSearchedAncestor(t *testing.T) {
	t.Parallel()
	refNode := &yaml.Node{Kind: yaml.MappingNode, Content: []*yaml.Node{
		{Kind: yaml.ScalarNode, Value: "$ref"},
		{Kind: yaml.ScalarNode, Value: "#/x"},
	}}
	searchedNode := &yaml.Node{Kind: yaml.MappingNode} // no $ref, but a searched ancestor
	plainNode := &yaml.Node{Kind: yaml.MappingNode}    // neither: must be excluded
	n := &yaml.Node{}                                  // the drifted node documentTargets is asked about

	r := &reach{
		drifted:  map[*yaml.Node]bool{n: true},
		scope:    map[*yaml.Node]*yaml.Node{refNode: nil, searchedNode: nil, plainNode: nil},
		searched: map[*yaml.Node]bool{searchedNode: true},
	}
	assert.ElementsMatch(t, []*yaml.Node{refNode, searchedNode}, r.documentTargets(n))
}

// TestChild_NilNodeIsNoTarget drives child's other early return: deref(nil)
// is nil, so child(nil, ...) must report no target rather than panic on a nil
// Kind switch.
func TestChild_NilNodeIsNoTarget(t *testing.T) {
	t.Parallel()
	assert.Nil(t, child(nil, "x"))
}

// TestDecodeFragment_InvalidPercentEncodingIsReturnedRaw drives decodeFragment's
// fallback: a fragment with a truncated "%" escape fails url.QueryUnescape, so
// the original string is returned rather than an error being swallowed into an
// empty one.
func TestDecodeFragment_InvalidPercentEncodingIsReturnedRaw(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "%zz", decodeFragment("%zz"))
}

// TestTargets_NeitherFragmentNorURIIsNoTarget drives targets' own early
// return: a $ref value with no "#" and nothing but whitespace before where one
// would be. Called directly against a hand-built node and a zero-value reach,
// since this arm returns before touching any of reach's maps — the same
// pattern documentTargets' and pointerTargets' own hand-built-node tests use.
func TestTargets_NeitherFragmentNorURIIsNoTarget(t *testing.T) {
	t.Parallel()
	n := &yaml.Node{Kind: yaml.MappingNode, Content: []*yaml.Node{
		{Kind: yaml.ScalarNode, Value: "$ref"},
		{Kind: yaml.ScalarNode, Value: "   "},
	}}
	r := &reach{}
	assert.Nil(t, r.targets(n))
}

// TestPointerTargets_EmptyPointerHasNoTokens drives pointerTargets' own guard:
// an empty pointer decodes to no tokens at all (RFC 6901's whole-document
// pointer), so there is nothing to walk from any start. targets() itself never
// reaches pointerTargets with an empty pointer — that shape takes the bare-
// fragment branch to documentTargets instead — so this is exercised directly.
func TestPointerTargets_EmptyPointerHasNoTokens(t *testing.T) {
	t.Parallel()
	r := &reach{}
	assert.Nil(t, r.pointerTargets(&yaml.Node{}, ""))
}

// aliasedSchemaAndItsAlias declares a real YAML alias (as opposed to every
// other fixture's $anchor/$id, which are JSON Schema keywords the library
// registers — an unrelated mechanism): B's value is a YAML alias to A's node.
// It carries $anchor so the gate runs reach at all, exercising the tree
// walk's own alias-dereferencing (index's traversal in reach.go and child's
// in chains shared with it) alongside declaresRegistryKeys' own alias test in
// TestDeclaresRegistryKeys. Neither schema carries a $ref, so nothing cycles.
const aliasedSchemaAndItsAlias = `openapi: 3.1.0
info: {title: t, version: "1"}
paths: {}
components:
  schemas:
    A: &shared {$anchor: a, type: string}
    B: *shared
`
