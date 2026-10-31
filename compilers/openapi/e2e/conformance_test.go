// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

// This file is a package-level suite, not a per-source-file test: it runs the
// capability-conformance corpus (one minimal spec per ir-spec-matrix.md row)
// through the whole compiler to keep "lossless by default" honest, so it has no
// single source file to pair with.
package e2e_test // external test package — exercises only the public API

import (
	"encoding/json/v2"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi"
	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/ir/irtest"
)

// conformanceDir is the corpus of one minimal spec per capability row of
// ir-spec-matrix.md, addressed relative to this test file.
const conformanceDir = "../../../testdata/conformance/openapi"

// witnessDoc is the file in the corpus directory that names the matrix rows each
// spec witnesses. It is no spec, and internal/conformance reads it.
const witnessDoc = "matrix.md"

// TestConformance drives one minimal spec per OpenAPI-expressible capability
// through the full compiler and asserts lossless capture: a focused
// capability-specific assertion plus a byte-exact golden IR snapshot.
// Regenerate the goldens with
// `go test ./compilers/openapi/e2e -run TestConformance -update`.
//
// What the corpus does not cover is derived, not described, so nothing about
// its reach is claimed by hand: `go test ./cmd/morphic-census` holds
// testdata/unwitnessed.golden.txt to every IR fact no committed spec sets. Some
// of what it lists no spec can reach, such as a field only another source
// format writes.
func TestConformance(t *testing.T) {
	t.Parallel()
	for _, tc := range conformanceCases() {
		t.Run(tc.file, func(t *testing.T) {
			t.Parallel()
			doc, diags := parseCorpus(t, tc.file)
			assertOnlyErrorDiags(t, diags, invalidCases()[tc.file])
			tc.assert(t, doc, diags)
			// A failed capability assertion must not reach the golden. require
			// aborts, but assert does not, and under -update a non-fatal failure
			// would rewrite the snapshot with the regressed document — leaving a
			// loud run behind a file that now agrees with the regression.
			require.False(t, t.Failed(), "capability assertion failed; not comparing or rewriting the golden")
			irtest.CompareGolden(t, filepath.Join(conformanceDir, tc.file+".golden.json"), doc)
			assertJSONRoundTrip(t, doc)
		})
	}
}

// TestConformance_TableNamesEveryCorpusSpec requires the table and the corpus
// directory to name the same specs. Both directions matter: a spec with no row
// gets neither a capability assertion nor a golden while the corpus-wide sweeps
// (dangling references, the fuzz seed, the census of unwitnessed facts) still
// read it, so it looks covered; a row naming a deleted spec fails the other
// way. Comparing sorted lists rather than sets also catches a spec named by two
// rows.
func TestConformance_TableNamesEveryCorpusSpec(t *testing.T) {
	t.Parallel()
	onDisk := corpusSpecNames(t)
	cases := conformanceCases()
	inTable := make([]string, 0, len(cases))
	for _, tc := range cases {
		inTable = append(inTable, tc.file)
	}
	slices.Sort(onDisk)
	slices.Sort(inTable)
	if diff := cmp.Diff(onDisk, inTable); diff != "" {
		t.Errorf("corpus and table disagree (-on disk +in table):\n%s", diff)
	}
}

// corpusSpecNames returns the base name of every spec in the corpus directory,
// failing on any file that is neither a .yaml spec, a golden beside one nor the
// witness document.
//
// The .yaml-only restriction is asserted rather than left to a narrower glob.
// parseCorpus reads only "<name>.yaml", so a spec committed as .yml or .json
// could not be named by a table row, yet the census of the corpus that
// cmd/morphic-census holds to its golden reads all three extensions and would
// count its facts as witnessed, leaving it with neither assertion nor golden.
func corpusSpecNames(t *testing.T) []string {
	t.Helper()
	entries, err := os.ReadDir(conformanceDir)
	require.NoError(t, err)

	specs := make([]string, 0, len(entries))
	goldens := make([]string, 0, len(entries))
	for _, e := range entries {
		name := e.Name()
		require.False(t, e.IsDir(), "corpus directory holds a subdirectory %q; no sweep descends into one", name)
		switch {
		case name == witnessDoc: // names rows, not a spec
		case strings.HasSuffix(name, ".golden.json"):
			goldens = append(goldens, strings.TrimSuffix(name, ".golden.json"))
		case strings.HasSuffix(name, ".yaml"):
			specs = append(specs, strings.TrimSuffix(name, ".yaml"))
		default:
			t.Errorf("corpus file %q is neither a .yaml spec nor a golden; "+
				"parseCorpus reads only .yaml, so a spec in any other extension gets no table row", name)
		}
	}
	require.NotEmpty(t, specs, "the corpus directory must hold specs")
	slices.Sort(specs)
	slices.Sort(goldens)
	if diff := cmp.Diff(specs, goldens); diff != "" {
		t.Errorf("corpus specs and goldens disagree (-specs +goldens):\n%s", diff)
	}
	return specs
}

