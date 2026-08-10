// This file is a package-level suite, not a per-source-file test: it reads
// docs/ir-spec-matrix.md and measures the whole committed corpus against it, so
// it pairs with no single source file.
//
// The table reader below is format-agnostic and lives in one compiler's test
// package, which is the wrong altitude for it the moment a second compiler needs
// a second column. It is here rather than in internal/testspec because
// compilers/openapi is not allowed to reach outside the pipeline — archtest's
// allowlist for it is ir, compilers, compilers/compile and its own internal/*
// — and archtest skips _test.go files, so importing testspec from here would
// pass by way of a blind spot rather than by the rule. Whoever adds the Swagger
// column moves it to a home both compilers can reach, and will have two callers
// to shape the API with.
package openapi_test // external test package — exercises only the public API

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// matrixPath is the capability matrix, addressed relative to this test file.
const matrixPath = "../../docs/ir-spec-matrix.md"

// matrixRowsGolden snapshots each row key beside the capability it labels.
const matrixRowsGolden = "testdata/matrix-rows.golden.txt"

// matrixKeyColumn and matrixCapabilityColumn are the two fixed columns every
// keyed capability table opens with; the rest are one format each.
const (
	matrixKeyColumn        = "Key"
	matrixCapabilityColumn = "Capability"
)

// matrixOpenAPIColumn is the header of the column this compiler answers to.
const matrixOpenAPIColumn = "OpenAPI 3.x"

// matrixKeyPattern is the shape a row key must have: a lowercase slug. Keys are
// spelled in Go string literals and in a markdown table, so anything looser
// makes two spellings of one row possible.
var matrixKeyPattern = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// matrixCell is one format's answer for a capability row.
type matrixCell struct {
	column string
	cell   string
}

// matrixRow is one capability row: its stable key, the capability it names, and
// every format's cell in document order.
type matrixRow struct {
	key        string
	capability string
	formats    []matrixCell
}

// openAPI returns the cell of the column this compiler answers to.
func (r matrixRow) openAPI() string {
	for _, f := range r.formats {
		if f.column == matrixOpenAPIColumn {
			return f.cell
		}
	}
	return ""
}

// TestMatrix_RowsCarryUniqueSlugKeys pins the half of the contract that lives in
// the document: every row has a key, the keys are unique, and every format cell
// opens with one of the legend's three markers.
//
// The marker check is what makes the coverage test below trustworthy. An
// unmarked cell is neither expressible nor absent, so a row whose marker was
// lost in an edit would drop out of the corpus contract without failing
// anything — the silent direction of the failure, which is why it is rejected
// here rather than defaulted.
//
// It sweeps every format column, not only the one this compiler answers to. The
// legend is the document's, the next compiler reads the next column, and a
// contract enforced for one column of nine says nothing about the eight a
// reviewer would assume it covered.
func TestMatrix_RowsCarryUniqueSlugKeys(t *testing.T) {
	t.Parallel()
	rows := readMatrixRows(t)
	seen := make(map[string]string, len(rows))
	for _, row := range rows {
		assert.Regexp(t, matrixKeyPattern, row.key, "row %q needs a lowercase-slug key", row.capability)
		assert.NotContains(t, seen, row.key,
			"rows %q and %q share the key %q", seen[row.key], row.capability, row.key)
		seen[row.key] = row.capability
		for _, f := range row.formats {
			if _, known := legendMarker(f.cell); !known {
				t.Errorf("matrix row %q: %s cell %q opens with none of the legend markers ✅ ⚠ —",
					row.key, f.column, f.cell)
			}
		}
	}
}

