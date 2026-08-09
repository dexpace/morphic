package load

import (
	"os"
	"testing"

	oas3 "github.com/speakeasy-api/openapi/jsonschema/oas3"
	"github.com/speakeasy-api/openapi/validation"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/compilers/openapi/internal/overlay"
	"github.com/dexpace/morphic/compilers/openapi/internal/sourceindex"
	"github.com/dexpace/morphic/ir"
)

// TestLoad_DegenerateCycleIsRefusedBeforeParsing pins the first gate in the
// load path. A document whose references close a cycle is refused with a
// diagnostic rather than handed to a parser that would fault on it, so the
// refusal is a spec problem and not a crash.
func TestLoad_DegenerateCycleIsRefusedBeforeParsing(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile("../../../../testdata/openapi/cycle_self_ref.yaml")
	require.NoError(t, err)

	doc, diags, err := Load(t.Context(), 0, compilers.Source{Path: "cycle.yaml", Data: data}, Options{})

	require.NoError(t, err, "a cyclic document is a spec problem, not a Go error")
	assert.Nil(t, doc, "nothing is lowered")
	require.True(t, diag.HasError(diags), "and the refusal is reported: %+v", diags)
}

// TestLoad_UnparseableSourceIsAGoError pins the other side of that split: bytes
// that are not a document at all are an I/O-level failure, so they leave as a Go
// error naming the source rather than as a diagnostic about the spec. (The
// errParse sentinel is narrower — it marks only a recovered parser panic, which
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
// defaultMapping keyword. The library validates every schema object against the
// 3.1 meta-schema unless told the document's version, so this is exactly the
// finding metaSchemaVersionArtifacts exists to drop.
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

// TestLoad_32KeywordIsNotAnInvalidSchema pins the meta-schema reconciliation
// end to end. A keyword the document's own version defines must not be reported
// as invalid merely because the library checked it against an older
// meta-schema — the finding appears in the library's run and not in the
// document-version run, which is what makes it an artifact rather than a defect.
func TestLoad_32KeywordIsNotAnInvalidSchema(t *testing.T) {
	t.Parallel()
	doc, diags, err := Load(t.Context(), 0, openapitest.SourceOf(defaultMapping32Spec), Options{})

	require.NoError(t, err)
	require.NotNil(t, doc)
	assert.Equal(t, "openapi@3.2", doc.Source.Format)
	assert.Zero(t, countErrorsAt(diags, diag.Validation+"/validation-invalid-schema"),
		"a 3.2 keyword in a 3.2 document is not an invalid schema: %+v", diags)
}

// TestMetaSchemaVersionArtifacts_ReconcilesOnlyWhatTheTwoRunsDisagreeOn pins the
// difference the reconciliation is: a finding both runs raise is real and stays,
// so the set returned holds only findings the library raised and the document's
// own version did not.
func TestMetaSchemaVersionArtifacts_ReconcilesOnlyWhatTheTwoRunsDisagreeOn(t *testing.T) {
	t.Parallel()
	doc, _ := parseSpec(t, defaultMapping32Spec)

	dropped := metaSchemaVersionArtifacts(t.Context(), doc, "3.2")
	assert.NotEmpty(t, dropped, "the 3.2-only keyword is a finding the library raises alone")

	atVersion := schemaFindings(t.Context(), doc)
	for site := range dropped {
		assert.Contains(t, atVersion, site,
			"a dropped finding is one the library's own run raised")
	}
}

// TestMetaSchemaVersionArtifacts_AFindingBothRunsRaiseIsKept pins the other half
// of the difference. A schema that is invalid at the document's own version is a
// real defect, so it must survive the reconciliation rather than being dropped
// alongside the version artifact beside it.
func TestMetaSchemaVersionArtifacts_AFindingBothRunsRaiseIsKept(t *testing.T) {
	t.Parallel()
	spec := defaultMapping32Spec + "    Broken: {type: 42}\n"
	doc, _ := parseSpec(t, spec)

	atVersion := schemaFindings(t.Context(), doc,
		validation.WithContextObject(&oas3.ParentDocumentVersion{OpenAPI: &doc.OpenAPI}))
	require.NotEmpty(t, atVersion, "the invalid schema is a finding at 3.2 too")

	dropped := metaSchemaVersionArtifacts(t.Context(), doc, "3.2")
	for site := range atVersion {
		assert.NotContains(t, dropped, site, "a real finding is never dropped as an artifact")
	}
}

// TestLoad_ResolverFaultBecomesADiagnostic pins the last refusal in the load
// path: a document the parser accepts and the resolver faults on is reported as
// an unresolved reference, so the fault never escapes as a Go error or a crash.
func TestLoad_ResolverFaultBecomesADiagnostic(t *testing.T) {
	t.Parallel()
	doc, diags, err := Load(t.Context(), 0, openapitest.SourceOf(resolverPanicSpec), Options{})

	require.NoError(t, err, "a resolver fault is a spec problem, not a Go error")
	assert.NotNil(t, doc, "resolution failure does not stop the document being lowered")
	assert.NotZero(t, countErrorsAt(diags, diag.UnresolvedRef),
		"and it is reported: %+v", diags)
}

