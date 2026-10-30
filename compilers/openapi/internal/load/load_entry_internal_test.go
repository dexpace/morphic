// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package load

import (
	"context"
	"encoding/json/jsontext"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/compilers/openapi/internal/overlay"
	"github.com/dexpace/morphic/compilers/openapi/internal/sourceindex"
	"github.com/dexpace/morphic/ir"
)

// TestLoad_ADegenerateCycleLoadsAndIsReportedOnce pins the first gate in the
// load path. A document whose schema $refs close a cycle loads: no schema $ref
// reaches the parser's resolver, which is what faulted on one, so the loop is a
// spec problem reported once, at its least member, and not a crash or a
// refusal.
func TestLoad_ADegenerateCycleLoadsAndIsReportedOnce(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile("../../../../testdata/openapi/cycle_self_ref.yaml")
	require.NoError(t, err)

	doc, diags, err := Load(t.Context(), 0, compilers.Source{Path: "cycle.yaml", Data: data}, Options{})

	require.NoError(t, err, "a cyclic document is a spec problem, not a Go error")
	require.NotNil(t, doc, "the document is lowered: %+v", diags)
	assert.Equal(t, []jsontext.Pointer{"/components/schemas/A"}, errorSites(diags, diag.CyclicRef))
	assert.Equal(t, 1, countErrors(diags), "and the loop is the only error: %+v", diags)
}

// TestLoad_UnparseableSourceIsAGoError pins the other side of that split: bytes
// that are not a document at all are an I/O-level failure, so they leave as a Go
// error naming the source rather than as a diagnostic about the spec. (The
// ErrParse sentinel is narrower — it marks only a recovered parser panic, which
// TestUnmarshal_RecoversParserPanic covers.)
func TestLoad_UnparseableSourceIsAGoError(t *testing.T) {
	t.Parallel()
	doc, diags, err := Load(t.Context(), 3, openapitest.SourceOf("\tnot: yaml\n"), Options{})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "source 3", "the failing source is named")
	assert.Nil(t, doc)
	assert.Nil(t, diags, "an unparseable source yields no spec diagnostics")
}

// defaultMapping32Spec is a 3.2 document whose discriminator uses the 3.2-only
// defaultMapping keyword, which the 3.1 grammar has no room for.
const defaultMapping32Spec = `openapi: 3.2.0
info: {title: T, version: "1"}
paths: {}
components:
  schemas:
    Pet:
      type: object
      properties: {petType: {type: string}}
      required: [petType]
      discriminator:
        propertyName: petType
        defaultMapping: '#/components/schemas/Dog'
        mapping: {cat: '#/components/schemas/Cat'}
    Dog: {allOf: [{$ref: '#/components/schemas/Pet'}]}
    Cat: {allOf: [{$ref: '#/components/schemas/Pet'}]}
`

// TestLoad_A32KeywordIn32DocumentDrawsNoFinding pins that a schema is held to
// the minor its document declares: a keyword that minor defines is no finding,
// though an older grammar would refuse it.
func TestLoad_A32KeywordIn32DocumentDrawsNoFinding(t *testing.T) {
	t.Parallel()
	doc, diags, err := Load(t.Context(), 0, openapitest.SourceOf(defaultMapping32Spec), Options{})

	require.NoError(t, err)
	require.NotNil(t, doc)
	assert.Equal(t, "openapi@3.2", doc.Source.Format)
	assert.Empty(t, diags, "a 3.2 keyword in a 3.2 document is no finding")
}

// TestLoad_ResolverFaultBecomesADiagnostic pins the last refusal in the load
// path: a document the parser accepts and the resolver faults on is reported as
// an unresolved reference, so the fault never escapes as a Go error or a crash.
// It is sited at the reference being resolved when the panic hit, the same way
// an ordinary resolution failure is (TestEachReference_PanicIsReportedAtTheReference
// covers the panic-handling mechanism itself).
func TestLoad_ResolverFaultBecomesADiagnostic(t *testing.T) {
	t.Parallel()
	doc, diags, err := Load(t.Context(), 0, openapitest.SourceOf(resolverPanicSpec), Options{})

	require.NoError(t, err, "a resolver fault is a spec problem, not a Go error")
	assert.NotNil(t, doc, "resolution failure does not stop the document being lowered")
	require.Equal(t, 1, countErrorsAt(diags, diag.UnresolvedRef), "and it is reported: %+v", diags)
	for _, d := range diags {
		if d.Code == diag.UnresolvedRef {
			assert.Equal(t, jsontext.Pointer("/components/responses/000"), d.Provenance.Pointer,
				"the reference being resolved when the panic hit")
		}
	}
}

