package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"

	"charm.land/fang/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"
	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/internal/testspec"
)

func TestExitError_MessageAndUnwrap(t *testing.T) {
	t.Parallel()

	cause := errors.New("disk full")
	withReason := &exitError{code: 2, err: cause}
	assert.Equal(t, "disk full", withReason.Error())
	assert.ErrorIs(t, withReason, cause, "the reason must stay reachable for errors.Is")

	reported := &exitError{code: 1}
	assert.Empty(t, reported.Error(), "a run whose findings were already printed has no message")
	assert.NoError(t, reported.Unwrap())
}

func TestExitWith_ZeroIsSuccess(t *testing.T) {
	t.Parallel()

	require.NoError(t, exitWith(0))

	err := exitWith(1)
	var ee *exitError
	require.ErrorAs(t, err, &ee)
	assert.Equal(t, 1, ee.code)
	assert.NoError(t, ee.err, "the findings are already on stderr, so there is no message")
}

func TestExitCode_Mapping(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want int
	}{
		{"nil is success", nil, 0},
		{"findings", &exitError{code: exitFindings}, 1},
		{"failure", failure(errors.New("boom")), 2},
		{"a code found through wrapping", fmt.Errorf("outer: %w", &exitError{code: exitFindings}), 1},
		{"misuse", &usageError{path: "morphic", err: errors.New("bad")}, 2},
		{"anything cobra refuses on its own", errors.New(`unknown command "x" for "morphic"`), 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, exitCode(tt.err))
		})
	}
}

func TestMisuse_NamesTheCommandItPointsAt(t *testing.T) {
	t.Parallel()

	root := newRootCmd()
	compile, _, err := root.Find([]string{"compile"})
	require.NoError(t, err)

	var ue *usageError
	require.ErrorAs(t, misuse(compile, "no spec"), &ue)
	assert.Equal(t, "morphic compile", ue.path)
	assert.EqualError(t, ue, "no spec")

	// A refused flag is classified the same way and keeps its own message.
	cause := errors.New("unknown flag: --x")
	require.ErrorAs(t, asMisuse(compile, cause), &ue)
	assert.ErrorIs(t, ue, cause)
}

// handlerCase is one error and the plain text errorHandler gives it off a
// terminal. The same cases run against a bare buffer and a colorprofile writer
// at NoTTY, which is what fang hands the handler when stderr is redirected.
type handlerCase struct {
	name string
	err  error
	want string
}

func plainHandlerCases() []handlerCase {
	return []handlerCase{
		{"misuse names its command's help", misuseOf("morphic compile", "compile requires exactly one spec file"),
			"morphic: compile requires exactly one spec file\nTry \"morphic compile --help\" for usage.\n"},
		{"a refusal before any command was chosen", errors.New(`unknown command "x" for "morphic"`),
			"morphic: unknown command \"x\" for \"morphic\"\nTry \"morphic --help\" for usage.\n"},
		{"a suggestion's trailing newline is not doubled", errors.New("unknown command \"compil\"\n\nDid you mean this?\n\tcompile\n"),
			"morphic: unknown command \"compil\"\n\nDid you mean this?\n\tcompile\nTry \"morphic --help\" for usage.\n"},
		{"a failure is reported without a pointer to help", failure(errors.New("open x: no such file")),
			"morphic: open x: no such file\n"},
		{"findings already printed say nothing more", &exitError{code: exitFindings}, ""},
	}
}

func misuseOf(path, msg string) error {
	return &usageError{path: path, err: errors.New(msg)}
}

func TestErrorHandler_PlainOffATerminal(t *testing.T) {
	t.Parallel()

	writers := []struct {
		name string
		wrap func(*bytes.Buffer) io.Writer
	}{
		{"buffer", func(b *bytes.Buffer) io.Writer { return b }},
		{"NoTTY writer", func(b *bytes.Buffer) io.Writer {
			return &colorprofile.Writer{Forward: b, Profile: colorprofile.NoTTY}
		}},
	}

	for _, tt := range plainHandlerCases() {
		for _, w := range writers {
			t.Run(tt.name+"/"+w.name, func(t *testing.T) {
				t.Parallel()
				var buf bytes.Buffer

				errorHandler(w.wrap(&buf), fang.Styles{}, tt.err)

				assert.Empty(t, cmp.Diff(tt.want, buf.String()))
			})
		}
	}
}

