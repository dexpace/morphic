package report_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/engine/report"
	"github.com/dexpace/morphic/ir"
)

// TestWriteDiagnostics_WithAndWithoutSourcePath is the table over every shape a
// location takes: each of the three locators alone, the precedence between
// them, a source with none of them, and a source index past the end of the
// table, a producer bug, which is written bare rather than guessed at. Whole
// lines are asserted: the location is what varies between rows, and a
// containment check on the message would pass for a line that rendered the
// location wrongly or invented one.
func TestWriteDiagnostics_WithAndWithoutSourcePath(t *testing.T) {
	t.Parallel()
	sources := []ir.SourceInfo{{Path: "spec.yaml"}}
	tests := []struct {
		name string
		prov ir.Provenance
		want string
	}{
		{
			name: "pointer",
			prov: ir.Provenance{Source: 0, Pointer: "/paths/~1x"},
			want: "error openapi/bad spec.yaml#/paths/~1x: m\n",
		},
		{
			name: "root: a source with no locator prints the bare path, not a trailing #",
			prov: ir.Provenance{Source: 0},
			want: "error openapi/bad spec.yaml: m\n",
		},
		{
			name: "position",
			prov: ir.Provenance{Source: 0, Position: ir.Position{Line: 5, Column: 1}},
			want: "error openapi/bad spec.yaml:5:1: m\n",
		},
		{
			name: "the first line is a position like any other",
			prov: ir.Provenance{Source: 0, Position: ir.Position{Line: 1, Column: 1}},
			want: "error openapi/bad spec.yaml:1:1: m\n",
		},
		{
			name: "line only: the producer knows no column",
			prov: ir.Provenance{Source: 0, Position: ir.Position{Line: 5}},
			want: "error openapi/bad spec.yaml:5: m\n",
		},
		{
			name: "pointer and position together: the link leads and the pointer follows",
			prov: ir.Provenance{Source: 0, Pointer: "/paths/~1x", Position: ir.Position{Line: 5, Column: 1}},
			want: "error openapi/bad spec.yaml:5:1 (#/paths/~1x): m\n",
		},
		{
			name: "pointer and the first line",
			prov: ir.Provenance{Source: 0, Pointer: "/paths/~1x", Position: ir.Position{Line: 1, Column: 1}},
			want: "error openapi/bad spec.yaml:1:1 (#/paths/~1x): m\n",
		},
		{
			name: "pointer and a line alone",
			prov: ir.Provenance{Source: 0, Pointer: "/paths/~1x", Position: ir.Position{Line: 5}},
			want: "error openapi/bad spec.yaml:5 (#/paths/~1x): m\n",
		},
		{
			name: "a node beside a source: the file is printed, not the node",
			prov: ir.Provenance{Source: 0, Node: "op/x"},
			want: "error openapi/bad spec.yaml: m\n",
		},
		{
			name: "NoSource with a node prints the node bare",
			prov: ir.Provenance{Source: ir.NoSource, Node: "op/x"},
			want: "error openapi/bad op/x: m\n",
		},
		{
			name: "an unresolvable source with a position prints the position bare",
			prov: ir.Provenance{Source: 99, Position: ir.Position{Line: 9, Column: 7}},
			want: "error openapi/bad 9:7: m\n",
		},
		{
			name: "an unresolvable source with a pointer prints the pointer bare",
			prov: ir.Provenance{Source: 99, Pointer: "/paths/~1x"},
			want: "error openapi/bad /paths/~1x: m\n",
		},
		{
			name: "an unresolvable source with both prints them bare",
			prov: ir.Provenance{Source: 99, Pointer: "/paths/~1x", Position: ir.Position{Line: 9, Column: 7}},
			want: "error openapi/bad 9:7 (#/paths/~1x): m\n",
		},
		{
			name: "nothing at all prints no location",
			prov: ir.Provenance{Source: ir.NoSource},
			want: "error openapi/bad: m\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var w strings.Builder

			err := report.WriteDiagnostics(&w, []ir.Diagnostic{diag("openapi/bad", tt.prov)}, sources)

			require.NoError(t, err)
			assert.Equal(t, tt.want, w.String())
		})
	}
}

