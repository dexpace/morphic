package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/engine"
	"github.com/dexpace/morphic/ir"
)

// styledResult has one diagnostic of every severity the CLI draws and one it
// does not know, each with a different kind of location, and the plain text
// each must read as once its styling is gone.
func styledResult() (res *engine.Result, plain []string) {
	res = &engine.Result{
		Sources: []ir.SourceInfo{{Path: "spec.yaml"}},
		Diagnostics: []ir.Diagnostic{
			{Severity: ir.SeverityError, Code: "openapi/bad", Message: "m1",
				Provenance: ir.Provenance{Source: 0, Pointer: "/paths/~1x"}},
			{Severity: ir.SeverityWarning, Code: "openapi/odd", Message: "m2",
				Provenance: ir.Provenance{Source: 0, Position: ir.Position{Line: 5, Column: 1}}},
			{Severity: ir.SeverityInfo, Code: "openapi/note", Message: "m3",
				Provenance: ir.Provenance{Source: ir.NoSource, Node: "op/x"}},
			{Severity: "hint", Code: "openapi/other", Message: "m4",
				Provenance: ir.Provenance{Source: ir.NoSource}},
		},
	}
	plain = []string{
		"error openapi/bad spec.yaml#/paths/~1x: m1",
		"warning openapi/odd spec.yaml:5:1: m2",
		"info openapi/note op/x: m3",
		"hint openapi/other: m4",
	}
	return res, plain
}

func TestWriteDiagnostics_StylesSeverityAndLocation(t *testing.T) {
	t.Parallel()

	res, plain := styledResult()
	var buf bytes.Buffer

	writeDiagnostics(&colorprofile.Writer{Forward: &buf, Profile: colorprofile.TrueColor}, res)

	lines := strings.Split(strings.TrimSuffix(buf.String(), "\n"), "\n")
	require.Len(t, lines, len(plain))
	for i, line := range lines {
		assert.Equal(t, plain[i], ansi.Strip(line), "styling must add to the plain line, not change it")

		// The message is the spec's own words and the last thing on the line: it
		// is never drawn, so nothing after the final ": " carries styling.
		message := line[strings.LastIndex(line, ": "):]
		assert.NotContains(t, message, "\x1b", "line %d: the message must not be styled", i)
	}

	// Compared against the styled word itself: the location is dimmed too, so a
	// line differing from the plain one does not say its severity was drawn.
	for i, severity := range []ir.Severity{ir.SeverityError, ir.SeverityWarning, ir.SeverityInfo} {
		drawn := severityStyle(severity).Render(string(severity))
		require.NotEqual(t, string(severity), drawn)
		assert.True(t, strings.HasPrefix(lines[i], drawn+" "), "line %d must open with the drawn severity: %q", i, lines[i])
	}
	assert.Equal(t, plain[3], lines[3], "a severity it does not know, with no location, is left exactly plain")
}

func TestStyledLocation_DimsOnlyWhatIsThere(t *testing.T) {
	t.Parallel()

	assert.Empty(t, styledLocation(""), "no location means no escape sequence either")

	got := styledLocation(" spec.yaml#/paths")
	assert.NotEqual(t, " spec.yaml#/paths", got, "a location is dimmed")
	assert.Equal(t, " spec.yaml#/paths", ansi.Strip(got))
}

// TestSeverityStyle_DrawsEachSeverityDifferently holds what the styling is for:
// the two severities that can fail a gate are told apart from each other and
// from the one that cannot, which a single colour for "something to read" would
// not do.
func TestSeverityStyle_DrawsEachSeverityDifferently(t *testing.T) {
	t.Parallel()

	drawn := map[string]ir.Severity{}
	for _, s := range []ir.Severity{ir.SeverityError, ir.SeverityWarning, ir.SeverityInfo} {
		got := severityStyle(s).Render(string(s))
		require.NotEqual(t, string(s), got, "%s must be drawn", s)
		require.Equal(t, string(s), ansi.Strip(got))

		prefix, _, found := strings.Cut(got, string(s))
		require.True(t, found, "the severity's own word is in what is drawn: %q", got)
		other, clash := drawn[prefix]
		require.False(t, clash, "%s and %s are drawn the same way", s, other)
		drawn[prefix] = s
	}
}

// TestWriteDiagnostics_EveryProfileReadsAsThePlainLines holds that downsampling
// for a poorer terminal changes how a line is drawn and never what it says, and
// that a stream that is no terminal at all receives no escape sequence.
func TestWriteDiagnostics_EveryProfileReadsAsThePlainLines(t *testing.T) {
	t.Parallel()

	res, plain := styledResult()
	want := strings.Join(plain, "\n") + "\n"

	profiles := []colorprofile.Profile{
		colorprofile.TrueColor, colorprofile.ANSI256, colorprofile.ANSI, colorprofile.ASCII, colorprofile.NoTTY,
	}
	for _, profile := range profiles {
		t.Run(profile.String(), func(t *testing.T) {
			t.Parallel()
			var buf bytes.Buffer

			writeDiagnostics(&colorprofile.Writer{Forward: &buf, Profile: profile}, res)

			assert.Equal(t, want, ansi.Strip(buf.String()))
			if profile == colorprofile.NoTTY {
				assert.Equal(t, want, buf.String(), "a stream that is no terminal gets the plain lines byte for byte")
			}
		})
	}
}

// TestRun_DiagnosticsAreStyledOnlyWhereColourIsAsked is the wiring the pieces
// above cannot show: that renderDiagnostics hands the stream to the profile
// detection with the process environment, so a redirected stderr is plain and a
// forced one is not. Not run in parallel: it sets an environment variable.
func TestRun_DiagnosticsAreStyledOnlyWhereColourIsAsked(t *testing.T) {
	spec := writeFile(t, "warn.yaml", `openapi: 3.1.0
info: {title: T, version: "1"}
paths: {}
components:
  schemas:
    S: {type: object, not: {required: [x]}}
`)

	var stdout, stderr bytes.Buffer
	require.Equal(t, 0, run([]string{"validate", spec}, &stdout, &stderr))
	require.Contains(t, stderr.String(), "openapi/validation-only-keyword")
	assert.NotContains(t, stderr.String(), "\x1b", "a redirected stderr must get no escape sequence")

	t.Setenv("CLICOLOR_FORCE", "1")
	stderr.Reset()
	require.Equal(t, 0, run([]string{"validate", spec}, &stdout, &stderr))
	assert.Contains(t, stderr.String(), "\x1b[", "CLICOLOR_FORCE must be honoured")
	assert.Contains(t, ansi.Strip(stderr.String()), "info openapi/validation-only-keyword "+spec)
}