// TestLoad_RecoverableLiteralIsNotAFinding pins that a numeric bound YAML writes
// as .5 is a number, as the table reads it: the library checks the schema as
// JSON text and refuses it, but Morphic reads that bound off the raw node and
// represents it exactly, so it is no problem with the spec.
func TestLoad_RecoverableLiteralIsNotAFinding(t *testing.T) {
	t.Parallel()
	spec := `openapi: 3.1.0
info: {title: T, version: "1"}
paths: {}
components: {schemas: {S: {type: number, minimum: .5}}}
`
	_, diags, err := Load(t.Context(), 0, openapitest.SourceOf(spec), Options{})

	require.NoError(t, err)
	for _, d := range diags {
		assert.False(t, strings.HasPrefix(d.Code, diag.Validation), "a recoverable literal draws no finding: %+v", d)
	}
}

// TestSupportedMinor_AcceptsOnlyTheThreeMinors pins the version gate. A version
// it wrongly accepts reaches a lowering written for a different format; one it
// wrongly rejects refuses a document the compiler can read.
func TestSupportedMinor_AcceptsOnlyTheThreeMinors(t *testing.T) {
	t.Parallel()
	tests := []struct {
		version string
		want    string
	}{
		{version: "3.0.3", want: "3.0"},
		{version: "3.1.0", want: "3.1"},
		{version: "3.2.0", want: "3.2"},
		{version: "3.1", want: "3.1"},
		{version: "3.3.0"},
		{version: "2.0.0"},
		{version: "4.0.0"},
		{version: "3"},
		{version: ""},
		{version: "3..1"},
		{version: ".1"},
	}
	for _, tc := range tests {
		t.Run(tc.version, func(t *testing.T) {
			t.Parallel()
			got, ok := SupportedMinor(tc.version)
			assert.Equal(t, tc.want != "", ok)
			assert.Equal(t, tc.want, got)
		})
	}
}

// TestSupportedMinor_AllocatesNothing pins what the lowering's dialect check
// costs: it asks this of every schema whose null it reads, so the minor is
// read off the version (oas.ParseMinor) and named by a constant. It is not run
// in parallel: AllocsPerRun refuses to be.
func TestSupportedMinor_AllocatesNothing(t *testing.T) {
	assert.Zero(t, testing.AllocsPerRun(100, func() { _, _ = SupportedMinor("3.1.0") }))
}

// overlayHeader is the preamble every overlay document below needs.
const overlayHeader = "overlay: 1.0.0\ninfo: {title: O, version: \"1\"}\nactions:\n"

// overlayOptions wraps one overlay document as the loader's options, at the
// index Compile gives an overlay.
func overlayOptions(body string) Options {
	return Options{Overlay: &overlay.Options{Path: "patch.yaml", Data: []byte(overlayHeader + body)},
		OverlaySrcIndex: 1}
}

// TestLoad_AppliesAnOverlayBeforeBuildingTheModel pins where the patching hook
// sits: the model the loader hands back is built from the patched tree, so a
// component the source never declared is present by the time anything can lower
// it. Applying it after the parse would leave the model describing the file.
func TestLoad_AppliesAnOverlayBeforeBuildingTheModel(t *testing.T) {
	t.Parallel()
	const spec = `openapi: 3.1.0
info: {title: T, version: "1"}
paths: {}
components:
  schemas:
    Pet: {type: object}
`
	got, diags, err := Load(t.Context(), 0, openapitest.SourceOf(spec),
		overlayOptions("  - target: $.components.schemas\n    update:\n      Owner: {type: object}\n"))

	require.NoError(t, err)
	require.NotNil(t, got, "the overlay applies cleanly: %+v", diags)
	_, ok := got.Doc.Components.GetSchemas().Get("Owner")
	assert.True(t, ok, "the overlay's schema reached the parsed model")
	assert.True(t, got.Overlay.Applied(), "and its attribution came back with it")
	assert.Equal(t, "patch.yaml", got.Overlay.Source().Path)
}

// TestLoad_RefusesToLowerAfterAnOverlayFails pins that a failed overlay stops
// the load the way an unsupported version does. The library applies actions in
// order and does not undo the ones that landed, so the tree left behind is
// neither the source nor what the overlay asked for — lowering it would produce
// an IR describing no document anyone has.
func TestLoad_RefusesToLowerAfterAnOverlayFails(t *testing.T) {
	t.Parallel()
	got, diags, err := Load(t.Context(), 0, openapitest.SourceOf(minimal31),
		overlayOptions("  - target: $.paths['/nope']\n    update: {description: x}\n"))

	require.NoError(t, err, "a bad overlay is a document problem, not a Go error")
	assert.Nil(t, got, "nothing is lowered")
	require.True(t, diag.HasError(diags), "and the refusal is reported: %+v", diags)
}

