package main

import (
	"bytes"
	"flag"
	"strings"
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
	require.NotNil(t, c.printFlags)
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

	table := commands()
	require.NotEmpty(t, table, "the command table must not be empty")
	seen := make(map[string]bool, len(table))
	for _, c := range table {
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

func TestCommand_PrintFlagsDocumentsTheCommandsOwnFlags(t *testing.T) {
	t.Parallel()

	c, ok := lookup("compile")
	require.True(t, ok)
	require.NotNil(t, c.printFlags)

	var buf bytes.Buffer
	c.printFlags(&buf)
	require.NotEmpty(t, buf.String(), "printFlags must render something")

	// Read back out of the render rather than off a FlagSet: the render is what
	// a user sees, and it is all the table hands out.
	assert.ElementsMatch(t, compileFlagNames, flagNamesIn(buf.String()),
		"the rendered flag table must document exactly the flags compile accepts")
}

// flagNamesIn returns the flag names documented by a rendered flag table.
// PrintDefaults writes each flag as "  -name ..." above an indented description
// line, so a line carrying the "  -" prefix names a flag and nothing else does.
func flagNamesIn(rendered string) []string {
	var names []string
	for _, line := range strings.Split(rendered, "\n") {
		rest, ok := strings.CutPrefix(line, "  -")
		if !ok {
			continue
		}
		name, _, _ := strings.Cut(rest, " ")
		names = append(names, name)
	}
	return names
}
