// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package main

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/internal/testspec"
)

// TestRun_SingleDashLongFlagsAreRefused pins that a long flag typed with one
// dash, as morphic's earlier flag package accepted, is refused with the
// spelling to use. pflag would read "-opt=a=1" on compile as "-o pt=a=1",
// dropping the option and writing a file named "pt=a=1" with exit 0. Not run
// in parallel: each row moves into an empty directory of its own, which a
// misread must not write to.
func TestRun_SingleDashLongFlagsAreRefused(t *testing.T) {
	spec := writeFile(t, "spec.yaml", testspec.Tiny)

	tests := []struct {
		name   string
		args   []string
		reason string
		help   string // the command the hint points at
	}{
		{"an option and its value joined", []string{"compile", spec, "-opt=a=1"},
			`write "--opt=a=1", not "-opt=a=1"`, "morphic compile"},
		{"an option and its separate value", []string{"compile", "-opt", "a=1", spec},
			`write "--opt", not "-opt"`, "morphic compile"},
		{"before the command it belongs to", []string{"-opt=a=1", "compile", spec},
			`write "--opt=a=1", not "-opt=a=1"`, "morphic compile"},
		{"a command's help flag, which cobra defines late", []string{"compile", spec, "-help"},
			`write "--help", not "-help"`, "morphic compile"},
		{"the version flag", []string{"-version"}, `write "--version", not "-version"`, "morphic"},
		{"validate's flags", []string{"validate", spec, "-skip-validate"},
			`write "--skip-validate", not "-skip-validate"`, "morphic validate"},
		{"the output file", []string{"compile", spec, "-output=ir.json"},
			`write "--output=ir.json", not "-output=ir.json"`, "morphic compile"},
		{"after a flag and then an operand", []string{"compile", "--pretty", spec, "-output=ir.json"},
			`write "--output=ir.json", not "-output=ir.json"`, "morphic compile"},
		// The rows from here put the one-dash flag after each spelling pflag
		// reads a flag ahead of it in: a long flag with no, a separate or a joined
		// value; a short flag or a cluster with no, a separate, an attached or a
		// joined value; and a "--" taken as a separate value.
		{"after a long flag and its separate value", []string{"compile", spec, "--fail-on", "error", "-output=ir.json"},
			`write "--output=ir.json", not "-output=ir.json"`, "morphic compile"},
		{"after a short flag and its separate value", []string{"compile", spec, "-o", "out.json", "-output=ir.json"},
			`write "--output=ir.json", not "-output=ir.json"`, "morphic compile"},
		{"after a long flag that takes no value", []string{"compile", spec, "--pretty", "-output=ir.json"},
			`write "--output=ir.json", not "-output=ir.json"`, "morphic compile"},
		{"after a long flag and its joined value", []string{"compile", spec, "--fail-on=error", "-output=ir.json"},
			`write "--output=ir.json", not "-output=ir.json"`, "morphic compile"},
		{"after a short flag and its attached value", []string{"compile", spec, "-oout.json", "-output=ir.json"},
			`write "--output=ir.json", not "-output=ir.json"`, "morphic compile"},
		{"after a short flag and its joined value", []string{"compile", spec, "-o=out.json", "-output=ir.json"},
			`write "--output=ir.json", not "-output=ir.json"`, "morphic compile"},
		{"after a short flag that takes no value", []string{"compile", spec, "-h", "-output=ir.json"},
			`write "--output=ir.json", not "-output=ir.json"`, "morphic compile"},
		{"after a cluster of short flags and its separate value", []string{"compile", spec, "-ho", "out.json", "-output=ir.json"},
			`write "--output=ir.json", not "-output=ir.json"`, "morphic compile"},
		{"after a cluster of short flags and its attached value", []string{"compile", spec, "-hoout.json", "-output=ir.json"},
			`write "--output=ir.json", not "-output=ir.json"`, "morphic compile"},
		{"after a cluster of short flags and its joined value", []string{"compile", spec, "-ho=out.json", "-output=ir.json"},
			`write "--output=ir.json", not "-output=ir.json"`, "morphic compile"},
		{"after a cluster of short flags that take no value", []string{"compile", spec, "-hh", "-output=ir.json"},
			`write "--output=ir.json", not "-output=ir.json"`, "morphic compile"},
		{"after a long flag's separate value that is a double dash", []string{"compile", spec, "--output", "--", "-output=ir.json"},
			`write "--output=ir.json", not "-output=ir.json"`, "morphic compile"},
		{"after a short flag's separate value that is a double dash", []string{"compile", spec, "-o", "--", "-output=ir.json"},
			`write "--output=ir.json", not "-output=ir.json"`, "morphic compile"},
		{"after a cluster's separate value that is a double dash", []string{"compile", spec, "-ho", "--", "-output=ir.json"},
			`write "--output=ir.json", not "-output=ir.json"`, "morphic compile"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			t.Chdir(dir)
			var stdout, stderr bytes.Buffer

			assert.Equal(t, 2, run(t.Context(), tt.args, &stdout, &stderr))
			assert.Empty(t, stdout.String())
			assert.Contains(t, stderr.String(), "long flags take two dashes: "+tt.reason)
			assert.Contains(t, stderr.String(), `Try "`+tt.help+` --help" for usage.`)
			assert.Equal(t, 1, strings.Count(stderr.String(), "Try "), "got:\n%s", stderr.String())

			entries, err := os.ReadDir(dir)
			require.NoError(t, err)
			assert.Empty(t, entries, "a refused run writes nothing")
		})
	}
}

