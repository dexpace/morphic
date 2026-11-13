// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package report_test

import (
	"encoding/json/jsontext"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/engine/report"
	"github.com/dexpace/morphic/engine/triage"
	"github.com/dexpace/morphic/ir"
)

// TestWriteText_WithAndWithoutSourcePath is the table over every shape a
// location takes: each of the three locators alone, the precedence between
// them, a source with none of them, and a source index past the end of the
// table, a producer bug, which is written bare rather than guessed at. Whole
// lines are asserted: the location is what varies between rows, and a
// containment check on the message would pass for a line that rendered the
// location wrongly or invented one.
func TestWriteText_WithAndWithoutSourcePath(t *testing.T) {
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

			err := writeDiagnostics(&w, []ir.Diagnostic{diag("openapi/bad", tt.prov)}, sources)

			require.NoError(t, err)
			assert.Equal(t, tt.want, w.String())
		})
	}
}

// TestWriteText_ResolvesTheSourceIndex pins how a diagnostic's source
// index finds its path: each index names its own entry, and an absent table, a
// negative index, one at the table's length and one past it all print the
// pointer without a path. An index at the length is the boundary a guard of
// "past the end" gets wrong by one, and would read beyond the table.
func TestWriteText_ResolvesTheSourceIndex(t *testing.T) {
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

			err := writeDiagnostics(&w, []ir.Diagnostic{diag("c", ir.Provenance{Source: tt.source, Pointer: "/p"})}, tt.sources)

			require.NoError(t, err)
			assert.Equal(t, tt.want, w.String())
		})
	}
}

// TestWriteText_NamesTheWriterBesideTheSource pins how a construct an
// overlay wrote into the source is located: at the source's own path and
// pointer, with the overlay named as its writer, and no writer named for one
// the source wrote, nor for a writer index the table does not hold.
func TestWriteText_NamesTheWriterBesideTheSource(t *testing.T) {
	t.Parallel()
	table := []ir.SourceInfo{{Path: "root.yaml"}, {Path: "patch.yaml", Format: "overlay@1.0.0"}}
	for _, tt := range []struct {
		name string
		prov ir.Provenance
		want string
	}{
		{"written by the overlay", ir.Provenance{Source: 0, Pointer: "/p", WrittenBy: 1},
			"error c root.yaml#/p (written by patch.yaml): m\n"},
		{"written by the source", ir.Provenance{Source: 0, Pointer: "/p"}, "error c root.yaml#/p: m\n"},
		{"about the overlay itself", ir.Provenance{Source: 1, Pointer: "/actions/0"}, "error c patch.yaml#/actions/0: m\n"},
		{"a writer the table lacks", ir.Provenance{Source: 0, Pointer: "/p", WrittenBy: 5}, "error c root.yaml#/p: m\n"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var w strings.Builder
			require.NoError(t, writeDiagnostics(&w, []ir.Diagnostic{diag("c", tt.prov)}, table))
			assert.Equal(t, tt.want, w.String())
		})
	}
}

// TestWriteText_WritesOneLinePerDiagnosticInOrder pins the line's fields
// and their order, severity then code then location then message, over
// diagnostics of different severities. A message holding format verbs is
// written as it stands, never read as a format.
func TestWriteText_WritesOneLinePerDiagnosticInOrder(t *testing.T) {
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

	err := writeDiagnostics(&w, diags, sources)

	require.NoError(t, err)
	assert.Equal(t, "warning openapi/w spec.yaml#/a: uses 100%s of %d\n"+
		"info ir/i: note\n"+
		"error openapi/e spec.yaml:2:3: broken: really\n", w.String())
}

// TestWriteText_StopsAtFirstFailingWrite pins that a destination which
// refuses a line ends the report: exactly that many writes are attempted, none
// after the failure, the lines before it are whole, and the error returned is
// the destination's own. Lines written after a failure would bury it.
func TestWriteText_StopsAtFirstFailingWrite(t *testing.T) {
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

			err := writeDiagnostics(w, diags, nil)

			require.ErrorIs(t, err, errDisk)
			assert.Equal(t, failAt, w.attempts, "no write may follow the failing one")
			assert.Equal(t, strings.Join(lines[:failAt-1], ""), w.text.String())
		})
	}
}

// TestWriteText_NothingToWrite pins that no diagnostics means no write
// at all, not an empty one, and no error: a destination that would refuse
// everything is never asked.
func TestWriteText_NothingToWrite(t *testing.T) {
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

			err := writeDiagnostics(w, tt.diags, nil)

			require.NoError(t, err)
			assert.Zero(t, w.attempts)
		})
	}
}

