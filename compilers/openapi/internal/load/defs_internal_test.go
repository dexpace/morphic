package load

import (
	"encoding/json/jsontext"
	"testing"

	oas3 "github.com/speakeasy-api/openapi/jsonschema/oas3"
	"github.com/speakeasy-api/openapi/references"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/scan"
)

const twoDefsSpec = `openapi: 3.1.0
info: {title: t, version: "1"}
paths: {}
components:
  schemas:
    A:
      type: object
      properties:
        p: {$ref: "#/$defs/n"}
      $defs:
        n: {type: object, properties: {x: {type: string}}}
    B:
      type: object
      properties:
        q: {$ref: "#/$defs/n"}
      $defs:
        n: {type: object, properties: {y: {type: integer}}}
`

// TestDefsRefs_CollectsEveryHeldReferenceWithTheRulesAnswer drives defsRefs'
// walk body: two "#/$defs/..." references, each spelling the same key name but
// each answered from its own schema, exactly the shape GitHub #557 reports.
func TestDefsRefs_CollectsEveryHeldReferenceWithTheRulesAnswer(t *testing.T) {
	t.Parallel()
	doc, root := buildDoc(t, twoDefsSpec)
	refs := defsRefs(t.Context(), root, doc)
	require.Len(t, refs, 2, "one held reference per schema")
	for _, r := range refs {
		assert.NotNil(t, r.target, "the rule finds each schema's own definition")
		assert.Equal(t, "#/$defs/n", r.written.String())
	}
}

// TestDefsRefs_NoDefsPointerAnywhereCollectsNothing pins the guard that keeps a
// document without a single "#/$defs/..." reference from paying for the walk.
func TestDefsRefs_NoDefsPointerAnywhereCollectsNothing(t *testing.T) {
	t.Parallel()
	doc, root := buildDoc(t, minimal31)
	assert.Nil(t, defsRefs(t.Context(), root, doc))
}

// TestWithDefsHeld_HoldsDuringFAndRestoresAfter pins the hold-and-restore
// contract on its own, isolated from resolveDefs: while f runs, the held
// reference is gone; once withDefsHeld returns, it reads exactly as written.
func TestWithDefsHeld_HoldsDuringFAndRestoresAfter(t *testing.T) {
	t.Parallel()
	doc, root := buildDoc(t, twoDefsSpec)
	refs := defsRefs(t.Context(), root, doc)
	require.Len(t, refs, 2)

	var duringHold []*references.Reference
	withDefsHeld(refs, func() {
		for _, r := range refs {
			duringHold = append(duringHold, r.js.GetSchema().Ref)
		}
	})

	for _, ref := range duringHold {
		assert.Nil(t, ref, "held out of the resolver's reach while f runs")
	}
	for _, r := range refs {
		require.NotNil(t, r.js.GetSchema().Ref, "restored once withDefsHeld returns")
		assert.Equal(t, "#/$defs/n", r.js.GetSchema().Ref.String())
	}
}

// TestWithDefsHeld_RestoresEvenWhenFPanics pins the property withDefsHeld
// exists for: f in production wraps the general resolver pass
// (resolveAll), and a panic from resolving some other, unrelated reference
// must not leave a held "#/$defs/..." reference permanently stripped. The
// panic itself is not swallowed — restoration happens before it keeps
// propagating, which is why the test recovers it itself.
func TestWithDefsHeld_RestoresEvenWhenFPanics(t *testing.T) {
	t.Parallel()
	doc, root := buildDoc(t, twoDefsSpec)
	refs := defsRefs(t.Context(), root, doc)
	require.Len(t, refs, 2)

	func() {
		defer func() {
			r := recover()
			require.Equal(t, "boom", r, "withDefsHeld must not swallow the panic")
		}()
		withDefsHeld(refs, func() { panic("boom") })
		t.Fatal("unreachable: withDefsHeld must let the panic propagate")
	}()

	for _, r := range refs {
		require.NotNil(t, r.js.GetSchema().Ref, "restored despite the panic")
		assert.Equal(t, "#/$defs/n", r.js.GetSchema().Ref.String())
	}
}

// TestResolveDefs_ResolvesToTheRulesTarget drives the success path end to end:
// after resolveDefs runs, each reference's own resolution info names exactly
// the node the rule found, at exactly the position the rule found it — not the
// document-rooted pointer the reference spelled, which the document does not
// have — and the reference reads restored as written afterward.
func TestResolveDefs_ResolvesToTheRulesTarget(t *testing.T) {
	t.Parallel()
	doc, root := buildDoc(t, twoDefsSpec)
	refs := defsRefs(t.Context(), root, doc)
	require.Len(t, refs, 2)

	diags := resolveDefs(t.Context(), scan.InSource(0), doc, "spec.yaml", Options{}, refs)
	assert.Empty(t, diags)

	for _, r := range refs {
		require.NotNil(t, r.target)
		info := r.js.GetReferenceResolutionInfo()
		require.NotNil(t, info, "resolved during resolveDefs")
		assert.Same(t, r.target, info.Object, "resolves to the rule's own target, not any other node")
		assert.Equal(t, r.at, jsontext.Pointer(info.AbsoluteReference.GetJSONPointer()),
			"the raw fragment of the absolute reference is exactly the rule's own position")
		require.NotNil(t, r.js.GetSchema().Ref, "restored as written")
		assert.Equal(t, "#/$defs/n", r.js.GetSchema().Ref.String())
	}
}

