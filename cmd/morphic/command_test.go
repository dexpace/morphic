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

	for _, name := range []string{"compile", "validate"} {
		c, ok := lookup(name)
		require.True(t, ok, "%s must be in the command table", name)
		assert.Equal(t, name, c.name)
		assert.NotEmpty(t, c.summary, "every command needs a summary for the root help list")
		assert.NotEmpty(t, c.usage)
		assert.NotEmpty(t, c.description)
	}

	_, ok := lookup("bogus")
	assert.False(t, ok)

	_, ok = lookup("")
	assert.False(t, ok, "the empty name must never resolve")
}

// TestCommands_TableIsWellFormed holds the invariants lookup and dispatch rely
// on rather than re-checking them at every call: a blank name would make
// lookup("") resolve, a duplicate name would be resolved to the first entry
// without a word, and a missing bind or printFlags is a nil call the moment
// anyone types the name. All are mistakes in the table, so this is where they
// can fail.
func TestCommands_TableIsWellFormed(t *testing.T) {
	t.Parallel()

	table := commands()
	require.NotEmpty(t, table, "the command table must not be empty")
	seen := make(map[string]bool, len(table))
	for _, c := range table {
		require.NotEmpty(t, c.name, "every command needs a name")
		require.False(t, seen[c.name], "duplicate command %q", c.name)
		seen[c.name] = true
		require.NotNil(t, c.bind, "%s has no bind, so dispatch cannot run it", c.name)
		require.NotNil(t, c.printFlags, "%s has no printFlags, so help cannot render it", c.name)
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

func TestNewValidateFlags_DefinesEveryFlag(t *testing.T) {
	t.Parallel()

	fs, opts := newValidateFlags()
	require.NotNil(t, opts)

	var got []string
	fs.VisitAll(func(f *flag.Flag) { got = append(got, f.Name) })
	assert.ElementsMatch(t, validateFlagNames, got,
		"validateFlagNames must be exactly the flags the constructor defines")

	assert.Equal(t, "error", opts.failOn, "default --fail-on")
	assert.False(t, opts.skipValidate)
}

// TestSpecFlags_SharedFlagsAgree pins that the flags both spec-taking commands
// take are one definition and not two that currently read alike: a default or a
// help string that drifted would make the same flag mean different things
// depending on which command it was typed after.
func TestSpecFlags_SharedFlagsAgree(t *testing.T) {
	t.Parallel()

	compileFS, _ := newCompileFlags()
	validateFS, _ := newValidateFlags()

	for _, name := range validateFlagNames {
		mine, theirs := validateFS.Lookup(name), compileFS.Lookup(name)
		require.NotNil(t, mine, "validate must define %s", name)
		require.NotNil(t, theirs, "compile must define %s", name)
		assert.Equal(t, theirs.Usage, mine.Usage, "help text for --%s", name)
		assert.Equal(t, theirs.DefValue, mine.DefValue, "default for --%s", name)
	}
}

// compileFlagNames and validateFlagNames are every flag each command accepts,
// asserted from both the constructor and the command-table entry so the two
// cannot drift.
var (
	compileFlagNames  = []string{"o", "fail-on", "skip-validate", "explain"}
	validateFlagNames = []string{"fail-on", "skip-validate"}
)

func TestCommand_PrintFlagsDocumentsTheCommandsOwnFlags(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		flags []string
	}{
		{"compile", compileFlagNames},
		{"validate", validateFlagNames},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			c, ok := lookup(tt.name)
			require.True(t, ok)
			require.NotNil(t, c.printFlags)

			var buf bytes.Buffer
			c.printFlags(&buf)
			require.NotEmpty(t, buf.String(), "printFlags must render something")

			// Read back out of the render rather than off a FlagSet: the render is
			// what a user sees, and it is all the table hands out.
			assert.ElementsMatch(t, tt.flags, flagNamesIn(buf.String()),
				"the rendered flag table must document exactly the flags %s accepts", tt.name)
		})
	}
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