// TestLoad_RecoverableLiteralIsNotAnInvalidSyntaxFinding pins the numeric-literal
// half of the artifact suppression. The library reports a document carrying .5 as
// invalid JSON, but Morphic reads that bound off the raw node and represents it
// exactly, so the finding describes a limitation of the library rather than a
// problem with the spec.
func TestLoad_RecoverableLiteralIsNotAnInvalidSyntaxFinding(t *testing.T) {
	t.Parallel()
	spec := `openapi: 3.1.0
info: {title: T, version: "1"}
paths: {}
components: {schemas: {S: {type: number, minimum: .5}}}
`
	_, diags, err := Load(t.Context(), 0, openapitest.SourceOf(spec), Options{})

	require.NoError(t, err)
	assert.Zero(t, countErrorsAt(diags,
		diag.Validation+"/"+string(validation.RuleValidationInvalidSyntax)),
		"a finding a recoverable literal explains stays suppressed: %+v", diags)
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

// TestLoad_RefusesACycleAnOverlayIntroduced pins that the pre-parse refusals
// cover the tree the parser is handed rather than only the bytes on disk. The
// source alone is acyclic, so the scan that runs before the overlay cannot see
// this; without the second scan the cycle reaches the third-party resolver,
// which is the crash those refusals exist to prevent.
func TestLoad_RefusesACycleAnOverlayIntroduced(t *testing.T) {
	t.Parallel()
	const acyclic = `openapi: 3.1.0
info: {title: T, version: "1"}
paths: {}
components:
  schemas:
    A: {$ref: '#/components/schemas/B'}
    B: {type: string}
`
	clean, _, err := Load(t.Context(), 0, openapitest.SourceOf(acyclic), Options{})
	require.NoError(t, err)
	require.NotNil(t, clean, "the source alone gives the scan nothing to find")

	got, diags, err := Load(t.Context(), 0, openapitest.SourceOf(acyclic),
		overlayOptions("  - target: $.components.schemas.B\n    update: {$ref: '#/components/schemas/A'}\n"))

	require.NoError(t, err)
	assert.Nil(t, got, "the patched document is refused before the resolver sees it")
	assert.Equal(t, 1, countErrorsAt(diags, diag.CyclicRef), "the introduced cycle is what refused it")
}

// TestLoad_ADocumentThatFailsToBuildIsAGoError pins the split between the two
// parse steps. Whitespace decodes as YAML and then faults building the model, so
// it must leave as a Go error naming the source — the same treatment bytes that
// are not YAML at all get, reached one step later.
func TestLoad_ADocumentThatFailsToBuildIsAGoError(t *testing.T) {
	t.Parallel()
	doc, diags, err := Load(t.Context(), 5, openapitest.SourceOf(" "), Options{})

	require.Error(t, err)
	assert.ErrorIs(t, err, errParse)
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
// before the cycle scan reads a single reference: one with more YAML nodes than
// the source index walks.
//
// The bound is reached by shrinking it rather than by building a document of
// sourceindex.MaxIndexedNodes nodes — that would be several gigabytes of
// fixture, and the part worth testing is what the loader does with a truncated
// index, not that the walk stops at a number. It is not parallel, because it
// rebinds a package-level value the parallel tests around it also read.
func TestLoad_ADocumentTooLargeToIndexIsRefused(t *testing.T) {
	orig := buildIndex
	t.Cleanup(func() { buildIndex = orig })
	buildIndex = func(root *yaml.Node) sourceindex.Index { return sourceindex.Build(root, 1) }

	doc, diags, err := Load(t.Context(), 4, openapitest.SourceOf(minimal31), Options{})

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

// countingIndexBuilder makes the loader count its index builds for one test,
// and returns the counter. It is not parallel-safe, for the reason
// TestLoad_ADocumentTooLargeToIndexIsRefused is not.
func countingIndexBuilder(t *testing.T) *int {
	t.Helper()
	orig := buildIndex
	t.Cleanup(func() { buildIndex = orig })
	built := 0
	buildIndex = func(root *yaml.Node) sourceindex.Index {
		built++
		return orig(root)
	}
	return &built
}

// TestLoad_IndexesTheSourceOnce guards the shape of this path rather than its
// output: one decode, one index, and every pre-parse refusal reading that index
// instead of walking the tree again. A change that re-added a walk of its own
// would break no assertion about what the compiler reports — the refusals would
// still be right — so the count is the only thing that can hold it.
func TestLoad_IndexesTheSourceOnce(t *testing.T) {
	built := countingIndexBuilder(t)

	got, diags, err := Load(t.Context(), 0, openapitest.SourceOf(minimal31), Options{})

	require.NoError(t, err)
	require.NotNil(t, got)
	assert.False(t, diag.HasError(diags), "unexpected refusal: %+v", diags)
	assert.Equal(t, 1, *built, "a compile with no overlay indexes its source exactly once")
}

// TestLoad_IndexesAPatchedTreeAgain is the one second index that is correct: an
// overlay leaves behind a tree the first one no longer describes, and what the
// refusals answer for is the tree the parser is handed.
func TestLoad_IndexesAPatchedTreeAgain(t *testing.T) {
	built := countingIndexBuilder(t)

	got, diags, err := Load(t.Context(), 0, openapitest.SourceOf(minimal31),
		overlayOptions("  - target: $.info\n    update: {description: d}\n"))

	require.NoError(t, err)
	require.NotNil(t, got)
	assert.False(t, diag.HasError(diags), "unexpected refusal: %+v", diags)
	assert.Equal(t, 2, *built, "the source, then the tree the overlay left behind")
}