// TestRun_GoTestPrefixedArgumentsAreRefused pins that an argument pflag would
// drop unread, as it keeps what starts "test." for go test's own flags, is
// refused as the shorthand it spells: "-test.output=ir.json" would otherwise
// write the document to stdout and exit 0. Each row runs in an empty directory
// of its own, which a refused run leaves empty.
func TestRun_GoTestPrefixedArgumentsAreRefused(t *testing.T) {
	spec := writeFile(t, "spec.yaml", testspec.Tiny)

	tests := []struct {
		name string
		args []string
		tok  string // the argument refused
		help string // the command the hint points at
	}{
		{"a switch", []string{"compile", spec, "-test.v"}, "-test.v", "morphic compile"},
		{"one with a value", []string{"compile", spec, "-test.output=ir.json"}, "-test.output=ir.json", "morphic compile"},
		{"validate's", []string{"validate", spec, "-test.anything"}, "-test.anything", "morphic validate"},
		{"before the command", []string{"-test.v", "validate", spec}, "-test.v", "morphic validate"},
		{"after switches", []string{"compile", spec, "-hhtest.v"}, "-hhtest.v", "morphic compile"},
		{"on root", []string{"-vtest.v"}, "-vtest.v", "morphic"},
		{"help's", []string{"help", "-test.v"}, "-test.v", "morphic help"},
		{"a completion script's", []string{"completion", "bash", "-test.v"}, "-test.v", "morphic completion bash"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Chdir(t.TempDir())
			var stdout, stderr bytes.Buffer

			assert.Equal(t, 2, run(t.Context(), tt.args, &stdout, &stderr))
			assert.Empty(t, stdout.String())
			assert.Equal(t, "morphic: unknown shorthand flag: 't' in "+tt.tok+"\n"+
				`Try "`+tt.help+` --help" for usage.`+"\n", stderr.String())
			assert.Empty(t, namesIn(t, "."), "a refused run writes nothing")
		})
	}
}

// TestRun_GoTestPrefixedArgumentsThatAreNotDropped pins what that refusal leaves
// alone: a value of a flag, a shorthand's attached value, a long flag, and an
// operand after "--", which pflag reads as it reads any other.
func TestRun_GoTestPrefixedArgumentsThatAreNotDropped(t *testing.T) {
	spec := writeFile(t, "spec.yaml", testspec.Tiny)

	tests := []struct {
		name   string
		args   []string
		code   int
		stderr string
		files  []string
	}{
		{"a long flag's value", []string{"compile", spec, "--output", "-test.x"}, 0, "", []string{"-test.x"}},
		{"a short flag's value", []string{"compile", spec, "-o", "-test.x"}, 0, "", []string{"-test.x"}},
		{"a short flag's attached value", []string{"compile", spec, "-otest.json"}, 0, "", []string{"test.json"}},
		{"a switch then an attached value", []string{"compile", spec, "-hotest.json"}, 0, "", nil},
		{"a long flag", []string{"compile", spec, "--test.v"}, 2, "morphic: unknown flag: --test.v\n", nil},
		{"an operand after --", []string{"compile", "--", "-test.v"}, 2, `read spec "-test.v"`, nil},
		{"an operand spelled with test. after its first letter", []string{"compile", "xtest.yaml"}, 2,
			`read spec "xtest.yaml"`, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Chdir(t.TempDir())
			var stdout, stderr bytes.Buffer

			assert.Equal(t, tt.code, run(t.Context(), tt.args, &stdout, &stderr), "stderr: %s", stderr.String())
			if tt.stderr == "" {
				assert.Empty(t, stderr.String())
			} else {
				assert.Contains(t, stderr.String(), tt.stderr)
			}
			assert.ElementsMatch(t, tt.files, namesIn(t, "."))
		})
	}
}

