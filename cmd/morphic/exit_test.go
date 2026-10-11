package main

import (
	"bytes"
	"errors"
	"fmt"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

// TestReportError_PlainLines pins every shape reportError writes: a misuse with
// the help page of the command misused, a refusal cobra made before choosing one,
// a failure with no pointer to help, and silence for a run with nothing to add.
func TestReportError_PlainLines(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want string
	}{
		{"misuse names its command's help", misuseOf("morphic compile", "compile requires exactly one spec file"),
			"morphic: compile requires exactly one spec file\nTry \"morphic compile --help\" for usage.\n"},
		{"a refusal before any command was chosen", errors.New(`unknown command "x" for "morphic"`),
			"morphic: unknown command \"x\" for \"morphic\"\nTry \"morphic --help\" for usage.\n"},
		{"a suggestion's trailing newline is not doubled", errors.New("unknown command \"compil\"\n\nDid you mean this?\n\tcompile\n"),
			"morphic: unknown command \"compil\"\n\nDid you mean this?\n\tcompile\nTry \"morphic --help\" for usage.\n"},
		{"a failure is reported without a pointer to help", failure(errors.New("open x: no such file")),
			"morphic: open x: no such file\n"},
		{"findings already printed say nothing more", &exitError{code: exitFindings}, ""},
		{"a run that ended well says nothing", nil, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var buf bytes.Buffer

			reportError(&buf, tt.err)

			assert.Empty(t, cmp.Diff(tt.want, buf.String()))
		})
	}
}

func misuseOf(path, msg string) error {
	return &usageError{path: path, err: errors.New(msg)}
}
