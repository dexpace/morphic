// This file is a package-level suite, not a per-source-file test: it reads
// docs/ir-spec-matrix.md and holds the document itself to the shape the
// corpus contract reads, a unique slug key on every row and every format column
// and a legend marker in every cell.
//
// Nothing here names a format. The reader serves every column, so a compiler
// added to engine.New is held to its own by data alone (registered_test.go).
package conformance_test // external test package — exercises only the public API

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/internal/keydoc"
	"github.com/dexpace/morphic/ir/irtest"
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

// matrixKeyPattern is the shape a row key or a format key must have: a
// lowercase slug. Keys are spelled in Go string literals and in a markdown
// table, so anything looser makes two spellings of one row possible.
var matrixKeyPattern = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// matrixColumn is one format's column, held as its header was written: a title
// for a reader, then the key a compiler registers the format under, in code.
type matrixColumn struct {
	header string
}

// keys returns the code spans the header carries. A well-formed header carries
// exactly one; keeping them all is what lets a header with none or two be
// reported rather than guessed at.
func (c matrixColumn) keys() []string { return keydoc.Spans(c.header) }

// matrixRow is one capability row: its stable key, the capability it names and
// one cell per format column, in column order.
type matrixRow struct {
	key        string
	capability string
	cells      []string
}

// matrix is the capability table: its format columns and its rows.
type matrix struct {
	columns []matrixColumn
	rows    []matrixRow
}

// columnsKeyed returns the index of every column whose header carries name as
// its one key.
func (m matrix) columnsKeyed(name string) []int {
	var at []int
	for i, c := range m.columns {
		if keys := c.keys(); len(keys) == 1 && keys[0] == name {
			at = append(at, i)
		}
	}
	return at
}

// TestMatrix_RowsCarryUniqueSlugKeys pins the half of the contract that lives in
// the document: every row has a key, the keys are unique, and every format cell
// opens with one of the legend's three markers.
//
// An unmarked cell is neither expressible nor absent, so a row whose marker was
// lost in an edit would silently drop out of the corpus contract; it is
// rejected here rather than defaulted. The check covers every format column,
// not only those with a compiler: the legend is the document's, and the next
// compiler reads the next column.
func TestMatrix_RowsCarryUniqueSlugKeys(t *testing.T) {
	t.Parallel()
	m := readMatrix(t)
	seen := make(map[string]string, len(m.rows))
	for _, row := range m.rows {
		assert.Regexp(t, matrixKeyPattern, row.key, "row %q needs a lowercase-slug key", row.capability)
		assert.NotContains(t, seen, row.key,
			"rows %q and %q share the key %q", seen[row.key], row.capability, row.key)
		seen[row.key] = row.capability
		for i, cell := range row.cells {
			if _, known := legendMarker(cell); !known {
				t.Errorf("matrix row %q: %s cell %q opens with none of the legend markers ✅ ⚠ —",
					row.key, m.columns[i].header, cell)
			}
		}
	}
}

// TestMatrix_FormatColumnsCarryUniqueKeys holds the other axis to the same
// standard: every format column's header ends in one key, a lowercase slug no
// other column uses.
//
// The key is how a registered compiler finds its column. A header without one
// leaves that format held to nothing, and a key two columns share would let a
// compiler be held to a neighbour's.
func TestMatrix_FormatColumnsCarryUniqueKeys(t *testing.T) {
	t.Parallel()
	for _, p := range columnKeyProblems(readMatrix(t).columns) {
		t.Error(p)
	}
}

// columnKeyProblems reports each way columns fail to be keyed: a header with
// no code span or several, a key that is no slug, and a key used twice.
func columnKeyProblems(columns []matrixColumn) []string {
	var problems []string
	seen := make(map[string]string, len(columns))
	for _, c := range columns {
		keys := c.keys()
		if len(keys) != 1 {
			problems = append(problems, fmt.Sprintf(
				"format column %q carries %d code spans, and needs exactly one: the name its compiler "+
					"registers the format under, after the title", c.header, len(keys)))
			continue
		}
		if !matrixKeyPattern.MatchString(keys[0]) {
			problems = append(problems, fmt.Sprintf("format column %q is keyed %q, which is no lowercase slug",
				c.header, keys[0]))
		}
		if first, dup := seen[keys[0]]; dup {
			problems = append(problems, fmt.Sprintf("format columns %q and %q share the key %q",
				first, c.header, keys[0]))
		}
		seen[keys[0]] = c.header
	}
	return problems
}

// TestMatrix_KeysStayPinnedToTheirCapabilities snapshots every row as
// "key<tab>capability". Regenerate with
// `go test ./internal/conformance -run TestMatrix -update`.
//
// Nothing else binds a key to the row it labels: an insert or delete that
// shifts the Key column against the Capability column leaves every other test
// green while each corpus spec witnesses its neighbour's capability. The
// snapshot also makes the document's promise that keys are append-only, and
// that renaming one fails a test, true for rows no Go file names.
func TestMatrix_KeysStayPinnedToTheirCapabilities(t *testing.T) {
	t.Parallel()
	var b strings.Builder
	for _, row := range readMatrix(t).rows {
		b.WriteString(row.key + "\t" + row.capability + "\n")
	}
	compareTextGolden(t, matrixRowsGolden, b.String())
}

// legendMarker classifies one matrix cell by the legend marker it opens with:
// expressible says whether the format can state the capability, known whether a
// marker was found at all.
//
// The two are separate answers because an unmarked cell is not an absent one.
// Folding them into one bool made a lost marker report "marked absent, yet
// these specs witness it" — a confident, false claim about the document,
// printed over the real cause.
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

