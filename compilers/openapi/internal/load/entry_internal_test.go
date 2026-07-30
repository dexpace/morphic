package load

import (
	"os"
	"testing"

	oas3 "github.com/speakeasy-api/openapi/jsonschema/oas3"
	"github.com/speakeasy-api/openapi/validation"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
)

// sourceOf wraps src as the single source a load call takes.
func sourceOf(src string) compilers.Source {
	return compilers.Source{Path: "spec.yaml", Data: []byte(src)}
}

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
	doc, diags, err := Load(t.Context(), 3, sourceOf("\tnot: yaml\n"), Options{})

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
	doc, diags, err := Load(t.Context(), 0, sourceOf(defaultMapping32Spec), Options{})

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
	doc, _, err := unmarshal(t.Context(), []byte(defaultMapping32Spec))
	require.NoError(t, err)
	require.NotNil(t, doc)

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
	doc, _, err := unmarshal(t.Context(), []byte(spec))
	require.NoError(t, err)
	require.NotNil(t, doc)

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
	doc, diags, err := Load(t.Context(), 0, sourceOf(resolverPanicSpec), Options{})

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
	_, diags, err := Load(t.Context(), 0, sourceOf(spec), Options{})

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