// TestRenderers_RefuseNoWriter pins that every renderer refuses a nil writer
// with an error rather than a panic, whether or not they have a line to write,
// so a refusal does not depend on what the run reported.
func TestRenderers_RefuseNoWriter(t *testing.T) {
	t.Parallel()
	full, _ := fullExplanation()
	tests := []struct {
		name   string
		render func() error
	}{
		{"diagnostics", func() error {
			return writeDiagnostics(nil, []ir.Diagnostic{diag("c", ir.Provenance{})}, nil)
		}},
		{"no diagnostics", func() error { return writeDiagnostics(nil, nil, nil) }},
		{"a report", func() error { return report.WriteJSON(nil, report.Report{}) }},
		{"an explanation", func() error { return report.WriteExplanation(nil, full) }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.EqualError(t, tt.render(), "report: no writer to write to")
		})
	}
}

// finding is a finding of code at sev, at pointer in the spec, no rule met.
func finding(sev ir.Severity, code, pointer string) triage.Finding {
	return triage.Finding{Severity: sev, Diagnostic: ir.Diagnostic{Severity: sev, Code: code, Message: "m",
		Provenance: ir.Provenance{Pointer: jsontext.Pointer(pointer)}}}
}

// TestWriteText_WithholdsPastTheCountOfACode pins PerCode: the first findings
// of each code and rating, in reported order, one per line, the rest counted
// by a line of their own once every finding is written, and a tally of all of
// them. A code at two ratings is two groups, each held to the count alone.
func TestWriteText_WithholdsPastTheCountOfACode(t *testing.T) {
	t.Parallel()
	r := report.Report{Sources: []ir.SourceInfo{{Path: "s.yaml"}}, Findings: []triage.Finding{
		finding(ir.SeverityInfo, "a", "/1"),
		finding(ir.SeverityWarning, "b", "/2"),
		finding(ir.SeverityInfo, "a", "/3"),
		finding(ir.SeverityInfo, "a", "/4"),
		finding(ir.SeverityWarning, "a", "/5"),
		finding(ir.SeverityInfo, "a", "/6"),
		finding(ir.SeverityError, "c", "/7"),
	}}
	var w strings.Builder
	require.NoError(t, report.WriteText(&w, r, report.TextOptions{PerCode: 2}))
	assert.Equal(t, "info a s.yaml#/1: m\n"+
		"warning b s.yaml#/2: m\n"+
		"info a s.yaml#/3: m\n"+
		"warning a s.yaml#/5: m\n"+
		"error c s.yaml#/7: m\n"+
		"  2 more info a not shown\n"+
		"  5 of 7 findings shown: 1 error, 2 warning, 4 info, 0 accepted\n", w.String())
}

// TestWriteText_WithholdsBelowTheLeastSeverity pins Least: a finding rated
// below it is counted, each code on a line of its own, and one at it or above
// is shown, however it was reported.
func TestWriteText_WithholdsBelowTheLeastSeverity(t *testing.T) {
	t.Parallel()
	lowered := finding(ir.SeverityInfo, "lowered", "/3")
	lowered.Diagnostic.Severity = ir.SeverityError
	r := report.Report{Findings: []triage.Finding{
		finding(ir.SeverityInfo, "a", "/1"),
		finding(ir.SeverityWarning, "b", "/2"),
		lowered,
		finding(ir.SeverityInfo, "a", "/4"),
		finding(ir.SeverityError, "c", "/5"),
	}}
	var w strings.Builder
	require.NoError(t, report.WriteText(&w, r, report.TextOptions{Least: ir.SeverityWarning}))
	assert.Equal(t, "warning b /2: m\n"+
		"error c /5: m\n"+
		"  2 info a not shown\n"+
		"  1 info lowered not shown\n"+
		"  2 of 5 findings shown: 1 error, 1 warning, 3 info, 0 accepted\n", w.String())

	w.Reset()
	require.NoError(t, report.WriteText(&w, r, report.TextOptions{Least: ir.SeverityError, PerCode: 1}))
	assert.Equal(t, "error c /5: m\n"+
		"  2 info a not shown\n"+
		"  1 warning b not shown\n"+
		"  1 info lowered not shown\n"+
		"  1 of 5 findings shown: 1 error, 1 warning, 3 info, 0 accepted\n", w.String())
}

// TestWriteText_CountsWhatARuleAccepted pins an accepted finding as counted
// and never shown, at any options, and the tally it brings with it.
func TestWriteText_CountsWhatARuleAccepted(t *testing.T) {
	t.Parallel()
	accepted := finding(ir.SeverityError, "a", "/1")
	accepted.Accepted = true
	r := report.Report{Findings: []triage.Finding{accepted, finding(ir.SeverityInfo, "b", "/2")}}
	var w strings.Builder
	require.NoError(t, report.WriteText(&w, r, report.TextOptions{}))
	assert.Equal(t, "info b /2: m\n"+
		"  1 of 2 findings shown: 0 error, 0 warning, 1 info, 1 accepted\n", w.String())
}

// TestWriteText_ShowsWhatItCannotRank pins a finding rated at a severity no
// threshold ranks as shown at every Least, never hidden below one.
func TestWriteText_ShowsWhatItCannotRank(t *testing.T) {
	t.Parallel()
	r := report.Report{Findings: []triage.Finding{finding("fatal", "a", "/1")}}
	var w strings.Builder
	require.NoError(t, report.WriteText(&w, r, report.TextOptions{Least: ir.SeverityError}))
	assert.Equal(t, "fatal a /1: m\n", w.String())
}