// TestResolveDefs_NoTargetLeavesTheReferenceUnresolvedWithoutADiagnostic pins
// the branch for a reference the rule has no answer for: it never reaches the
// resolver at all, so resolving the rest of the document cannot read it as
// pointing anywhere by accident, and resolveDefs itself reports nothing — an
// unresolved "#/$defs/..." reference is diagnosed the way any other unresolved
// reference is, not doubly by this function.
func TestResolveDefs_NoTargetLeavesTheReferenceUnresolvedWithoutADiagnostic(t *testing.T) {
	t.Parallel()
	const spec = `openapi: 3.1.0
info: {title: t, version: "1"}
paths: {}
components:
  schemas:
    A:
      type: object
      properties:
        p: {$ref: "#/$defs/n"}
`
	doc, root := buildDoc(t, spec)
	refs := defsRefs(t.Context(), root, doc)
	require.Len(t, refs, 1)
	require.Nil(t, refs[0].target, "no $defs exists anywhere for the rule to find")

	diags := resolveDefs(t.Context(), scan.InSource(0), doc, "spec.yaml", Options{}, refs)
	assert.Empty(t, diags)
	require.NotNil(t, refs[0].js.GetSchema().Ref, "restored as written")
	assert.Equal(t, "#/$defs/n", refs[0].js.GetSchema().Ref.String())
}

// TestResolveDefs_ConcretePointerMismatchIsReported drives the defensive path
// for a rule answer the resolver itself cannot stand behind: at is deliberately
// overwritten to a position nothing sits at, so resolving the concrete pointer
// the rule handed the resolver fails on its own terms rather than through
// defs.Target disagreeing with the resolver's own navigation.
func TestResolveDefs_ConcretePointerMismatchIsReported(t *testing.T) {
	t.Parallel()
	doc, root := buildDoc(t, twoDefsSpec)
	refs := defsRefs(t.Context(), root, doc)
	require.Len(t, refs, 2)
	refs[0].at = "/does/not/exist"

	diags := resolveDefs(t.Context(), scan.InSource(0), doc, "spec.yaml", Options{}, refs)
	require.Len(t, diags, 1)
	assert.Equal(t, diag.UnresolvedRef, diags[0].Code)
}

// TestResolveDefs_RecoversFromAPanicAndReportsIt drives resolveDefs' own
// recover directly: a defsRef whose schema is nil is not a shape defsRefs ever
// produces (heldDefsPointer only ever runs against a *schemaRef soa.Walk
// itself handed the model), but resolveDefs must not let a panic mid-batch —
// from this or from the vendored resolver's own internals — escape as a Go
// panic and abort the whole load; it is reported as a diagnostic instead, the
// way every other stage reports a spec problem.
func TestResolveDefs_RecoversFromAPanicAndReportsIt(t *testing.T) {
	t.Parallel()
	refs := []defsRef{{js: nil, target: &oas3.JSONSchema[oas3.Referenceable]{}, at: "/$defs/n"}}

	diags := resolveDefs(t.Context(), scan.InSource(0), nil, "spec.yaml", Options{}, refs)
	require.Len(t, diags, 1)
	assert.Equal(t, diag.UnresolvedRef, diags[0].Code)
	assert.Contains(t, diags[0].Message, "reference resolver panicked")
}

// TestFragmentOf_RoundTrips pins that every byte fragmentOf percent-encodes
// decodes back to exactly the pointer it started from once the resolver reads
// it as a URI fragment (references.Reference.GetJSONPointer trims and
// query-unescapes) — the property resolveDefs relies on to hand the resolver a
// pointer it will read back unchanged.
func TestFragmentOf_RoundTrips(t *testing.T) {
	t.Parallel()
	tests := []jsontext.Pointer{
		"/$defs/a+b",
		"/$defs/50%",
		"/$defs/a#b",
		"/$defs/a b",
		"/$defs/héllo",
	}
	for _, p := range tests {
		t.Run(string(p), func(t *testing.T) {
			t.Parallel()
			ref := references.Reference("#" + fragmentOf(p))
			got := jsontext.Pointer(ref.GetJSONPointer())
			assert.Equal(t, p, got)
		})
	}
}

// TestMapsDefsPointer_DecodesAPercentEncodedDollarSign pins that mapsDefsPointer
// decodes a $ref value the way the resolver decodes one, not by a raw substring
// match: "%24" is "$" percent-encoded, so "#/%24defs/n" is exactly the same
// pointer as "#/$defs/n".
func TestMapsDefsPointer_DecodesAPercentEncodedDollarSign(t *testing.T) {
	t.Parallel()
	n := &yaml.Node{Kind: yaml.MappingNode, Content: []*yaml.Node{
		{Kind: yaml.ScalarNode, Value: "$ref"},
		{Kind: yaml.ScalarNode, Value: "#/%24defs/n"},
	}}
	assert.True(t, mapsDefsPointer(n))
}

// TestMapsDefsPointer_ExternalDocumentIsNotHeld pins the other half of the
// same guard: a $defs-shaped fragment on a reference into another document is
// not one load holds, since resolveDefs only ever resolves a same-document
// pointer.
func TestMapsDefsPointer_ExternalDocumentIsNotHeld(t *testing.T) {
	t.Parallel()
	n := &yaml.Node{Kind: yaml.MappingNode, Content: []*yaml.Node{
		{Kind: yaml.ScalarNode, Value: "$ref"},
		{Kind: yaml.ScalarNode, Value: "other.yaml#/$defs/n"},
	}}
	assert.False(t, mapsDefsPointer(n))
}
