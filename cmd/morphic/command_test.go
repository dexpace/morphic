package main

import (
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

func TestNewCompileFlags_DefinesEveryFlag(t *testing.T) {
	t.Parallel()

	fs, opts := newCompileFlags()
	require.NotNil(t, opts)

	for _, name := range []string{"o", "fail-on", "skip-validate"} {
		assert.NotNil(t, fs.Lookup(name), "flag %q must be defined", name)
	}
	assert.Equal(t, "error", opts.failOn, "default --fail-on")
	assert.Empty(t, opts.outPath)
	assert.False(t, opts.skipValidate)
}