// TestWriteText_NamesEachUnusedRule pins a line for each rule no finding met,
// naming its code and place, after the counts, and no tally for them alone.
func TestWriteText_NamesEachUnusedRule(t *testing.T) {
	t.Parallel()
	r := report.Report{UnusedRules: []triage.Rule{
		{Code: "a", Severity: ir.SeverityInfo, Reason: "r"},
		{Code: "b", Pointer: "/p", Accept: true, Reason: "r"},
		{Code: "c", Node: "s/x", Severity: ir.SeverityError, Reason: "r"},
	}}
	var w strings.Builder
	require.NoError(t, report.WriteText(&w, r, report.TextOptions{}))
	assert.Equal(t, "  triage rule for a matched no finding\n"+
		"  triage rule for b at /p matched no finding\n"+
		"  triage rule for c at node s/x matched no finding\n", w.String())
}

// TestWriteText_RefusesOptionsItCannotRead pins a Least naming no severity and
// a negative count as refused before anything is written.
func TestWriteText_RefusesOptionsItCannotRead(t *testing.T) {
	t.Parallel()
	r := report.Report{Findings: []triage.Finding{finding(ir.SeverityInfo, "a", "/1")}}
	for _, tc := range []struct {
		name string
		opts report.TextOptions
		want string
	}{
		{"a severity the IR does not declare", report.TextOptions{Least: "fatal"}, `"fatal" is no severity`},
		{"a negative count", report.TextOptions{PerCode: -1}, "-1 findings of a code"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			w := &failingWriter{failAt: 1}
			err := report.WriteText(w, r, tc.opts)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.want)
			assert.Zero(t, w.attempts)
		})
	}
}

// TestWriteText_EscapesWhatASpecWrites holds every string a spec can write to
// one printable line: a path, a pointer, a node, a code and a message, in the
// lines and in the counts, each control character, DEL, C1 control, format
// character and byte that is not UTF-8 spelled as %q spells it. A key holding a
// newline cannot forge a second finding.
func TestWriteText_EscapesWhatASpecWrites(t *testing.T) {
	t.Parallel()
	forged := finding(ir.SeverityInfo, "a", "/S\nerror openapi-forged spec.yaml: forged")
	shown := []ir.Diagnostic{
		{Severity: ir.SeverityWarning, Code: "c\x1b[31m", Message: "\a\x7f\u009b\u202e\u00a0\xff é \\x1b \ufffd",
			Provenance: ir.Provenance{Source: 0, Pointer: "/k\x1b]0;PWNED\a", Position: ir.Position{Line: 1}}},
		{Severity: ir.SeverityWarning, Code: "n", Message: "m", Provenance: ir.Provenance{Source: ir.NoSource, Node: "s/\r"}},
		{Severity: ir.SeverityWarning, Code: "w", Message: "m", Provenance: ir.Provenance{Source: 0, Pointer: "/p", WrittenBy: 1}},
	}
	r := report.Report{
		Sources:  []ir.SourceInfo{{Path: "dir\t/s.yaml"}, {Path: "o\nverlay.yaml", Format: "overlay@1.0.0"}},
		Findings: append(rated(shown), forged, finding(ir.SeverityInfo, "a\x00", "/1"), finding(ir.SeverityInfo, "a\x00", "/2")),
		UnusedRules: []triage.Rule{
			{Code: "u\x1b", Pointer: "/\n", Accept: true, Reason: "r"},
			{Code: "v", Node: "s/\x1b", Severity: ir.SeverityInfo, Reason: "r"},
		},
	}
	var w strings.Builder
	require.NoError(t, report.WriteText(&w, r, report.TextOptions{PerCode: 1}))
	assert.Equal(t, `warning c\x1b[31m dir\t/s.yaml:1 (#/k\x1b]0;PWNED\a): \a\x7f\u009b\u202e\u00a0\xff é \x1b `+"\ufffd\n"+
		`warning n s/\r: m`+"\n"+
		`warning w dir\t/s.yaml#/p (written by o\nverlay.yaml): m`+"\n"+
		`info a dir\t/s.yaml#/S\nerror openapi-forged spec.yaml: forged: m`+"\n"+
		`info a\x00 dir\t/s.yaml#/1: m`+"\n"+
		`  1 more info a\x00 not shown`+"\n"+
		`  triage rule for u\x1b at /\n matched no finding`+"\n"+
		`  triage rule for v at node s/\x1b matched no finding`+"\n"+
		"  5 of 6 findings shown: 0 error, 3 warning, 3 info, 0 accepted\n", w.String())
	for i, line := range strings.Split(strings.TrimSuffix(w.String(), "\n"), "\n") {
		assert.Negative(t, strings.IndexFunc(line, func(r rune) bool { return r < 0x20 || r == 0x7f }), "line %d: %q", i, line)
	}
}
