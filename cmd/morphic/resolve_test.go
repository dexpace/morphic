// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package main

import (
	"bytes"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/internal/testspec"
)

// TestRun_AFlagBeforeItsCommandIsReadAsTheCommandReadsIt pins each shape of
// argument ahead of a command's name: a switch, a flag that takes a value, one
// the command does not know, a "--", an operand, and a value that is the
// command's own name. cobra alone reads a flag it does not know as taking the
// next argument, so --pretty took "compile" and the spec was reported as an
// unknown command. Each row runs in an empty directory of its own, and files
// lists every name the run leaves there.
func TestRun_AFlagBeforeItsCommandIsReadAsTheCommandReadsIt(t *testing.T) {
	spec := writeFile(t, "spec.yaml", testspec.Tiny)

	tests := []struct {
		name   string
		args   []string
		code   int
		stderr string   // a line stderr holds; empty for none at all
		files  []string // what the run leaves in its directory
	}{
		{"a switch of the command", []string{"--pretty", "compile", spec, "-o", "ir.json"}, 0, "", []string{"ir.json"}},
		{"a switch the command reads", []string{"--verify", "compile", spec}, 0, "", nil},
		{"a switch of another command", []string{"--pretty", "validate", spec}, 2,
			"morphic: unknown flag: --pretty\nTry \"morphic validate --help\" for usage.\n", nil},
		{"a switch of another command ahead of a command's name", []string{"--pretty", "validate", "compile", spec}, 2,
			"morphic: unknown flag: --pretty\nTry \"morphic validate --help\" for usage.\n", nil},
		{"a long flag no command knows", []string{"--nosuchflag", "compile", spec}, 2,
			"morphic: unknown flag: --nosuchflag\nTry \"morphic compile --help\" for usage.\n", nil},
		{"a short flag no command knows", []string{"-p", "compile", spec}, 2,
			"morphic: unknown shorthand flag: 'p' in -p\nTry \"morphic compile --help\" for usage.\n", nil},
		{"a flag and its separate value", []string{"--fail-on", "warning", "compile", spec}, 0, "", nil},
		{"a short flag and its separate value", []string{"-o", "ir.json", "compile", spec}, 0, "", []string{"ir.json"}},
		{"a value that is the command's name", []string{"-o", "compile", "compile", spec}, 0, "", []string{"compile"}},
		{"a value that is another command's name", []string{"-o", "validate", "compile", spec}, 0, "", []string{"validate"}},
		{"a shared flag's value that is another command's name", []string{"--fail-on", "validate", "compile", spec}, 2,
			"morphic: invalid argument \"validate\" for \"--fail-on\" flag", nil},
		{"a value that is a double dash", []string{"-o", "--", "compile", spec}, 0, "", []string{"--"}},
		{"a value that is the spec", []string{"-o", spec, "compile"}, 2,
			"morphic: compile requires exactly one spec file\nTry \"morphic compile --help\" for usage.\n", nil},
		{"a switch, then the command's name as an operand", []string{"--verify", "compile", "validate"}, 2,
			"morphic: engine: read spec \"validate\": open validate: no such file or directory\n", nil},
		{"a switch of the command ahead of help", []string{"--skip-validate", "help", "compile"}, 2,
			"morphic: unknown flag: --skip-validate\nTry \"morphic help --help\" for usage.\n", nil},
		{"a double dash", []string{"--", "compile", spec}, 2,
			"morphic: unexpected argument \"compile\" after \"--\"\nTry \"morphic --help\" for usage.\n", nil},
		{"an operand", []string{spec, "compile"}, 2, "morphic: unknown command", nil},
		{"an empty argument", []string{"", "compile", spec}, 2,
			"morphic: compile requires exactly one spec file\nTry \"morphic compile --help\" for usage.\n", nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Chdir(t.TempDir())
			var stdout, stderr bytes.Buffer

			code := run(t.Context(), tt.args, &stdout, &stderr)

			assert.Equal(t, tt.code, code, "stderr: %s", stderr.String())
			if tt.stderr == "" {
				assert.Empty(t, stderr.String())
			} else {
				assert.Contains(t, stderr.String(), tt.stderr)
			}
			assert.ElementsMatch(t, tt.files, namesIn(t, "."))
		})
	}
}