// TestMatrix_KeysStayPinnedToTheirCapabilities snapshots every row as
// "key<tab>capability". Regenerate with
// `go test ./compilers/openapi -run TestMatrix -update`.
//
// Nothing else binds a key to the row it labels. Every other test here asks only
// whether a key is *declared*, so an insert or delete that shifts the Key column
// against the Capability column by one leaves them all green while each corpus
// spec silently witnesses its neighbour's capability.
//
// It is also what makes the document's own promise — keys are append-only, and
// renaming one fails a test — true for rows no Go file names. Those are exactly
// the rows the next compiler will be first to bind to, and without a snapshot
// every one of them is freely renameable today.
func TestMatrix_KeysStayPinnedToTheirCapabilities(t *testing.T) {
	t.Parallel()
	var b strings.Builder
	for _, row := range readMatrixRows(t) {
		b.WriteString(row.key + "\t" + row.capability + "\n")
	}
	compareTextGolden(t, matrixRowsGolden, b.String())
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
// What it cannot check is that a spec naming a row exercises that capability;
// that claim is read by a reviewer, and docs/architecture.md says so rather than
// promising otherwise.
//
// A row OpenAPI cannot express must have neither a witness nor an excuse. Both
// directions catch the matrix and the corpus disagreeing: a witness means one of
// the two is wrong about the source format, and an excuse describes a gap that
// cannot exist, which nothing else would ever retire.
func TestConformance_EveryExpressibleMatrixRowIsWitnessed(t *testing.T) {
	t.Parallel()
	witnesses := matrixRowWitnesses(t)
	uncovered := matrixRowsUncovered()
	for _, row := range readMatrixRows(t) {
		expressible, known := legendMarker(row.openAPI())
		if !known {
			continue // TestMatrix_RowsCarryUniqueSlugKeys reports the unmarked cell.
		}
		if !expressible {
			assert.NotContains(t, witnesses, row.key,
				"matrix row %q is marked absent for OpenAPI, yet %v witness it",
				row.key, witnesses[row.key])
			assert.NotContains(t, uncovered, row.key,
				"matrix row %q is marked absent for OpenAPI, so matrixRowsUncovered's line for it "+
					"describes a gap no spec could ever close; delete it", row.key)
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
//
// Each reason names the blocker as it stands today. Two of them used to point at
// an IR that could not hold the capability yet, which had stopped being true —
// ir.LongRunning and ir.Idempotency both model theirs — and nothing here can
// tell a stale reason from a live one, so a reader chasing one is sent to wait
// on a change that already landed.
func matrixRowsUncovered() map[string]string {
	return map[string]string{
		"open-enums": "OpenAPI has no open-enum keyword; the matrix's ⚠ is the " +
			"anyOf: [{enum: [...]}, {type: string}] idiom, which lowers as an ordinary union " +
			"and needs a spec pinning that the enum branch survives beside the open one",
		"pagination": "OpenAPI states it only through links and x-*, and this compiler keeps both " +
			"verbatim rather than reading either into ir.Pagination — response-links pins that they " +
			"survive, so a spec for this row would assert nothing further until a policy pass infers " +
			"pagination, which invariant 6 puts outside the compiler",
		"long-running-operations": "OpenAPI states it only through vendor extensions, so a spec for " +
			"this row would assert what extensions-x already asserts — that x-* survives. ir.LongRunning " +
			"models the capability; what is missing is a policy pass reading those extensions into it, " +
			"which invariant 6 puts outside the compiler",
		"idempotency": "OpenAPI conveys it through HTTP verb semantics alone, and the method string " +
			"http-binding pins is the whole of what a spec could read; ir.Idempotency models the " +
			"capability, but nothing in the document declares it for a compiler to capture",
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

// legendMarker classifies one matrix cell by the legend marker it opens with:
// expressible says whether the format can state the capability, known whether a
// marker was found at all.
//
// The two are separate answers because an unmarked cell is not an absent one.
// Folding them into one bool made a lost marker report "marked absent for
// OpenAPI, yet these specs witness it" — a confident, false claim about the
// document, printed over the real cause.
func legendMarker(cell string) (expressible, known bool) {
	switch {
	case strings.HasPrefix(cell, "✅"), strings.HasPrefix(cell, "⚠"):
		return true, true
	case strings.HasPrefix(cell, "—"):
		return false, true
	default:
		return false, false
	}
}

// readMatrixRows parses the capability table into one matrixRow per data row.
func readMatrixRows(t *testing.T) []matrixRow {
	t.Helper()
	lines := matrixTableLines(t)
	header := splitMatrixCells(lines[0])
	require.Equal(t, matrixKeyColumn, header[0], "the capability table's first column is the row key")
	require.Equal(t, matrixCapabilityColumn, header[1], "and its second names the capability")
	require.Contains(t, header, matrixOpenAPIColumn, "the capability table needs a %q column", matrixOpenAPIColumn)

	rows := make([]matrixRow, 0, len(lines))
	for _, line := range lines[2:] {
		cells := splitMatrixCells(line)
		require.Len(t, cells, len(header), "row %q has a different cell count than the header", line)
		formats := make([]matrixCell, 0, len(header)-2)
		for i, column := range header[2:] {
			formats = append(formats, matrixCell{column: column, cell: cells[i+2]})
		}
		rows = append(rows, matrixRow{
			key:        strings.Trim(cells[0], "`"),
			capability: cells[1],
			formats:    formats,
		})
	}
	require.NotEmpty(t, rows, "the capability table must hold data rows")
	return rows
}

// matrixTableLines returns the keyed capability table: its header, its separator
// and every contiguous row under it.
//
// It requires the document to hold exactly one such table, and to hold no
// table-shaped line outside it. That second requirement is what makes the first
// mean anything: the walk ends the table at the first line that is not a row, so
// a blank line, an HTML comment or a GFM-legal indent between two rows would
// otherwise drop that row and every row below it out of every check in this
// file — silently, since the rows at the end of the table are the ones no spec
// names.
func matrixTableLines(t *testing.T) []string {
	t.Helper()
	data, err := os.ReadFile(matrixPath)
	require.NoError(t, err)

	var table []string
	tableShaped := 0
	inTable := false
	for _, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(raw)
		isRow := strings.HasPrefix(line, "|")
		if isRow {
			tableShaped++
		}
		switch {
		case isRow && isMatrixHeader(line):
			require.Empty(t, table, "ir-spec-matrix.md holds more than one keyed capability table")
			inTable = true
		case !isRow:
			inTable = false
		}
		if inTable {
			table = append(table, line)
		}
	}

	require.Greater(t, len(table), 2, "the keyed table needs a header, a separator and rows")
	require.True(t, strings.HasPrefix(strings.ReplaceAll(table[1], " ", ""), "|---"),
		"the header is followed by its separator")
	require.Len(t, table, tableShaped,
		"ir-spec-matrix.md holds %d table-shaped lines and the keyed capability table reaches %d of "+
			"them: a blank line, a comment or an indented row between two rows ends the table early, "+
			"dropping every row below it from this file's checks", tableShaped, len(table))
	return table
}

// isMatrixHeader reports whether a table line opens the keyed capability table,
// deciding on parsed cells rather than raw text.
//
// A header padded for column alignment and one written without padding are the
// same header to every markdown reader. Matching raw text recognizes only one of
// the two: it lets a second keyed table evade the "exactly one" guard by leaving
// the padding out, and it loses the real table the moment a formatter adds it.
func isMatrixHeader(line string) bool {
	cells := splitMatrixCells(line)
	return len(cells) > 2 && cells[0] == matrixKeyColumn && cells[1] == matrixCapabilityColumn
}

// splitMatrixCells splits one markdown table row into trimmed cells, honouring
// the \| escape a cell uses for a literal pipe — the Erlang union spelling has
// one, and splitting naively would give that row an extra cell.
//
// Only \| is an escape. Consuming every backslash would silently rewrite any
// cell holding one for its own sake, which is a thing prose does.
func splitMatrixCells(line string) []string {
	body := strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(line), "|"), "|")
	runes := []rune(body)
	var cells []string
	var cell strings.Builder
	for i := 0; i < len(runes); i++ {
		switch {
		case runes[i] == '\\' && i+1 < len(runes) && runes[i+1] == '|':
			cell.WriteRune('|')
			i++
		case runes[i] == '|':
			cells = append(cells, strings.TrimSpace(cell.String()))
			cell.Reset()
		default:
			cell.WriteRune(runes[i])
		}
	}
	return append(cells, strings.TrimSpace(cell.String()))
}
