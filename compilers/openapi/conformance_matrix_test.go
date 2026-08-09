// This file is a package-level suite, not a per-source-file test: it reads
// docs/ir-spec-matrix.md and measures the whole committed corpus against it, so
// it pairs with no single source file.
package openapi_test // external test package — exercises only the public API

import (
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// matrixPath is the capability matrix, addressed relative to this test file.
const matrixPath = "../../docs/ir-spec-matrix.md"

// matrixHeaderPrefix opens the one keyed capability table in the matrix.
const matrixHeaderPrefix = "| Key |"

// matrixOpenAPIColumn is the header of the column this compiler answers to.
const matrixOpenAPIColumn = "OpenAPI 3.x"

// matrixKeyPattern is the shape a row key must have: a lowercase slug. Keys are
// spelled in Go string literals and in a markdown table, so anything looser
// makes two spellings of one row possible.
var matrixKeyPattern = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// matrixRow is one capability row: its stable key, the capability it names, and
// the cell saying how OpenAPI expresses it.
type matrixRow struct {
	key        string
	capability string
	openAPI    string
}

// TestMatrix_RowsCarryUniqueSlugKeys pins the half of the contract that lives in
// the document: every row has a key, the keys are unique, and every OpenAPI cell
// opens with one of the legend's three markers.
//
// The marker check is what makes the coverage test below trustworthy. It reads
// an unmarked cell as neither expressible nor absent, so a row whose marker was
// lost in an edit would drop out of the corpus contract without failing
// anything — the silent direction of the failure, which is why it is rejected
// here rather than defaulted.
func TestMatrix_RowsCarryUniqueSlugKeys(t *testing.T) {
	t.Parallel()
	rows := readMatrixRows(t)
	seen := make(map[string]string, len(rows))
	for _, row := range rows {
		assert.Regexp(t, matrixKeyPattern, row.key, "row %q needs a lowercase-slug key", row.capability)
		assert.NotContains(t, seen, row.key,
			"rows %q and %q share the key %q", seen[row.key], row.capability, row.key)
		seen[row.key] = row.capability
		openAPIExpressible(t, row)
	}
	assert.Len(t, seen, len(rows), "every row contributes a distinct key")
}

// TestConformance_EveryExpressibleMatrixRowIsWitnessed is the corpus contract
// CLAUDE.md and docs/architecture.md state in prose — "one minimal spec per
// ir-spec-matrix.md row per format that can express it" — as something that can
// disagree with the tree.
//
// It runs row → spec. The reverse direction, spec → row, is
// TestConformance_TableNamesEveryCorpusSpec's: between them, a spec cannot be
// added without a table row and a row cannot be added to the matrix without
// either a witnessing spec or a written reason there is none yet.
//
// A row OpenAPI cannot express must have no witness either. That direction
// catches the matrix and the corpus disagreeing the other way round: a spec
// naming such a row means one of the two is wrong about the source format, and
// leaving it unchecked would let the corpus quietly redefine the matrix.
func TestConformance_EveryExpressibleMatrixRowIsWitnessed(t *testing.T) {
	t.Parallel()
	witnesses := matrixRowWitnesses(t)
	uncovered := matrixRowsUncovered()
	for _, row := range readMatrixRows(t) {
		if !openAPIExpressible(t, row) {
			assert.NotContains(t, witnesses, row.key,
				"matrix row %q is marked absent for OpenAPI, yet %v witness it",
				row.key, witnesses[row.key])
			continue
		}
		if reason, excused := uncovered[row.key]; excused {
			assert.NotEmpty(t, reason, "matrixRowsUncovered must say why %q has no spec", row.key)
			assert.NotContains(t, witnesses, row.key,
				"matrix row %q is listed uncovered yet witnessed by %v; delete its line",
				row.key, witnesses[row.key])
			continue
		}
		assert.Contains(t, witnesses, row.key,
			"matrix row %q (%s) has no conformance spec: add one naming it in conformanceCases, "+
				"or list it in matrixRowsUncovered with the reason it has none", row.key, row.capability)
	}
}

// TestConformance_MatrixRowNamesResolve requires every key spelled in Go to name
// a row the document declares. Without it a renamed or deleted matrix row leaves
// the corpus still claiming to cover it, and the coverage test above stays green
// because it only ever asks about rows the document still has.
func TestConformance_MatrixRowNamesResolve(t *testing.T) {
	t.Parallel()
	rows := readMatrixRows(t)
	declared := make([]string, 0, len(rows))
	for _, row := range rows {
		declared = append(declared, row.key)
	}
	require.NotEmpty(t, declared, "the matrix must declare rows to resolve against")

	for key, specs := range matrixRowWitnesses(t) {
		assert.Contains(t, declared, key,
			"corpus specs %v witness matrix row %q, which ir-spec-matrix.md does not declare", specs, key)
	}
	for key := range matrixRowsUncovered() {
		assert.Contains(t, declared, key,
			"matrixRowsUncovered names matrix row %q, which ir-spec-matrix.md does not declare", key)
	}
}

// matrixRowsUncovered names every OpenAPI-expressible matrix row the corpus does
// not witness, each with the reason it has none. Closing a gap means deleting
// its line here and naming the row from a case: a row that is both listed and
// witnessed fails, so the list cannot outlive the gap it describes.
func matrixRowsUncovered() map[string]string {
	return map[string]string{
		"open-enums": "OpenAPI has no open-enum keyword; the matrix's ⚠ is the " +
			"anyOf: [{enum: [...]}, {type: string}] idiom, which lowers as an ordinary union " +
			"and needs a spec pinning that the enum branch survives beside the open one",
		"long-running-operations": "OpenAPI states it only through vendor extensions, so a spec " +
			"for this row would assert what extensions-x already asserts — that x-* survives — " +
			"until the IR models polling as something a spec can read back",
		"idempotency": "OpenAPI conveys it through HTTP verb semantics alone, and the method " +
			"string http-binding pins is the whole of what a spec could read; there is no " +
			"idempotency declaration to capture until the IR infers one as policy",
	}
}

// matrixRowWitnesses inverts the corpus table into row key → the specs naming
// it, rejecting a case that names one row twice.
func matrixRowWitnesses(t *testing.T) map[string][]string {
	t.Helper()
	witnesses := map[string][]string{}
	for _, tc := range conformanceCases() {
		for i, key := range tc.rows {
			assert.NotContains(t, tc.rows[:i], key, "case %q names matrix row %q twice", tc.file, key)
			witnesses[key] = append(witnesses[key], tc.file)
		}
	}
	assert.NotEmpty(t, witnesses, "the corpus table must witness matrix rows")
	return witnesses
}

// openAPIExpressible reports whether the OpenAPI cell claims the capability is
// expressible, failing the test on a cell that opens with no legend marker.
func openAPIExpressible(t *testing.T, row matrixRow) bool {
	t.Helper()
	switch {
	case strings.HasPrefix(row.openAPI, "✅"), strings.HasPrefix(row.openAPI, "⚠"):
		return true
	case strings.HasPrefix(row.openAPI, "—"):
		return false
	default:
		t.Errorf("matrix row %q: OpenAPI cell %q opens with none of the legend markers ✅ ⚠ —",
			row.key, row.openAPI)
		return false
	}
}

// readMatrixRows parses the capability table into one matrixRow per data row.
func readMatrixRows(t *testing.T) []matrixRow {
	t.Helper()
	lines := matrixTableLines(t)
	header := splitMatrixCells(lines[0])
	require.Equal(t, "Key", header[0], "the capability table's first column is the row key")
	openAPIAt := slices.Index(header, matrixOpenAPIColumn)
	require.Positive(t, openAPIAt, "the capability table needs a %q column", matrixOpenAPIColumn)

	rows := make([]matrixRow, 0, len(lines))
	for _, line := range lines[2:] {
		cells := splitMatrixCells(line)
		require.Len(t, cells, len(header), "row %q has a different cell count than the header", line)
		rows = append(rows, matrixRow{
			key:        strings.Trim(cells[0], "`"),
			capability: cells[1],
			openAPI:    cells[openAPIAt],
		})
	}
	require.NotEmpty(t, rows, "the capability table must hold data rows")
	return rows
}

// matrixTableLines returns the keyed capability table: its header, its separator
// and every contiguous row under it.
//
// It requires the document to hold exactly one such table. The alternative — take
// the first — would let a second keyed table be added and read by nothing, which
// is the same class of silence this whole file exists to remove.
func matrixTableLines(t *testing.T) []string {
	t.Helper()
	data, err := os.ReadFile(matrixPath)
	require.NoError(t, err)

	var table []string
	inTable := false
	for _, line := range strings.Split(string(data), "\n") {
		switch {
		case strings.HasPrefix(line, matrixHeaderPrefix):
			require.Empty(t, table, "ir-spec-matrix.md holds more than one keyed capability table")
			inTable = true
		case !strings.HasPrefix(line, "|"):
			inTable = false
		}
		if inTable {
			table = append(table, line)
		}
	}
	require.Greater(t, len(table), 2, "the keyed table needs a header, a separator and rows")
	require.True(t, strings.HasPrefix(table[1], "|---"), "the header is followed by its separator")
	return table
}

// splitMatrixCells splits one markdown table row into trimmed cells, honouring
// the \| escape a cell uses for a literal pipe — the Erlang union spelling has
// one, and splitting naively would give that row an extra cell.
func splitMatrixCells(line string) []string {
	body := strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(line), "|"), "|")
	var cells []string
	var cell strings.Builder
	escaped := false
	for _, r := range body {
		switch {
		case escaped:
			cell.WriteRune(r)
			escaped = false
		case r == '\\':
			escaped = true
		case r == '|':
			cells = append(cells, strings.TrimSpace(cell.String()))
			cell.Reset()
		default:
			cell.WriteRune(r)
		}
	}
	return append(cells, strings.TrimSpace(cell.String()))
}