// TestRun_TheSpecOperandTakesNoStandIn pins the operands a spec-taking command
// reads as a file named so, and none as standard input: a lone dash and an
// empty argument are paths like any other.
func TestRun_TheSpecOperandTakesNoStandIn(t *testing.T) {
	t.Chdir(t.TempDir())
	for operand, reason := range map[string]string{
		"-": `read spec "-": open -: no such file or directory`,
		"":  `read spec "": open : no such file or directory`,
	} {
		for _, command := range []string{"compile", "validate"} {
			var stdout, stderr bytes.Buffer

			assert.Equal(t, 2, run(t.Context(), []string{command, operand}, &stdout, &stderr), "%s %q", command, operand)
			assert.Contains(t, stderr.String(), reason, "%s %q", command, operand)
			assert.Empty(t, stdout.String(), "%s %q", command, operand)
		}
	}
}

// TestRun_ACompletionRequestReadsAFlagBeforeItsCommand pins that completing
// what follows a command reads a flag ahead of the command's name as the
// command does, as running it would, and leaves the word being completed last.
func TestRun_ACompletionRequestReadsAFlagBeforeItsCommand(t *testing.T) {
	t.Parallel()

	got, directive := complete(t, "--pretty", "compile", "")
	assert.Equal(t, []string{"yaml", "yml", "json"}, got)
	assert.Equal(t, cobra.ShellCompDirectiveFilterFileExt, directive)

	got, directive = complete(t, "--pretty", "compile", "--output", "")
	assert.Equal(t, []string{"json"}, got, "the word before the one completed stays where it was")
	assert.Equal(t, cobra.ShellCompDirectiveFilterFileExt, directive)
}

// TestCommandFirst_MovesOnlyTheCommandsWords pins the order commandFirst hands
// cobra: the words naming the command, at every level, then every other
// argument as it came. A completion request keeps its own word first and the
// word being completed last. Reading the flags defines a command's help flag,
// so each case reads a tree of its own.
func TestCommandFirst_MovesOnlyTheCommandsWords(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		args []string
		want []string
	}{
		{"nothing", nil, nil},
		{"the command first already", []string{"compile", "s", "--pretty"}, []string{"compile", "s", "--pretty"}},
		{"a switch ahead of it", []string{"--pretty", "compile", "s"}, []string{"compile", "--pretty", "s"}},
		{"a value ahead of it", []string{"-o", "x", "compile", "s"}, []string{"compile", "-o", "x", "s"}},
		{"its name as a value", []string{"-o", "compile", "compile", "s"}, []string{"compile", "-o", "compile", "s"}},
		{"another's name as a value", []string{"-o", "validate", "compile", "s"}, []string{"compile", "-o", "validate", "s"}},
		{"another's name as a shared flag's value", []string{"--fail-on", "validate", "compile", "s"},
			[]string{"compile", "--fail-on", "validate", "s"}},
		{"a flag neither command knows", []string{"--x", "validate", "compile", "s"},
			[]string{"validate", "--x", "compile", "s"}},
		{"a command beneath another", []string{"--no-descriptions", "completion", "bash"},
			[]string{"completion", "bash", "--no-descriptions"}},
		{"no command", []string{"--pretty", "s"}, []string{"--pretty", "s"}},
		{"an operand first", []string{"s", "compile"}, []string{"s", "compile"}},
		{"a name after a double dash", []string{"--", "compile"}, []string{"--", "compile"}},
		{"a completion request", []string{"__complete", "--pretty", "compile", "-o", ""},
			[]string{"__complete", "compile", "--pretty", "-o", ""}},
		{"a completion request of one word", []string{"__complete", "compile"}, []string{"__complete", "compile"}},
		{"a completion request completing a command's name", []string{"__complete", "--pretty", "compile"},
			[]string{"__complete", "--pretty", "compile"}},
		{"a completion request without descriptions", []string{"__completeNoDesc", "-o", "x", "compile", "c"},
			[]string{"__completeNoDesc", "compile", "-o", "x", "c"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			root := newRootCmd()
			root.InitDefaultHelpCmd()
			root.InitDefaultCompletionCmd()
			assert.Equal(t, tt.want, commandFirst(root, tt.args))
		})
	}
}

// TestNextCommand_AnAliasNamesItsCommand pins that a word is matched against a
// command's aliases as well as its name, as cobra matches it.
func TestNextCommand_AnAliasNamesItsCommand(t *testing.T) {
	t.Parallel()
	root := newRootCmd()
	validate := subcommand(root, "validate")
	require.NotNil(t, validate)
	validate.Aliases = []string{"check"}

	sub, at, ok := nextCommand(root, []string{"--verify", "check", "s"})
	require.True(t, ok)
	assert.Same(t, validate, sub)
	assert.Equal(t, 1, at)
	assert.Nil(t, subcommand(root, "bogus"))
}