// conformanceCase pairs one corpus spec with the assertion that its capability
// is captured losslessly.
//
// Which ir-spec-matrix.md rows a spec witnesses is data, not a field here: it
// lives in testdata/conformance/openapi/matrix.md, which a test outside this
// package can read. A spec pinning a construct the matrix has no row for, such
// as a dialect keyword, appears under no row there.
type conformanceCase struct {
	file   string
	assert func(*testing.T, *ir.Document, []ir.Diagnostic)
}

// conformanceCases is the corpus table: one row per spec, each naming the file
// and the assertion that reads it.
func conformanceCases() []conformanceCase {
	return []conformanceCase{
		{"named-types", assertNamedTypes},
		{"neutral-naming", assertNeutralNaming},
		{"empty-names", assertEmptyNames},
		{"inline-types", assertInlineTypes},
		{"component-reuse", assertComponentReuse},
		{"shared-response-across-status", assertSharedResponseAcrossStatus},
		{"allof-inheritance", assertAllOfInheritance},
		{"allof-mixins", assertAllOfMixins},
		{"allof-inline-merge", assertAllOfInlineMerge},
		{"allof-required-only", assertAllOfRequiredOnly},
		{"required-members", assertRequiredMembers},
		{"allof-oneof-cooccurrence", assertAllOfOneOfCooccurrence},
		{"allof-inline-residue", assertAllOfInlineResidue},
		{"allof-nested-inline", assertAllOfNestedInline},
		{"allof-ref-branch-siblings", assertAllOfRefBranchSiblings},
		{"allof-boolean-branch", assertAllOfBooleanBranch},
		{"allof-conflicting-type", assertAllOfConflictingType},
		{"allof-narrowing-redeclaration", assertAllOfNarrowingRedeclaration},
		{"allof-redeclared-models", assertAllOfRedeclaredModels},
		{"allof-position-constraints", assertAllOfPositionConstraints},
		{"oneof-discriminated", assertOneOfDiscriminated},
		{"anyof-discriminated", assertAnyOfDiscriminated},
		{"discriminator-inheritance", assertDiscriminatorInheritance},
		{"discriminator-inline-subtype", assertDiscriminatorInlineSubtype},
		{"discriminator-default-mapping", assertDiscriminatorDefaultMapping},
		{"discriminator-transitive", assertDiscriminatorTransitive},
		{"discriminator-alias-mapping", assertDiscriminatorAliasMapping},
		{"discriminated-union-beside-model", assertDiscriminatedUnionBesideModel},
		{"nullable-union-discriminator", assertNullableUnionDiscriminator},
		{"unhomed-keywords", assertUnhomedKeywords},
		{"union-beside-ref", assertUnionBesideRef},
		{"codeclared-keywords", assertCoDeclaredKeywords},
		{"codeclared-schema-content", assertCoDeclaredSchemaContent},
		{"anyof-untagged", assertAnyOfUntagged},
		{"union-decorated-branch", assertUnionDecoratedBranch},
		{"negation-not", assertNegationNot},
		{"dependent-required", assertDependentRequired},
		{"dialect-keywords", assertDialectKeywords},
		{"dynamic-ref", assertDynamicRef},
		{"schema-identifiers", assertSchemaIdentifiers},
		{"enum-string", assertEnumString},
		{"enum-numeric", assertEnumNumeric},
		{"empty-enum", assertEmptyEnum},
		{"missing-array-items", assertMissingArrayItems},
		{"scalar-format", assertScalarFormat},
		{"secret-format", assertSecretFormat},
		{"encoding-byte", assertEncodingByte},
		{"content-vocabulary", assertContentVocabulary},
		{"xml-hints", assertXMLHints},
		{"nullability-four-states", assertNullabilityFourStates},
		{"nullable-30", assertNullable30},
		{"nullable-31-ref", assertNullable31Ref},
		{"nullable-enum-31", assertNullableEnum31},
		{"nullable-keyword-30", assertNullableKeyword30},
		{"nullable-keyword-31", assertNullableKeyword31},
		{"schema-ref-siblings-30", assertSchemaRefSiblings30},
		{"nullability-conjunction", assertNullabilityConjunction},
		{"null-only-union", assertNullOnlyUnion},
		{"defaults", assertDefaults},
		{"yaml-timestamp-scalars", assertYAMLTimestampScalars},
		{"constraints", assertConstraints},
		{"numeric-precision", assertNumericPrecision},
		{"readonly-writeonly", assertReadOnlyWriteOnly},
		{"recursive", assertRecursive},
		{"maps", assertMaps},
		{"tuples-prefixitems", assertTuples},
		{"literal-const", assertLiteralConst},
		{"tags-grouping", assertTagsGrouping},
		{"group-identity", assertGroupIdentity},
		{"param-identity", assertParamIdentity},
		{"enum-member-identity", assertEnumMemberIdentity},
		{"http-binding", assertHTTPBinding},
		{"param-styles", assertParamStyles},
		{"param-style-matrix", assertParamStyleMatrix},
		{"param-querystring", assertParamQuerystring},
		{"querystring-forbidden-keywords", assertQuerystringForbiddenKeywords},
		{"param-allowreserved-locations", assertParamAllowReservedLocations},
		{laterVersionCase, assertLaterVersionConstructs},
		{"param-xml-residue", assertParamXMLResidue},
		{"param-ref-inheritance", assertParamRefInheritance},
		{"header-content-schema", assertHeaderContentSchema},
		{"multi-content", assertMultiContent},
		{"multipart-encoding", assertMultipartEncoding},
		{"file-body", assertFileBody},
		{"media-type-spelling", assertMediaTypeSpelling},
		{"sequential-media", assertSequentialMedia},
		{"streaming-media-30", assertStreamingMedia30},
		{"streaming-media-31", assertStreamingMedia31},
		{"per-status-errors", assertPerStatusErrors},
		{"default-response", assertDefaultResponse},
		{"response-links", assertResponseLinks},
		{"webhooks", assertWebhooks},
		{"callbacks", assertCallbacks},
		{"inline-hoist-positions", assertInlineHoistPositions},
		{"path-item-docs", assertPathItemDocs},
		{"path-item-operations", assertPathItemOperations},
		{"path-item-anchored-key", assertPathItemAnchoredKey},
		{"yaml-merge-keys", assertYAMLMergeKeys},
		{"path-item-ref-siblings", assertPathItemRefSiblings},
		{"deprecation", assertDeprecation},
		{"extension-promotion", assertExtensionPromotion},
		{"examples", assertExamples},
		{"docs-summary-desc", assertDocsSummaryDesc},
		{"request-body-docs", assertRequestBodyDocs},
		{"ref-site-docs", assertRefSiteDocs},
		{"param-content-fields", assertParamContentFields},
		{"examples-32", assertExamples32},
		{"tags-grouping-32", assertTagsGrouping32},
		{"response-summary-32", assertResponseSummary32},
		{"component-media-types-32", assertComponentMediaTypes32},
		{"xml-nodetype-32", assertXMLNodeType32},
		{"nested-encoding-32", assertNestedEncoding32},
		{"unreferenced-components", assertUnreferencedComponents},
		{"self-uri", assertSelfURI},
		{"extensions-x", assertExtensionsX},
		{"inline-annotations", assertInlineAnnotations},
		{"inline-residue", assertInlineResidue},
		{"servers-variables", assertServersVariables},
		{"security-schemes", assertSecuritySchemes},
		{"oauth2-no-flow", assertOAuth2NoFlow},
		{"empty-required-values", assertEmptyRequiredValues},
		{"relaxed-required-fields", assertRelaxedRequiredFields},
		{"empty-request-content", assertEmptyRequestContent},
		{"oauth-flow-fields", assertOAuthFlowFields},
		{"reached-outside-model", assertReachedOutsideModel},
		{"security-or-and", assertSecurityOrAnd},
	}
}