// TestLoad_ACycleAnOverlayIntroducedIsReportedOnce pins that the schema $refs
// are bound over the tree the parser is handed rather than only the bytes on
// disk. The source alone is acyclic, so a loop found only in the patched
// document is one the overlay introduced; it is reported once, at its least
// member.
func TestLoad_ACycleAnOverlayIntroducedIsReportedOnce(t *testing.T) {
	t.Parallel()
	const acyclic = `openapi: 3.1.0
info: {title: T, version: "1"}
paths: {}
components:
  schemas:
    A: {$ref: '#/components/schemas/B'}
    B: {type: string}
`
	clean, cleanDiags, err := Load(t.Context(), 0, openapitest.SourceOf(acyclic), Options{})
	require.NoError(t, err)
	require.NotNil(t, clean, "the source alone gives the binder nothing to find")
	assert.Empty(t, errorSites(cleanDiags, diag.CyclicRef))

	got, diags, err := Load(t.Context(), 0, openapitest.SourceOf(acyclic),
		overlayOptions("  - target: $.components.schemas.B\n    update: {$ref: '#/components/schemas/A'}\n"))

	require.NoError(t, err)
	require.NotNil(t, got, "the patched document is lowered: %+v", diags)
	assert.Equal(t, []jsontext.Pointer{"/components/schemas/A"}, errorSites(diags, diag.CyclicRef),
		"the introduced loop is reported once, at its least member")
}

// TestLoad_ADocumentThatFailsToBuildIsAGoError pins the split between the two
// parse steps. Whitespace decodes as YAML and then faults building the model, so
// it must leave as a Go error naming the source — the same treatment bytes that
// are not YAML at all get, reached one step later.
func TestLoad_ADocumentThatFailsToBuildIsAGoError(t *testing.T) {
	t.Parallel()
	doc, diags, err := Load(t.Context(), 5, openapitest.SourceOf(" "), Options{})

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrParse)
	assert.Contains(t, err.Error(), "source 5", "the failing source is named")
	assert.Nil(t, doc)
	assert.Nil(t, diags)
}

// TestLoad_RejectsAnOverlaySharingTheSourceIndex pins the one way the
// attribution can go wrong without anything noticing.
//
// The overlay's index and its place in Document.Sources are decided in two
// packages and have to agree. An index past the end disagrees loudly — irverify
// reports it as the dangling reference it is — but an index equal to the
// source's addresses a declared entry, so every position the overlay introduced
// would quietly name the spec while Sources carried an entry nothing references.
// That is a caller mistake rather than a document problem, so it leaves as a Go
// error.
func TestLoad_RejectsAnOverlaySharingTheSourceIndex(t *testing.T) {
	t.Parallel()
	opts := overlayOptions("  - target: $.info\n    update: {description: d}\n")
	opts.OverlaySrcIndex = 2

	refused, _, err := Load(t.Context(), 2, openapitest.SourceOf(minimal31), opts)
	require.Error(t, err, "the overlay may not share source 2's index")
	assert.Contains(t, err.Error(), "overlay source index 2", "the collision is named")
	assert.Nil(t, refused)

	opts.OverlaySrcIndex = 3
	got, diags, err := Load(t.Context(), 2, openapitest.SourceOf(minimal31), opts)
	require.NoError(t, err, "an index of its own is fine: %+v", diags)
	assert.True(t, got.Overlay.Applied())
}

// TestLoad_ADocumentTooLargeToIndexIsRefused drives the refusal a document draws
// before the alias scan reads a single alias: one with more YAML nodes than the
// source index walks.
//
// The bound is reached by shrinking it rather than by building a document of
// sourceindex.MaxIndexedNodes nodes — that would be several gigabytes of
// fixture, and the part worth testing is what the loader does with a truncated
// index, not that the walk stops at a number.
func TestLoad_ADocumentTooLargeToIndexIsRefused(t *testing.T) {
	t.Parallel()
	opts := Options{buildIndex: func(ctx context.Context, root *yaml.Node) sourceindex.Index {
		return sourceindex.Build(ctx, root, 1)
	}}

	doc, diags, err := Load(t.Context(), 4, openapitest.SourceOf(minimal31), opts)

	require.NoError(t, err, "an oversized document is a spec problem, not a Go error")
	assert.Nil(t, doc, "nothing is lowered from a document the pre-parse scan cannot cover")
	require.Len(t, diags, 1)
	assert.Equal(t, diag.SourceTooLarge, diags[0].Code)
	assert.Equal(t, ir.SeverityError, diags[0].Severity)
	assert.Equal(t, 4, diags[0].Provenance.Source, "the refusal names the source it read")
}