// TestDropsAsGoTestFlag_NoCommandDefinesT holds the refusal's message to the
// tree: it names 't' an unknown shorthand, which is true only while no command
// defines one.
func TestDropsAsGoTestFlag_NoCommandDefinesT(t *testing.T) {
	t.Parallel()
	root := newRootCmd()
	root.InitDefaultHelpCmd()
	root.InitDefaultCompletionCmd()

	pending := []*cobra.Command{root}
	for len(pending) > 0 {
		c := pending[0]
		pending = append(pending[1:], c.Commands()...)
		assert.Nil(t, c.Flags().ShorthandLookup("t"), "%s defines -t", c.CommandPath())
	}
}

// TestRun_SingleDashArgumentsThatAreNotFlags pins what the refusal leaves to
// pflag: an argument pflag reads as a flag's value or as an operand is not a
// flag, whatever it is spelled like, and a run pflag refuses earlier reports
// that first problem.
func TestRun_SingleDashArgumentsThatAreNotFlags(t *testing.T) {
	t.Parallel()

	spec := writeFile(t, "spec.yaml", testspec.Tiny)
	tests := []struct {
		name   string
		args   []string
		reason string
	}{
		// --explain refuses -o before anything is written, which shows -opt was
		// read as -o's value.
		{"the value of a short flag", []string{"compile", spec, "--explain", "", "-o", "-opt"},
			"--explain writes no document"},
		{"the value of a long flag", []string{"compile", spec, "--fail-on", "-pretty"},
			`invalid argument "-pretty" for "--fail-on" flag`},
		{"an operand after --", []string{"compile", "--", "-opt"}, `read spec "-opt"`},
		{"an argument of an unknown command", []string{"bogus", "-opt"}, `unknown command "bogus"`},
		{"a root flag's one-dash spelling after an unknown command", []string{"bogus", "-version"},
			`unknown command "bogus"`},
		{"an argument after one pflag refuses", []string{"compile", spec, "--bogus", "-opt=a=1"},
			"unknown flag: --bogus"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var stdout, stderr bytes.Buffer

			assert.Equal(t, 2, run(t.Context(), tt.args, &stdout, &stderr))
			assert.Contains(t, stderr.String(), tt.reason)
			assert.NotContains(t, stderr.String(), "two dashes")
		})
	}
}

// TestRun_AJoinedValueIsReadOnce pins that looking at the arguments before
// cobra parses them sets nothing: an --opt value set twice is refused as a
// repeated key, so a look that set it would fail this run.
func TestRun_AJoinedValueIsReadOnce(t *testing.T) {
	t.Parallel()

	spec := writeFile(t, "spec.yaml", testspec.Tiny)
	var stdout, stderr bytes.Buffer

	require.Equal(t, 0, run(t.Context(), []string{"compile", spec, "--opt=grouping=path-prefix"}, &stdout, &stderr),
		"stderr: %s", stderr.String())
	assert.NotEmpty(t, stdout.String())
}

// TestFindCommand_ReturnsNothingWhenCobraFindsNone pins findCommand's comma-ok
// form: a command found comes with the arguments left for it, and one not found
// with neither, where cobra itself hands back the root and everything it was
// given.
func TestFindCommand_ReturnsNothingWhenCobraFindsNone(t *testing.T) {
	t.Parallel()
	root := newRootCmd()

	target, rest, found := findCommand(root, []string{"compile", "spec.yaml", "--output=ir.json"})
	require.True(t, found)
	assert.Equal(t, "compile", target.Name())
	assert.Equal(t, []string{"spec.yaml", "--output=ir.json"}, rest)

	target, rest, found = findCommand(root, []string{"bogus", "-version"})
	assert.False(t, found)
	assert.Nil(t, target)
	assert.Nil(t, rest)
}
