// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/internal/harness"
	"github.com/dexpace/morphic/internal/testspec"
)

func writeSpec(t *testing.T, dir, name, content string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600))
}

func TestRun_AllOKExitsZero(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeSpec(t, dir, "a.yaml", testspec.Minimal)
	writeSpec(t, dir, "b.yaml", testspec.Minimal)

	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{dir}, &stdout, &stderr)

	require.Equal(t, 0, code, "stderr: %s", stderr.String())
	assert.Contains(t, stdout.String(), "a.yaml")
	assert.Contains(t, stdout.String(), "b.yaml")
}

// TestRun_PrintsWhatEachOracleReached pins the summary after the report: a
// row per oracle, counting what it asked and declined, so a spec an oracle
// could not ask is never read as one it passed. A key written twice compiles,
// last declaration winning, and is no spec the order oracle can permute.
func TestRun_PrintsWhatEachOracleReached(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeSpec(t, dir, "a.yaml", testspec.Minimal)
	writeSpec(t, dir, "dup.yaml", testspec.Minimal+"info: {title: again, version: \"2\"}\n")

	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{dir}, &stdout, &stderr)

	require.Equal(t, 0, code, "stderr: %s", stderr.String())
	report, summary, found := strings.Cut(stdout.String(), "\n\n")
	require.True(t, found, "a blank line parts the report from the summary: %s", stdout.String())
	assert.Contains(t, report, "a.yaml")
	assert.Equal(t, harness.Summary(mustCheck(t, dir)), summary)
	assert.Contains(t, summary, fmt.Sprintf("%-25s %6d %6d %8d  duplicate key 1\n", "order-invariance", 1, 0, 1))
	assert.Contains(t, summary, fmt.Sprintf("%-25s %6d %6d %8d  nothing to permute 2\n", "sequence-order-invariance", 0, 0, 2))
}

// mustCheck sweeps dir as run does.
func mustCheck(t *testing.T, dir string) []harness.Result {
	t.Helper()
	results, err := harness.CheckPath(context.Background(), dir)
	require.NoError(t, err)
	return results
}

func TestRun_AnyFailureExitsOne(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeSpec(t, dir, "good.yaml", testspec.Minimal)
	writeSpec(t, dir, "bad.yaml", testspec.BadHeader)

	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{dir}, &stdout, &stderr)

	assert.Equal(t, 1, code, "a failing spec must gate the exit code")
}

func TestRun_SpecNoCompilerServesExitsOne(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeSpec(t, dir, "swagger.yaml", "swagger: \"2.0\"\n")

	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{dir}, &stdout, &stderr)

	assert.Equal(t, 1, code, "a recognized format nobody serves is a finding")
	assert.Contains(t, stdout.String(), "unrecognized")
	assert.Contains(t, stdout.String(), "swagger@2.0")
}

func TestRun_FilesNoCompilerRecognizesAreNotSwept(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeSpec(t, dir, "notes.txt", "not a spec")

	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{dir}, &stdout, &stderr)

	assert.Equal(t, 0, code, "stderr: %s", stderr.String())
	assert.Empty(t, stdout.String(), "nothing in the directory is a spec, so there is nothing to report")
}

func TestRun_MissingPathExitsTwo(t *testing.T) {
	t.Parallel()
	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{filepath.Join(t.TempDir(), "nope")}, &stdout, &stderr)

	assert.Equal(t, 2, code)
	assert.Contains(t, stderr.String(), "morphic-harness:")
}

func TestRun_NoArgsExitsTwo(t *testing.T) {
	t.Parallel()
	var stdout, stderr bytes.Buffer
	code := run(context.Background(), nil, &stdout, &stderr)

	assert.Equal(t, 2, code)
	assert.Contains(t, stderr.String(), "usage")
}

func TestMain_ExitCode(t *testing.T) {
	origArgs, origExit := os.Args, osExit
	t.Cleanup(func() { os.Args, osExit = origArgs, origExit })

	var got int
	osExit = func(code int) { got = code }
	os.Args = []string{"morphic-harness"} // no path args → usage → exit 2

	main()

	assert.Equal(t, 2, got)
}