// TestLoad_TheSameDocumentLoadsOnceItFitsTheIndex is the control for the test
// above: with the real bound in force the identical source loads, so the refusal
// is the bound's doing and not the document's.
func TestLoad_TheSameDocumentLoadsOnceItFitsTheIndex(t *testing.T) {
	t.Parallel()
	got, diags, err := Load(t.Context(), 4, openapitest.SourceOf(minimal31), Options{})

	require.NoError(t, err)
	require.NotNil(t, got)
	assert.False(t, diag.HasError(diags), "unexpected refusal: %+v", diags)
}

// countingIndexBuilder returns base with an index builder that counts its calls,
// and the counter it writes. The count is one load's own, so tests using it stay
// independent of each other and of anything running beside them.
func countingIndexBuilder(base Options) (Options, *int) {
	built := 0
	base.buildIndex = func(ctx context.Context, root *yaml.Node) sourceindex.Index {
		built++
		return defaultIndex(ctx, root)
	}
	return base, &built
}

// TestLoad_IndexesTheSourceOnce guards the shape of this path rather than its
// output: one decode, one index, and every pre-parse refusal reading that index
// instead of walking the tree again. A change that re-added a walk of its own
// would break no assertion about what the compiler reports — the refusals would
// still be right — so the count is the only thing that can hold it.
func TestLoad_IndexesTheSourceOnce(t *testing.T) {
	t.Parallel()
	opts, built := countingIndexBuilder(Options{})

	got, diags, err := Load(t.Context(), 0, openapitest.SourceOf(minimal31), opts)

	require.NoError(t, err)
	require.NotNil(t, got)
	assert.False(t, diag.HasError(diags), "unexpected refusal: %+v", diags)
	assert.Equal(t, 1, *built, "a compile with no overlay indexes its source exactly once")
}

// TestLoad_IndexesAPatchedTreeAgain pins the indexes a compile with an overlay
// builds, which is one per tree and no more. There are three trees: the source,
// the overlay document — refused before the library that applies it follows its
// aliases (GitHub #489) — and the tree the overlay leaves behind, which the
// first index no longer describes and which is what the parser is handed.
func TestLoad_IndexesAPatchedTreeAgain(t *testing.T) {
	t.Parallel()
	opts, built := countingIndexBuilder(
		overlayOptions("  - target: $.info\n    update: {description: d}\n"))

	got, diags, err := Load(t.Context(), 0, openapitest.SourceOf(minimal31), opts)

	require.NoError(t, err)
	require.NotNil(t, got)
	assert.False(t, diag.HasError(diags), "unexpected refusal: %+v", diags)
	assert.Equal(t, 3, *built, "the source, the overlay document, then the tree the overlay left behind")
}

// TestLoad_A32KeywordBesideAPathItemRefDrawsNoFinding pins that the fields
// beside a Path Item's $ref are held to the document's minor as its own: a 3.2
// keyword in a schema written there is no finding.
func TestLoad_A32KeywordBesideAPathItemRefDrawsNoFinding(t *testing.T) {
	t.Parallel()
	_, diags, err := Load(t.Context(), 0, compilers.Source{Path: "spec.yaml", Data: []byte(`openapi: 3.2.0
info: {title: T, version: "1"}
paths:
  /a:
    $ref: '#/paths/~1b'
    put:
      requestBody:
        content:
          application/json:
            schema:
              type: object
              properties: {petType: {type: string}}
              discriminator: {propertyName: petType, defaultMapping: '#/components/schemas/Dog'}
      responses: {"200": {description: ok}}
  /b:
    get: {responses: {"200": {description: ok}}}
components:
  schemas:
    Dog: {type: object}
`)}, Options{})
	require.NoError(t, err)
	openapitest.RequireNoErrorDiags(t, diags)
}

// errorSites returns where each error of the given code is reported, in order.
func errorSites(diags []ir.Diagnostic, code string) []jsontext.Pointer {
	var sites []jsontext.Pointer
	for _, d := range diags {
		if d.Code == code && d.Severity == ir.SeverityError {
			sites = append(sites, d.Provenance.Pointer)
		}
	}
	return sites
}

// countErrors counts the errors among diags, whatever their code.
func countErrors(diags []ir.Diagnostic) int {
	var n int
	for _, d := range diags {
		if d.Severity == ir.SeverityError {
			n++
		}
	}
	return n
}