// TestWriteDiagnostics_ResolvesTheSourceIndex pins how a diagnostic's source
// index finds its path: each index names its own entry, and an absent table, a
// negative index, one at the table's length and one past it all print the
// pointer without a path. An index at the length is the boundary a guard of
// "past the end" gets wrong by one, and would read beyond the table.
func TestWriteDiagnostics_ResolvesTheSourceIndex(t *testing.T) {
	t.Parallel()
	table := []ir.SourceInfo{{Path: "a.yaml"}, {Path: "b.yaml"}}
	tests := []struct {
		name    string
		sources []ir.SourceInfo
		source  int
		want    string
	}{
		{"no table", nil, 0, "error c /p: m\n"},
		{"NoSource", table, ir.NoSource, "error c /p: m\n"},
		{"a negative index below NoSource", table, -2, "error c /p: m\n"},
		{"the first entry", table, 0, "error c a.yaml#/p: m\n"},
		{"the last entry", table, 1, "error c b.yaml#/p: m\n"},
		{"an index at the table's length", table, 2, "error c /p: m\n"},
		{"an index past the end", table, 7, "error c /p: m\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var w strings.Builder

			err := report.WriteDiagnostics(&w, []ir.Diagnostic{diag("c", ir.Provenance{Source: tt.source, Pointer: "/p"})}, tt.sources)

			require.NoError(t, err)
			assert.Equal(t, tt.want, w.String())
		})
	}
}

// TestWriteDiagnostics_WritesOneLinePerDiagnosticInOrder pins the line's fields
// and their order, severity then code then location then message, over
// diagnostics of different severities. A message holding format verbs is
// written as it stands, never read as a format.
func TestWriteDiagnostics_WritesOneLinePerDiagnosticInOrder(t *testing.T) {
	t.Parallel()
	sources := []ir.SourceInfo{{Path: "spec.yaml"}}
	diags := []ir.Diagnostic{
		{Severity: ir.SeverityWarning, Code: "openapi/w", Message: "uses 100%s of %d",
			Provenance: ir.Provenance{Source: 0, Pointer: "/a"}},
		{Severity: ir.SeverityInfo, Code: "ir/i", Message: "note",
			Provenance: ir.Provenance{Source: ir.NoSource}},
		{Severity: ir.SeverityError, Code: "openapi/e", Message: "broken: really",
			Provenance: ir.Provenance{Source: 0, Position: ir.Position{Line: 2, Column: 3}}},
	}
	var w strings.Builder

	err := report.WriteDiagnostics(&w, diags, sources)

	require.NoError(t, err)
	assert.Equal(t, "warning openapi/w spec.yaml#/a: uses 100%s of %d\n"+
		"info ir/i: note\n"+
		"error openapi/e spec.yaml:2:3: broken: really\n", w.String())
}

// TestWriteDiagnostics_StopsAtFirstFailingWrite pins that a destination which
// refuses a line ends the report: exactly that many writes are attempted, none
// after the failure, the lines before it are whole, and the error returned is
// the destination's own. Lines written after a failure would bury it.
func TestWriteDiagnostics_StopsAtFirstFailingWrite(t *testing.T) {
	t.Parallel()
	diags := []ir.Diagnostic{
		diag("c/1", ir.Provenance{Source: ir.NoSource}),
		diag("c/2", ir.Provenance{Source: ir.NoSource}),
		diag("c/3", ir.Provenance{Source: ir.NoSource}),
		diag("c/4", ir.Provenance{Source: ir.NoSource}),
	}
	lines := []string{"error c/1: m\n", "error c/2: m\n", "error c/3: m\n", "error c/4: m\n"}
	for failAt := 1; failAt <= len(diags); failAt++ {
		t.Run(fmt.Sprintf("write %d fails", failAt), func(t *testing.T) {
			t.Parallel()
			w := &failingWriter{failAt: failAt}

			err := report.WriteDiagnostics(w, diags, nil)

			require.ErrorIs(t, err, errDisk)
			assert.Equal(t, failAt, w.attempts, "no write may follow the failing one")
			assert.Equal(t, strings.Join(lines[:failAt-1], ""), w.text.String())
		})
	}
}

// TestWriteDiagnostics_NothingToWrite pins that no diagnostics means no write
// at all, not an empty one, and no error: a destination that would refuse
// everything is never asked.
func TestWriteDiagnostics_NothingToWrite(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		diags []ir.Diagnostic
	}{
		{"nil", nil},
		{"empty", []ir.Diagnostic{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			w := &failingWriter{failAt: 1}

			err := report.WriteDiagnostics(w, tt.diags, nil)

			require.NoError(t, err)
			assert.Zero(t, w.attempts)
		})
	}
}
