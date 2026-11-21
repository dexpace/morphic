// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// locateSpec names a document by a URL that a mapping puts beside it.
const locateSpec = "openapi: 3.1.0\ninfo: {title: t, version: \"1\"}\npaths: {}\ncomponents:\n  schemas:\n" +
	"    p: {$ref: 'https://example.com/x/b.yaml#/B'}\n"

// TestRun_LocateOptionMapsAURLPrefixToADirectory holds the option's text form
// to the mapping it writes: with it the document beside the spec is read for
// the URL and the compile is clean, without it the URL is refused as on the
// network, and a prefix with no final slash is refused as an option.
func TestRun_LocateOptionMapsAURLPrefixToADirectory(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	spec := filepath.Join(dir, "spec.yaml")
	require.NoError(t, os.WriteFile(spec, []byte(locateSpec), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "b.yaml"), []byte("B: {type: string}\n"), 0o600))

	var stdout, stderr bytes.Buffer
	code := run(t.Context(), []string{"validate", spec, "--compiler-option", "locate=https://example.com/x/=."}, &stdout, &stderr)
	assert.Equal(t, 0, code, "stderr: %s", stderr.String())
	assert.Empty(t, stderr.String())

	stdout.Reset()
	stderr.Reset()
	code = run(t.Context(), []string{"validate", spec}, &stdout, &stderr)
	assert.Equal(t, 1, code)
	assert.Contains(t, stderr.String(), "it is on the network")
	assert.Contains(t, stderr.String(), "a locate option maps its URL to a location")

	stdout.Reset()
	stderr.Reset()
	code = run(t.Context(), []string{"validate", spec, "--compiler-option", "locate=https://example.com/x=."}, &stdout, &stderr)
	assert.NotEqual(t, 0, code)
	assert.Contains(t, stderr.String(), "locate")
}
