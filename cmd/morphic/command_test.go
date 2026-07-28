package main

import (
	"flag"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLookup_KnownAndUnknown(t *testing.T) {
	t.Parallel()

	c, ok := lookup("compile")
	require.True(t, ok, "compile must be in the command table")
	assert.Equal(t, "compile", c.name)
	assert.NotEmpty(t, c.summary, "every command needs a summary for the root help list")
	assert.NotEmpty(t, c.usage)
	assert.NotEmpty(t, c.description)
	require.NotNil(t, c.flagSet)
	require.NotNil(t, c.run)

	_, ok = lookup("bogus")
	assert.False(t, ok)

	_, ok = lookup("")
	assert.False(t, ok, "the empty name must never resolve")
}

// TestCommands_TableIsWellFormed holds the invariants lookup relies on rather
// than re-checking them at every call: a blank name would make lookup("")
// resolve, and a duplicate name would be resolved to the first entry without a
// word. Both are mistakes in the table, so this is where they can fail.
func TestCommands_TableIsWellFormed(t *testing.T) {
	t.Parallel()

	require.NotEmpty(t, commands, "the command table must not be empty")
	seen := make(map[string]bool, len(commands))
	for _, c := range commands {
		require.NotEmpty(t, c.name, "every command needs a name")
		require.False(t, seen[c.name], "duplicate command %q", c.name)
		seen[c.name] = true
	}
}

func TestNewCompileFlags_DefinesEveryFlag(t *testing.T) {
	t.Parallel()

	fs, opts := newCompileFlags()
	require.NotNil(t, opts)

	var got []string
	fs.VisitAll(func(f *flag.Flag) { got = append(got, f.Name) })
	assert.ElementsMatch(t, compileFlagNames, got,
		"compileFlagNames must be exactly the flags the constructor defines")

	assert.Equal(t, "error", opts.failOn, "default --fail-on")
	assert.Empty(t, opts.outPath)
	assert.False(t, opts.skipValidate)
	assert.Empty(t, opts.explain)
}

// compileFlagNames is every flag compile accepts, asserted from both the
// constructor and the command-table entry so the two cannot drift.
var compileFlagNames = []string{"o", "fail-on", "skip-validate", "explain"}

func TestCommand_FlagSetBindsTheCommandsOwnFlags(t *testing.T) {
	t.Parallel()

	c, ok := lookup("compile")
	require.True(t, ok)

	// The table's flagSet is what help rendering reads. Today it just calls
	// newCompileFlags, so nothing can drift yet; this holds the line if the
	// table ever gets its own.
	fs := c.flagSet()
	require.NotNil(t, fs)

	var got []string
	fs.VisitAll(func(f *flag.Flag) { got = append(got, f.Name) })
	assert.ElementsMatch(t, compileFlagNames, got)
}
