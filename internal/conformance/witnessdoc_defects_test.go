// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

// This file is a package-level suite, not a per-source-file test: it plants,
// in a small matrix and a witness document written as text, each defect the
// checks of matrix_test.go and witnessdoc_test.go exist to report, and
// requires each to be reported once and nothing else to be.
package conformance_test // external test package — exercises only the public API

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
)

// plantedMatrix is a matrix of two format columns and four rows: alpha and beta
// are expressible in the second column, gamma is absent there, and delta's cell
// there has lost its marker.
func plantedMatrix() matrix {
	return matrix{
		columns: []matrixColumn{{header: "Other `other`"}, {header: "Planted `planted`"}},
		rows: []matrixRow{
			{key: "alpha", capability: "Alpha", cells: []string{"✅", "✅ native"}},
			{key: "beta", capability: "Beta", cells: []string{"✅", "⚠ indirect"}},
			{key: "gamma", capability: "Gamma", cells: []string{"✅", "— absent"}},
			{key: "delta", capability: "Delta", cells: []string{"✅", "unmarked"}},
		},
	}
}

// plantedSpecs are the specs the planted corpus holds.
var plantedSpecs = []string{"one", "two", "three"}

// Entries a planted document is assembled from.
const (
	alphaWitnessed = "### `alpha`\n\n- Witnesses: `one`, `two`\n\n"
	betaUncovered  = "### `beta`\n\n- Uncovered: nothing reads it yet; this retires when a pass lands\n\n"
)

// TestWitnessProblems_ReportsEachPlantedDefect gives witnessProblems a document
// with one defect at a time and requires exactly the messages listed, in order.
// The first case is the control: a document that is right says nothing, so a
// message in the others is the defect's and not the fixture's.
func TestWitnessProblems_ReportsEachPlantedDefect(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		text string
		want []string
	}{
		{"a document that is right", alphaWitnessed + betaUncovered, nil},
		{"a witness list that runs over lines", "### `alpha`\n\n- Witnesses: `one`,\n  `two`\n\n" + betaUncovered, nil},
		{"a row with no entry", alphaWitnessed,
			[]string{`matrix row "beta" (Beta) is marked expressible for planted but has no entry`}},
		{"an entry for a row marked absent", alphaWitnessed + betaUncovered + "### `gamma`\n\n- Uncovered: x\n",
			[]string{`entry "gamma" is for a row ir-spec-matrix.md marks absent (—) for planted`}},
		{"an entry for a row the matrix lacks", alphaWitnessed + betaUncovered + "### `zeta`\n\n- Uncovered: x\n",
			[]string{`entry "zeta" names no row of ir-spec-matrix.md`}},
		{"an entry for a row with no marker", alphaWitnessed + betaUncovered + "### `delta`\n\n- Witnesses: `three`\n",
			nil},
		{"an entry written twice", alphaWitnessed + alphaWitnessed + betaUncovered,
			[]string{`entry "alpha" repeats the one at line 1`}},
		{"both fields", "### `alpha`\n\n- Witnesses: `one`\n- Uncovered: x\n\n" + betaUncovered,
			[]string{`entry "alpha" needs exactly one of Witnesses and Uncovered, and has both`}},
		{"neither field", "### `alpha`\n\n" + betaUncovered,
			[]string{`entry "alpha" needs exactly one of Witnesses and Uncovered, and has neither`}},
		{"an empty reason", alphaWitnessed + "### `beta`\n\n- Uncovered:\n",
			[]string{`entry "beta" has an empty Uncovered field`}},
		{"no witness", "### `alpha`\n\n- Witnesses:\n\n" + betaUncovered,
			[]string{`entry "alpha" lists no spec in Witnesses`}},
		{"a witness written outside code", "### `alpha`\n\n- Witnesses: `one`, two\n\n" + betaUncovered,
			[]string{`entry "alpha" writes "two" in Witnesses outside a code span`}},
		{"a witness named twice", "### `alpha`\n\n- Witnesses: `one`, `one`\n\n" + betaUncovered,
			[]string{`entry "alpha" names "one" twice`}},
		{"a witness that is no spec", "### `alpha`\n\n- Witnesses: `one`, `nine`\n\n" + betaUncovered,
			[]string{`entry "alpha" names "nine", which is no spec in testdata/conformance/planted/`}},
		{"a field no reader knows", "### `alpha`\n\n- Witnesses: `one`\n- Note: x\n\n" + betaUncovered,
			[]string{`alpha writes a field no reader knows`}},
		{"a heading that is no key", alphaWitnessed + betaUncovered + "### Gamma\n",
			[]string{`a third-level heading is not a key`}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := witnessProblems(plantedMatrix(), 1, "planted", tc.text, plantedSpecs)
			require.Len(t, got, len(tc.want), "problems: %q", got)
			for i, want := range tc.want {
				assert.Contains(t, got[i], want)
				assert.Contains(t, got[i], "testdata/conformance/planted/matrix.md", "a message says which file to change")
			}
		})
	}
}