// parseCorpus reads and parses one corpus spec through the full compiler.
func parseCorpus(t *testing.T, name string) (*ir.Document, []ir.Diagnostic) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(conformanceDir, name+".yaml"))
	require.NoError(t, err)
	doc, diags, err := openapi.New().Compile(t.Context(),
		[]compilers.Source{{Path: name + ".yaml", Data: data}}, compilers.Options{})
	require.NoError(t, err)
	require.NotNil(t, doc)
	return doc, diags
}

// assertJSONRoundTrip marshals doc, reads it back, and re-marshals, requiring
// the two encodings to be byte-identical — the serialized oracle fuzz_test.go
// and internal/harness already apply, compared as JSON so the unpreservable
// nil-vs-empty-collection distinction is ignored while real loss still shows.
//
// Invariant 7 is asserted per corpus document rather than only on the golden
// one because each spec reaches IR shapes the others never build — a reserved
// map key, a preserved raw payload, a sum-typed node — and only the shapes a
// document contains can round-trip wrongly.
func assertJSONRoundTrip(t *testing.T, doc *ir.Document) {
	t.Helper()
	first, err := json.Marshal(doc)
	require.NoError(t, err)
	var back ir.Document
	require.NoError(t, json.Unmarshal(first, &back))
	second, err := json.Marshal(&back)
	require.NoError(t, err)
	assert.Equal(t, string(first), string(second), "round-trip must preserve the document")
}

// invalidCases lists the corpus specs that write a construct the specification
// forbids, because what they pin is how such a document lowers: each draws
// errors of the codes listed, and of no other.
func invalidCases() map[string][]string {
	return map[string][]string{
		// A parameter and a header writing both schema and content: "but not both".
		"codeclared-schema-content": {"openapi/validation/validation-mutually-exclusive-fields"},
	}
}
