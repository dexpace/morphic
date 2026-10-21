// This file is a package-level suite, not a per-source-file test: it asks
// engine.New which formats this build compiles and holds each to its column of
// docs/ir-spec-matrix.md, so a compiler registered tomorrow is held to its own
// with no test code, only the data it is held to.
package conformance_test // external test package — exercises only the public API

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/engine"
)

// TestConformance_EveryRegisteredFormatIsHeldToItsColumn turns the corpus
// contract of docs/architecture.md, one minimal spec per matrix row per format
// that can express it, into something that can disagree with the tree. For each
// format the engine compiles, the witness document beside its corpus must
// account for exactly the rows its matrix column marks expressible.
//
// A row with no spec needs a reason; a row the format cannot express needs
// neither, since a witness would mean the matrix or the corpus is wrong. Whether
// a spec naming a row exercises it is judged by reading, not checked.
func TestConformance_EveryRegisteredFormatIsHeldToItsColumn(t *testing.T) {
	t.Parallel()
	eng, err := engine.New()
	require.NoError(t, err)
	names := formatNames(eng.Formats())
	require.NotEmpty(t, names, "engine.New compiles no format, so no column is held to anything")

	m := readMatrix(t)
	for _, format := range names {
		t.Run(format, func(t *testing.T) {
			t.Parallel()
			at := m.columnsKeyed(format)
			require.Len(t, at, 1, "engine.New registers a compiler for %q, so ir-spec-matrix.md needs exactly "+
				"one format column whose header ends in `%s` as its one code span, and has %d", format, format, len(at))
			text, specs := readCorpus(t, format)
			for _, p := range witnessProblems(m, at[0], format, text, specs) {
				t.Error(p)
			}
		})
	}
}

// formatNames returns the distinct names among formats: a format's column is
// keyed by its name, and one name serves every version of it.
func formatNames(formats []compilers.SourceFormat) []string {
	names := make([]string, 0, len(formats))
	for _, f := range formats {
		names = append(names, f.Name)
	}
	slices.Sort(names)
	return slices.Compact(names)
}
