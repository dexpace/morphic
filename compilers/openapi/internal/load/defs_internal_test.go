package load

import (
	"context"
	"encoding/json/jsontext"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"

	oas3 "github.com/speakeasy-api/openapi/jsonschema/oas3"
	"github.com/speakeasy-api/openapi/marshaller"
	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/speakeasy-api/openapi/references"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi/internal/defs"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/compilers/openapi/internal/overlay"
	"github.com/dexpace/morphic/ir"
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

// TestHeldRefs_CollectsEveryHeldReferenceWithTheRulesAnswer drives heldRefs'
// walk body: two "#/$defs/..." references, each spelling the same key name but
// each answered from its own schema, exactly the shape GitHub #557 reports.
func TestHeldRefs_CollectsEveryHeldReferenceWithTheRulesAnswer(t *testing.T) {
	t.Parallel()
	doc, _ := buildDoc(t, twoDefsSpec)
	refs := heldRefs(t.Context(), doc, defs.NewReader(doc))
	require.Len(t, refs, 2, "one held reference per schema")
	for _, r := range refs {
		assert.NotNil(t, r.target, "the rule finds each schema's own definition")
		assert.Equal(t, "#/$defs/n", r.written.String())
	}
}

// TestHeldRefs_NoDefsPointerAnywhereCollectsNothing pins that a document without
// a single "#/$defs/..." reference collects nothing.
func TestHeldRefs_NoDefsPointerAnywhereCollectsNothing(t *testing.T) {
	t.Parallel()
	doc, _ := buildDoc(t, minimal31)
	assert.Nil(t, heldRefs(t.Context(), doc, defs.NewReader(doc)))
}

// deepDefsDoc is a component nested depth levels deep, with the "#/$defs/m"
// definition at the top and a reference to it at every level.
func deepDefsDoc(depth int) string {
	var sb strings.Builder
	sb.WriteString("openapi: 3.1.0\ninfo: {title: t, version: \"1\"}\npaths: {}\ncomponents:\n  schemas:\n" +
		"    A:\n      $defs: {m: {type: string}}\n      properties:\n")
	indent := "        "
	for i := range depth {
		fmt.Fprintf(&sb, "%sp%d:\n%s  allOf: [{$ref: \"#/$defs/m\"}]\n%s  properties:\n", indent, i, indent, indent)
		indent += "    "
	}
	return sb.String() + indent + "leaf: {type: string}\n"
}

// TestHeldRefs_ShareOneReader pins that the references of a document are read
// through the one reader they are given, whose memory is what keeps the work
// proportional to the document: a reference at every level of a schema nested
// d deep costs a constant number of navigations per level. A reader per
// reference would walk the whole chain above each, and the work would grow with
// the square of the depth.
func TestHeldRefs_ShareOneReader(t *testing.T) {
	t.Parallel()
	const depth = 60
	doc, _ := buildDoc(t, deepDefsDoc(depth))
	rule := defs.NewReader(doc)

	refs := heldRefs(t.Context(), doc, rule)

	require.Len(t, refs, depth)
	for _, r := range refs {
		require.NotNil(t, r.target, r.site)
	}
	assert.GreaterOrEqual(t, rule.Reads(), depth, "every level was read, through the reader it was given")
	assert.LessOrEqual(t, rule.Reads(), 9*depth, "a constant number of navigations per level")
}

// TestHeldRefs_ReadsTheTreeToDecideWhetherToWalk pins the gate's input: it asks
// the tree, not the model, so a tree spelling no "#/$defs/..." pointer is not
// walked even though the model it was parsed into still holds the references.
// The tree is edited after the parse to tell the two apart.
func TestHeldRefs_ReadsTheTreeToDecideWhetherToWalk(t *testing.T) {
	t.Parallel()
	doc, _ := buildDoc(t, twoDefsSpec)
	require.Len(t, heldRefs(t.Context(), doc, defs.NewReader(doc)), 2, "the model holds two such references")

	stack := []*yaml.Node{doc.GetRootNode()}
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if n.Kind == yaml.ScalarNode && isDefsRef(n.Value) {
			n.Value = "#/components/schemas/A"
		}
		stack = append(stack, n.Content...)
	}
	assert.Nil(t, heldRefs(t.Context(), doc, defs.NewReader(doc)), "a tree spelling none is not walked")
}

