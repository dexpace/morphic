// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package main

import (
	"bytes"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/engine"
)

// TestHelp_NamesEveryFormatTheEngineCompiles derives the formats from the
// engine rather than spelling them, so a compiler registered later is named by
// the help of each command that reads a spec without an edit here.
func TestHelp_NamesEveryFormatTheEngineCompiles(t *testing.T) {
	t.Parallel()
	eng, err := engine.New()
	require.NoError(t, err)
	formats := eng.Formats()
	require.NotEmpty(t, formats)

	for _, command := range []string{"compile", "validate"} {
		var stdout, stderr bytes.Buffer
		require.Equal(t, 0, run(t.Context(), []string{command, "--help"}, &stdout, &stderr))
		for _, f := range formats {
			assert.Contains(t, stdout.String(), f.String(), "%s help names %s", command, f)
		}
	}
}

// TestHelp_SaysWhyFormatsCannotBeListed covers an engine that cannot be built:
// every run would report the failure, and the help says so rather than naming
// no format at all. It swaps newEngine, so it does not run in parallel.
func TestHelp_SaysWhyFormatsCannotBeListed(t *testing.T) {
	orig := newEngine
	t.Cleanup(func() { newEngine = orig })
	newEngine = func(...engine.Option) (*engine.Engine, error) { return nil, errors.New("no compilers") }

	assert.Equal(t, "The formats this build compiles cannot be listed: no compilers.", servedFormats())
}