func TestErrorHandler_StyledOnATerminal(t *testing.T) {
	t.Parallel()

	styles := fang.Styles{
		ErrorHeader: lipgloss.NewStyle().SetString("ERROR"),
		ErrorText:   lipgloss.NewStyle(),
	}
	tests := []struct {
		name string
		err  error
		want string
	}{
		{"misuse carries the pointer to help", misuseOf("morphic validate", "validate requires exactly one spec file"),
			"ERROR\nValidate requires exactly one spec file\n\nTry \"morphic validate --help\" for usage.\n\n"},
		{"a failure does not", failure(errors.New("open x: no such file")),
			"ERROR\nOpen x: no such file\n\n"},
		{"findings already printed say nothing more", &exitError{code: exitFindings}, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var buf bytes.Buffer

			errorHandler(&colorprofile.Writer{Forward: &buf, Profile: colorprofile.ANSI}, styles, tt.err)

			assert.Empty(t, cmp.Diff(tt.want, buf.String()))
		})
	}
}

func TestUpperFirst_Cases(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name, in, want string
	}{
		{"a lower-case word", "unknown flag: --x", "Unknown flag: --x"},
		{"a flag is left as spelled", "--explain writes no document", "--explain writes no document"},
		{"nothing but the first letter changes", "openAPI.yaml: bad", "OpenAPI.yaml: bad"},
		{"already raised", "Engine: read spec", "Engine: read spec"},
		{"a letter outside ASCII", "élan", "Élan"},
		{"a digit", "9 specs", "9 specs"},
		{"a quote", `"x" is unknown`, `"x" is unknown`},
		{"empty", "", ""},
		{"invalid UTF-8", "\xffabc", "\xffabc"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, upperFirst(tt.in))
		})
	}
}

// TestErrorHandler_StyledDoesNotRewriteTheMessage holds that fang's transform of
// the error text is not applied: it title-cases the whole first word, so a
// message that opens with a flag would come out as "--Explain".
func TestErrorHandler_StyledDoesNotRewriteTheMessage(t *testing.T) {
	t.Parallel()

	styles := fang.Styles{
		ErrorHeader: lipgloss.NewStyle().SetString("ERROR"),
		ErrorText:   lipgloss.NewStyle().Transform(strings.ToUpper),
	}
	var buf bytes.Buffer

	errorHandler(&colorprofile.Writer{Forward: &buf, Profile: colorprofile.ANSI}, styles,
		misuseOf("morphic compile", "--explain writes no document"))

	assert.Contains(t, buf.String(), "--explain writes no document")
	assert.Contains(t, buf.String(), `Try "morphic compile --help" for usage.`)
	assert.NotContains(t, buf.String(), "EXPLAIN", "the style's transform must not reach the message")
}

// TestRun_StyledErrorKeepsAFlagsSpelling is the same property through fang's own
// styles, which a hand-built Styles cannot stand in for: the transform that
// mangles the message lives in the ones fang makes. A terminal is forced with
// the environment. Not run in parallel: it sets variables.
func TestRun_StyledErrorKeepsAFlagsSpelling(t *testing.T) {
	spec := writeFile(t, "spec.yaml", testspec.Tiny)
	t.Setenv("TTY_FORCE", "1")
	t.Setenv("TERM", "xterm-256color")
	var stdout, stderr bytes.Buffer

	code := run([]string{"compile", spec, "--explain", "", "-o", "ir.json"}, &stdout, &stderr)

	require.Equal(t, 2, code)
	plain := ansi.Strip(stderr.String())
	require.Contains(t, plain, "ERROR", "the styled block must have been chosen; got:\n%s", plain)
	assert.Contains(t, plain, "--explain writes no document")
	assert.NotContains(t, plain, "--Explain")
}