// TestWithDefsHeld_HoldsDuringFAndRestoresAfter pins the hold-and-restore
// contract on its own, isolated from resolveHeld: while f runs, the held
// reference is gone; once withDefsHeld returns, it reads exactly as written.
func TestWithDefsHeld_HoldsDuringFAndRestoresAfter(t *testing.T) {
	t.Parallel()
	doc, _ := buildDoc(t, twoDefsSpec)
	refs := heldRefs(t.Context(), doc, defs.NewReader(doc))
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
// exists for: f in production wraps the general resolver pass, and a panic from
// resolving some other, unrelated reference must not leave a held
// "#/$defs/..." reference permanently stripped. The panic itself is not
// swallowed — restoration happens before it keeps propagating, which is why the
// test recovers it itself.
func TestWithDefsHeld_RestoresEvenWhenFPanics(t *testing.T) {
	t.Parallel()
	doc, _ := buildDoc(t, twoDefsSpec)
	refs := heldRefs(t.Context(), doc, defs.NewReader(doc))
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

// heldPass is a resolution of doc as the load stage runs one, without an
// external reader.
func heldPass(t *testing.T, doc *soa.OpenAPI) *resolution {
	t.Helper()
	return newResolution(t.Context(), pointerAt(0, overlay.Origin{}), doc, "spec.yaml", Options{}, nil)
}

// TestResolveHeld_ResolvesToTheRulesTarget drives the success path end to end:
// after resolveHeld runs, each reference's own resolution info names exactly the
// node the rule found, at exactly the position the rule found it — not the
// document-rooted pointer the reference spelled, which the document does not
// have — and the reference reads restored as written afterward.
func TestResolveHeld_ResolvesToTheRulesTarget(t *testing.T) {
	t.Parallel()
	doc, _ := buildDoc(t, twoDefsSpec)
	refs := heldRefs(t.Context(), doc, defs.NewReader(doc))
	require.Len(t, refs, 2)

	pass := heldPass(t, doc)
	site, err := pass.resolveHeld(refs)
	require.NoError(t, err)
	assert.Empty(t, site)
	assert.Empty(t, pass.failures)

	for _, r := range refs {
		require.NotNil(t, r.target)
		info := r.js.GetReferenceResolutionInfo()
		require.NotNil(t, info, "resolved during resolveHeld")
		assert.Same(t, r.target, info.Object, "resolves to the rule's own target, not any other node")
		assert.Equal(t, r.at, jsontext.Pointer(info.AbsoluteReference.GetJSONPointer()),
			"the raw fragment of the absolute reference is exactly the rule's own position")
		require.NotNil(t, r.js.GetSchema().Ref, "restored as written")
		assert.Equal(t, "#/$defs/n", r.js.GetSchema().Ref.String())
	}
}

// TestResolveHeld_NoTargetIsReportedAsAMissingDefinition pins the branch for a
// reference the rule has no answer for: it never reaches the resolver at all,
// so resolving the rest of the document cannot read it as pointing anywhere by
// accident, and resolveHeld reports it, at the reference, as the resolver
// reports a definition it cannot find. The lowering reports only the positions
// it models, so a reference under "not", say, would otherwise go unreported.
func TestResolveHeld_NoTargetIsReportedAsAMissingDefinition(t *testing.T) {
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
	doc, _ := buildDoc(t, spec)
	refs := heldRefs(t.Context(), doc, defs.NewReader(doc))
	require.Len(t, refs, 1)
	require.Nil(t, refs[0].target, "no $defs exists anywhere for the rule to find")

	pass := heldPass(t, doc)
	_, err := pass.resolveHeld(refs)
	require.NoError(t, err)
	require.Len(t, pass.failures, 1)
	got := pass.failures[0]
	assert.Equal(t, diag.UnresolvedRef, got.Code)
	assert.Equal(t, ir.SeverityError, got.Severity)
	assert.Equal(t, refs[0].site, got.Provenance.Pointer, "placed at the reference")
	assert.Equal(t, `unresolved $ref "#/$defs/n": definition not found: #/$defs/n`, got.Message)
	assert.False(t, refs[0].js.IsResolved(), "the resolver was never handed it")
	require.NotNil(t, refs[0].js.GetSchema().Ref, "restored as written")
	assert.Equal(t, "#/$defs/n", refs[0].js.GetSchema().Ref.String())
}

// TestLoad_AHeldReferenceWithNoDefinitionIsReportedWhereverItSits pins that a
// "#/$defs/..." reference no definition answers for is an error at the
// reference whether or not the lowering models its position: "not" is kept
// verbatim, so only load can report the reference there. At a modeled position
// the lowering would report it too, and the compiler keeps the one at load.
func TestLoad_AHeldReferenceWithNoDefinitionIsReportedWhereverItSits(t *testing.T) {
	t.Parallel()
	const spec = `openapi: 3.1.0
info: {title: t, version: "1"}
paths: {}
components:
  schemas:
    A:
      type: object
      not: {$ref: "#/$defs/missing"}
      properties:
        p: {$ref: "#/$defs/missing"}
`
	_, diags, err := Load(t.Context(), 0, compilers.Source{Path: "root.yaml", Data: []byte(spec)}, Options{})
	require.NoError(t, err)

	const want = `unresolved $ref "#/$defs/missing": definition not found: #/$defs/missing`
	for _, site := range []string{"/components/schemas/A/not", "/components/schemas/A/properties/p"} {
		assert.Equal(t, want, openapitest.DiagMessageAt(t, diags, diag.UnresolvedRef, ir.SeverityError, site), site)
	}
}

// TestResolveHeld_ConcretePointerMismatchIsReportedAsWritten drives the
// defensive path for a rule answer the resolver itself cannot stand behind: at
// is deliberately overwritten to a position nothing sits at, so resolving the
// concrete pointer the rule handed the resolver fails on its own terms rather
// than through defs.Reader.Target disagreeing with the resolver's own navigation. The
// failure is placed at the reference and quotes it as written, not the pointer
// it was handed.
func TestResolveHeld_ConcretePointerMismatchIsReportedAsWritten(t *testing.T) {
	t.Parallel()
	doc, _ := buildDoc(t, twoDefsSpec)
	refs := heldRefs(t.Context(), doc, defs.NewReader(doc))
	require.Len(t, refs, 2)
	refs[0].at = "/does/not/exist"

	pass := heldPass(t, doc)
	_, err := pass.resolveHeld(refs)
	require.NoError(t, err)
	require.Len(t, pass.failures, 1)
	got := pass.failures[0]
	assert.Equal(t, diag.UnresolvedRef, got.Code)
	assert.Equal(t, refs[0].site, got.Provenance.Pointer, "placed at the reference that failed")
	assert.Contains(t, got.Message, `unresolved $ref "#/$defs/n"`, "quoted as written")
	assert.NotContains(t, got.Message, "/does/not/exist")
}

// TestResolveHeld_RecoversFromAPanicAndReportsWhere drives resolveHeld's own
// recover directly. A heldRef whose schema is nil is not a shape heldRefs ever
// produces, but resolveHeld must not let a panic mid-batch — from this or from
// the vendored resolver's own internals — escape as a Go panic and abort the
// whole load: it comes back as an error naming the reference it stopped at. A
// panic before any reference is reached names none.
func TestResolveHeld_RecoversFromAPanicAndReportsWhere(t *testing.T) {
	t.Parallel()
	doc, _ := buildDoc(t, twoDefsSpec)
	pass := heldPass(t, doc)

	site, err := pass.resolveHeld([]heldRef{{js: nil, target: &oas3.JSONSchema[oas3.Referenceable]{}, at: "/$defs/n"}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "reference resolver panicked")
	assert.Empty(t, site, "stopped before reaching any reference")

	held := heldRefs(t.Context(), doc, defs.NewReader(doc))
	require.Len(t, held, 2)
	held[1].written = nil // resolving the second then dereferences nothing
	site, err = pass.resolveHeld(held)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "reference resolver panicked")
	assert.Equal(t, held[1].site, site, "stopped at the reference being resolved")
}

// TestFragmentOf_RoundTrips pins that every byte fragmentOf percent-encodes
// decodes back to exactly the pointer it started from once the resolver reads
// it as a URI fragment (references.Reference.GetJSONPointer trims and
// query-unescapes) — the property resolveHeld relies on to hand the resolver a
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

// TestFragmentOf_RoundTripsEveryByte pins the same property for every byte
// value at the start, the middle and the end of a key. The resolver trims the
// fragment before it decodes it, and a '#' ends it, so those are the places a
// byte left unencoded would be lost.
func TestFragmentOf_RoundTripsEveryByte(t *testing.T) {
	t.Parallel()
	for b := range 256 {
		for _, key := range []string{string([]byte{byte(b), 'a'}), string([]byte{'a', byte(b), 'b'}), string([]byte{'a', byte(b)})} {
			p := jsontext.Pointer("/$defs/" + key)
			ref := references.Reference("#" + fragmentOf(p))
			require.Equal(t, p, jsontext.Pointer(ref.GetJSONPointer()), "byte 0x%02x in %q", b, key)
			require.Empty(t, ref.GetURI(), "byte 0x%02x in %q", b, key)
		}
	}
}

// TestHeldDefsPointer pins which references load holds out of the resolver's
// pass: a schema reference with no document part whose fragment the resolver
// reads as a "#/$defs/..." pointer, decoded as the resolver decodes it, so
// "%24" is "$". A $defs-shaped fragment on a reference into another document
// is not one, since resolveHeld only ever resolves a same-document pointer.
func TestHeldDefsPointer(t *testing.T) {
	t.Parallel()
	tests := []struct {
		body string
		want jsontext.Pointer
	}{
		{`{$ref: "#/$defs/n"}`, "/$defs/n"},
		{`{$ref: "#/%24defs/n"}`, "/$defs/n"},
		{`{$ref: "other.yaml#/$defs/n"}`, ""},
		{`{$ref: "#/components/schemas/A"}`, ""},
		{`{type: string}`, ""},
	}
	for _, tc := range tests {
		t.Run(tc.body, func(t *testing.T) {
			t.Parallel()
			js := &oas3.JSONSchema[oas3.Referenceable]{}
			valErrs, err := marshaller.Unmarshal(t.Context(), strings.NewReader(tc.body), js)
			require.NoError(t, err)
			require.Empty(t, valErrs)
			pointer, held := heldDefsPointer(js)
			assert.Equal(t, tc.want != "", held)
			assert.Equal(t, tc.want, pointer)
		})
	}
	_, held := heldDefsPointer(nil)
	assert.False(t, held, "no schema holds nothing")
}

// TestLoad_TheRebuiltDocumentHoldsDefsReferencesOutToo pins that the second
// pass resolveExternal makes over a rebuilt document resolves its "#/$defs/..."
// references by the rule as the first did. The rebuilt model is new objects, so
// a pass that held nothing out would hand them to the resolver's own lookup,
// which caches the first definition it finds for a pointer: B.q would be typed
// with A's definition (GitHub #557).
func TestLoad_TheRebuiltDocumentHoldsDefsReferencesOutToo(t *testing.T) {
	t.Parallel()
	srv, _ := countingServer(t, anchoredExternalDoc)
	spec := rootReferencing(respelled(srv.URL, "HTTP")+"/ext.yaml") + `components:
  schemas:
    A:
      type: object
      properties: {p: {$ref: "#/$defs/n"}}
      $defs: {n: {type: object, properties: {x: {type: string}}}}
    B:
      type: object
      properties: {q: {$ref: "#/$defs/n"}}
      $defs: {n: {type: object, properties: {y: {type: integer}}}}
`
	var rebuilds atomic.Int32
	opts := Options{AllowExternalRefs: true, rebuildDoc: func(ctx context.Context, data []byte, root *yaml.Node) (*soa.OpenAPI, []error, error) {
		rebuilds.Add(1)
		return unmarshal(ctx, data, root)
	}}

	got, diags, err := Load(t.Context(), 0, compilers.Source{Path: "root.yaml", Data: []byte(spec)}, opts)

	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Empty(t, diags)
	require.Equal(t, int32(1), rebuilds.Load(), "the respelled anchored document forces the second pass")
	for owner, want := range map[string][2]string{"A": {"p", "x"}, "B": {"q", "y"}} {
		schema, ok := got.Doc.Components.Schemas.Get(owner)
		require.True(t, ok, owner)
		prop, ok := schema.GetSchema().GetProperties().Get(want[0])
		require.True(t, ok, owner)
		info := prop.GetReferenceResolutionInfo()
		require.NotNil(t, info, "%s.%s resolved", owner, want[0])
		_, ownDefinition := info.Object.GetSchema().GetProperties().Get(want[1])
		assert.True(t, ownDefinition, "%s.%s is typed with %s's own definition", owner, want[0], owner)
	}
}

// defsToExternalSpec holds a "#/$defs/..." definition that is itself a $ref into
// another document, so resolving the held reference reads that document.
func defsToExternalSpec(url string) string {
	return `openapi: 3.1.0
info: {title: T, version: "1"}
paths: {}
components:
  schemas:
    A:
      type: object
      properties: {p: {$ref: "#/$defs/n"}}
      $defs: {n: {$ref: "` + url + `#/components/schemas/X"}}
`
}

const externalSchemaDoc = `openapi: 3.1.0
info: {title: O, version: "1"}
paths: {}
components:
  schemas:
    X: {type: string}
`

// TestLoad_AHeldReferenceResolvesThroughADefinitionIntoAnotherDocument pins that
// a held reference whose definition leads into another document resolves, its
// chain reaching that document, which is fetched once.
func TestLoad_AHeldReferenceResolvesThroughADefinitionIntoAnotherDocument(t *testing.T) {
	t.Parallel()
	srv, requests := countingServer(t, externalSchemaDoc)
	src := compilers.Source{Path: "root.yaml", Data: []byte(defsToExternalSpec(srv.URL + "/ext.yaml"))}

	got, diags, err := Load(t.Context(), 0, src, Options{AllowExternalRefs: true})

	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Empty(t, diags)
	assert.Equal(t, int32(1), requests.Load(), "the document is fetched once")
	schema, ok := got.Doc.Components.Schemas.Get("A")
	require.True(t, ok)
	prop, ok := schema.GetSchema().GetProperties().Get("p")
	require.True(t, ok)
	assert.Contains(t, resolutionTrail(prop).docs, srv.URL+"/ext.yaml", "p's chain reached the external document")
}

// TestLoad_AHeldReferenceThatCannotBeResolvedIsReportedAsWritten pins where and
// how a failure of a held reference is reported: at the reference, quoting it
// as the source wrote it, never as the pointer the resolver was handed in its
// place.
func TestLoad_AHeldReferenceThatCannotBeResolvedIsReportedAsWritten(t *testing.T) {
	t.Parallel()
	src := compilers.Source{Path: "root.yaml", Data: []byte(defsToExternalSpec("https://ext.invalid/ext.yaml"))}

	_, diags, err := Load(t.Context(), 0, src, Options{})

	require.NoError(t, err)
	msg := openapitest.DiagMessageAt(t, diags, diag.UnresolvedRef, ir.SeverityError, "/components/schemas/A/properties/p")
	assert.Contains(t, msg, `unresolved $ref "#/$defs/n"`)
	assert.NotContains(t, msg, "/components/schemas/A/$defs/n")
}
