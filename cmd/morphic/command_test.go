// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package main

import (
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// compileFlagNames and validateFlagNames are every flag each command defines,
// asserted from the command that declares them so the documented set and the
// parsed set cannot drift. help is cobra's and is added when a command runs.
var (
	compileFlagNames  = []string{"output", "fail-on", "skip-validate", "disable-pass", "verify", "explain", "pretty", "opt", "cache-dir"}
	validateFlagNames = []string{"fail-on", "skip-validate", "disable-pass", "verify", "opt", "cache-dir"}
)

func flagNames(cmd *cobra.Command) []string {
	var names []string
	cmd.Flags().VisitAll(func(f *pflag.Flag) { names = append(names, f.Name) })
	return names
}

// TestNewRootCmd_CommandsAreWellFormed holds the invariants running a command
// relies on rather than re-checking them on every call: a blank name could never
// be typed, a duplicate would be resolved to one of the two without a word, and
// a command without a RunE, an Args check or text for help fails only when
// someone runs it.
func TestNewRootCmd_CommandsAreWellFormed(t *testing.T) {
	t.Parallel()

	cmds := newRootCmd().Commands()
	require.NotEmpty(t, cmds, "the command tree must not be empty")

	names := make([]string, 0, len(cmds))
	for _, c := range cmds {
		require.NotEmpty(t, c.Name(), "every command needs a name")
		require.NotContains(t, names, c.Name(), "duplicate command %q", c.Name())
		names = append(names, c.Name())

		assert.NotNil(t, c.RunE, "%s has no RunE, so running it does nothing", c.Name())
		assert.NotNil(t, c.Args, "%s has no Args check, so it accepts any operands", c.Name())
		assert.NotEmpty(t, c.Short, "%s needs a summary for the root command list", c.Name())
		assert.NotEmpty(t, c.Long, "%s needs a description", c.Name())
		assert.NotEmpty(t, c.Example, "%s needs an example", c.Name())
	}
	assert.ElementsMatch(t, []string{"compile", "validate"}, names)
}

func TestCompileCmd_DefinesEveryFlag(t *testing.T) {
	t.Parallel()

	cmd := newCompileCmd()
	assert.ElementsMatch(t, compileFlagNames, flagNames(cmd),
		"compileFlagNames must be exactly the flags the constructor defines")

	assert.Equal(t, "error", cmd.Flags().Lookup("fail-on").DefValue, "default --fail-on")
	assert.Equal(t, "o", cmd.Flags().Lookup("output").Shorthand, "-o is --output's short form")
	assert.Empty(t, cmd.Flags().Lookup("output").DefValue)
	assert.Equal(t, "false", cmd.Flags().Lookup("skip-validate").DefValue)
	assert.Equal(t, "false", cmd.Flags().Lookup("verify").DefValue, "a run verifies only when asked")
}

func TestValidateCmd_DefinesEveryFlag(t *testing.T) {
	t.Parallel()

	cmd := newValidateCmd()
	assert.ElementsMatch(t, validateFlagNames, flagNames(cmd),
		"validateFlagNames must be exactly the flags the constructor defines")

	assert.Equal(t, "error", cmd.Flags().Lookup("fail-on").DefValue, "default --fail-on")
	assert.Equal(t, "false", cmd.Flags().Lookup("skip-validate").DefValue)
}

// TestSpecFlags_SharedFlagsAgree pins that the flags both spec-taking commands
// take are one definition and not two that currently read alike: a default or a
// help string that drifted would make the same flag mean different things
// depending on which command it was typed after.
func TestSpecFlags_SharedFlagsAgree(t *testing.T) {
	t.Parallel()

	compile, validate := newCompileCmd().Flags(), newValidateCmd().Flags()
	for _, name := range validateFlagNames {
		mine, theirs := validate.Lookup(name), compile.Lookup(name)
		require.NotNil(t, mine, "validate must define --%s", name)
		require.NotNil(t, theirs, "compile must define --%s", name)
		assert.Equal(t, theirs.Usage, mine.Usage, "help text for --%s", name)
		assert.Equal(t, theirs.DefValue, mine.DefValue, "default for --%s", name)
	}
}

// TestFlags_EveryFlagHasUsageText holds the property help depends on: a flag
// without usage text is a bare name in the table, and pflag reads a backquoted
// word in it as the name of the flag's value.
func TestFlags_EveryFlagHasUsageText(t *testing.T) {
	t.Parallel()

	for _, cmd := range newRootCmd().Commands() {
		cmd.Flags().VisitAll(func(f *pflag.Flag) {
			assert.NotEmpty(t, f.Usage, "%s --%s has no usage text", cmd.Name(), f.Name)
			assert.NotContains(t, f.Usage, "`",
				"%s --%s: pflag would take a backquoted word for the value's name", cmd.Name(), f.Name)
		})
	}
}