// readMatrix parses the capability table into its format columns and one
// matrixRow per data row.
func readMatrix(t *testing.T) matrix {
	t.Helper()
	lines := matrixTableLines(t)
	header := splitMatrixCells(lines[0])
	require.Greater(t, len(header), 2, "the capability table needs a format column")
	require.Equal(t, matrixKeyColumn, header[0], "the capability table's first column is the row key")
	require.Equal(t, matrixCapabilityColumn, header[1], "and its second names the capability")

	m := matrix{columns: make([]matrixColumn, 0, len(header)-2)}
	for _, h := range header[2:] {
		m.columns = append(m.columns, matrixColumn{header: h})
	}
	for _, line := range lines[2:] {
		cells := splitMatrixCells(line)
		require.Len(t, cells, len(header), "row %q has a different cell count than the header", line)
		m.rows = append(m.rows, matrixRow{
			key:        strings.Trim(cells[0], "`"),
			capability: cells[1],
			cells:      cells[2:],
		})
	}
	require.NotEmpty(t, m.rows, "the capability table must hold data rows")
	return m
}

// matrixTableLines returns the keyed capability table: its header, its
// delimiter row and every row under it, each trimmed.
//
// It finds the table by its delimiter row, not a leading pipe: GFM does not
// require one, and a reader keyed on "|" misses a table or last row written
// without outer pipes. Prose has no delimiter row, so counting them makes
// "exactly one table" true however a second is spelled. Fenced blocks are
// skipped.
//
// A line outside the table is reported only when it splits into as many cells
// as the header, as a fallen-out row does and prose naming `|` does not.
func matrixTableLines(t *testing.T) []string {
	t.Helper()
	data, err := os.ReadFile(matrixPath)
	require.NoError(t, err)
	lines := matrixUnfencedLines(string(data))

	var delimiters []int
	for i, line := range lines {
		if isMatrixDelimiter(line) {
			delimiters = append(delimiters, i)
		}
	}
	require.Len(t, delimiters, 1,
		"ir-spec-matrix.md must hold exactly one markdown table outside its fenced blocks, and "+
			"holds %d", len(delimiters))
	separator := delimiters[0]
	require.Positive(t, separator, "the delimiter row needs a header line above it")

	end := separator + 1
	for end < len(lines) && isMatrixRow(lines[end]) {
		end++
	}
	width := len(splitMatrixCells(lines[separator-1]))
	for i, line := range lines {
		if isMatrixRow(line) && (i < separator-1 || i >= end) && len(splitMatrixCells(line)) == width {
			t.Errorf("ir-spec-matrix.md line %d is table-shaped but falls outside the capability "+
				"table, which this file reads from line %d to line %d: %q",
				i+1, separator, end, line)
		}
	}

	table := lines[separator-1 : end]
	require.Greater(t, len(table), 2, "the keyed table needs a header, a delimiter row and rows")
	return table
}

// matrixUnfencedLines returns the document's lines, trimmed, with the contents
// of fenced code blocks blanked out so a markdown table written as an example is
// not read as one. Both fence spellings count: CommonMark gives ~~~ and ``` the
// same meaning, and honouring one of them would tolerate an example written one
// way and fail on the same example written the other.
//
// Blanking rather than dropping keeps the index of each line equal to its
// position in the file, which the messages above report.
func matrixUnfencedLines(data string) []string {
	raw := strings.Split(data, "\n")
	out := make([]string, len(raw))
	fenced := false
	for i, line := range raw {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			fenced = !fenced
			continue
		}
		if !fenced {
			out[i] = trimmed
		}
	}
	return out
}

// isMatrixRow reports whether a line could be a row of a markdown table. GFM
// requires a pipe somewhere and requires one at neither end.
func isMatrixRow(line string) bool {
	return strings.Contains(line, "|")
}

// isMatrixDelimiter reports whether a line is a table's delimiter row — the
// ---|--- under a header. It is the one line every markdown table must have and
// that prose does not write by accident, which is what makes it the thing to
// count when asking how many tables a document holds.
func isMatrixDelimiter(line string) bool {
	if !strings.Contains(line, "|") || !strings.Contains(line, "-") {
		return false
	}
	return strings.TrimLeft(line, "|-: ") == ""
}

// splitMatrixCells splits one markdown table row into trimmed cells, honouring
// the \| escape a cell uses for a literal pipe — the Erlang union spelling has
// one, and splitting naively would give that row an extra cell.
//
// Only \| and \\ are escapes, which is GFM's rule. Consuming every backslash
// would silently rewrite a cell holding one for its own sake; consuming only
// \| would read the pipe in \\| as escaped when the backslash before it is
// what was escaped, merging two cells into one.
func splitMatrixCells(line string) []string {
	body := strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(line), "|"), "|")
	runes := []rune(body)
	var cells []string
	var cell strings.Builder
	for i := 0; i < len(runes); i++ {
		switch {
		case runes[i] == '\\' && i+1 < len(runes) && (runes[i+1] == '|' || runes[i+1] == '\\'):
			cell.WriteRune(runes[i+1])
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

// compareTextGolden compares got against the golden file at path, or rewrites it
// when -update is set — irtest.CompareGolden's contract for a non-document
// artifact, sharing its flag so one -update run refreshes every golden.
func compareTextGolden(t *testing.T, path, got string) {
	t.Helper()
	if irtest.Update() {
		require.NoError(t, os.WriteFile(path, []byte(got), 0o644))
		return
	}
	want, err := os.ReadFile(path)
	require.NoError(t, err, "read golden %s (run with -update to create)", path)
	if diff := cmp.Diff(string(want), got); diff != "" {
		t.Errorf("golden mismatch for %s (-golden +got):\n%s", path, diff)
	}
}