// TestWitnessProblems_JudgesTheColumnItIsGiven plants the one mistake a pair of
// columns allows: reading the neighbour's. Gamma is expressible in the first and
// absent in the second, so the same document is right for one and wrong for the
// other.
func TestWitnessProblems_JudgesTheColumnItIsGiven(t *testing.T) {
	t.Parallel()
	text := alphaWitnessed + betaUncovered + "### `gamma`\n\n- Uncovered: x\n\n### `delta`\n\n- Uncovered: x\n"
	assert.Empty(t, witnessProblems(plantedMatrix(), 0, "other", text, plantedSpecs))

	got := witnessProblems(plantedMatrix(), 1, "planted", text, plantedSpecs)
	require.Len(t, got, 1, "problems: %q", got)
	assert.Contains(t, got[0], `entry "gamma" is for a row`)
}

// TestSpecNames_SkipsWhatIsNoSpec requires the witness document and the golden
// beside a spec to stay out of the spec list, and every other extension in.
func TestSpecNames_SkipsWhatIsNoSpec(t *testing.T) {
	t.Parallel()
	got := specNames([]string{"a.yaml", "a.golden.json", "b.json", "matrix.md", "c.yml"})
	if diff := cmp.Diff([]string{"a", "b", "c"}, got); diff != "" {
		t.Errorf("spec names (-want +got):\n%s", diff)
	}
}

// TestFormatNames_NamesEachFormatOnce requires the versions of one format to
// collapse into one column to hold.
func TestFormatNames_NamesEachFormatOnce(t *testing.T) {
	t.Parallel()
	got := formatNames([]compilers.SourceFormat{
		{Name: "b", Version: "1"}, {Name: "a", Version: "3.1"}, {Name: "a", Version: "3.0"},
	})
	if diff := cmp.Diff([]string{"a", "b"}, got); diff != "" {
		t.Errorf("format names (-want +got):\n%s", diff)
	}
}

// TestColumnKeyProblems_ReportsEachPlantedDefect plants the ways a format
// column's header fails to carry its key.
func TestColumnKeyProblems_ReportsEachPlantedDefect(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		headers []string
		want    string
	}{
		{"keyed headers", []string{"One `one`", "Two 2.0 `two`"}, ""},
		{"no code span", []string{"One `one`", "Two"}, `"Two" carries 0 code spans`},
		{"two code spans", []string{"One `one` `uno`"}, `carries 2 code spans`},
		{"a key that is no slug", []string{"One `One_1`"}, `is keyed "One_1", which is no lowercase slug`},
		{"a key two columns share", []string{"One `one`", "Uno `one`"}, `share the key "one"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			columns := make([]matrixColumn, 0, len(tc.headers))
			for _, h := range tc.headers {
				columns = append(columns, matrixColumn{header: h})
			}
			got := columnKeyProblems(columns)
			if tc.want == "" {
				assert.Empty(t, got)
				return
			}
			require.Len(t, got, 1)
			assert.Contains(t, got[0], tc.want)
		})
	}
}
