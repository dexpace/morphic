package harness_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/internal/harness"
)

// brokenSpec is a syntactically valid YAML document that is not a usable OpenAPI
// spec: the response header's required is the string "notabool", mirroring the
// committed testdata/openapi/resolve_target_invalid.yaml fixture. It surfaces as
// a non-OK outcome, so a sweep flags it.
const brokenSpec = `openapi: 3.1.0
info: {title: Broken, version: "1"}
paths: {}
components:
  responses:
    Bad:
      headers:
        X:
          schema: {type: string}
          required: notabool
`

// writeSpec writes content to name inside dir, failing the test on a write
// error.
func writeSpec(t *testing.T, dir, name, content string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(p, []byte(content), 0o600))
	return p
}

func TestCheckPath_DirectorySweepsGoodAndBroken(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	goodPath := writeSpec(t, dir, "good.yaml", minimalSpec)
	brokenPath := writeSpec(t, dir, "broken.yaml", brokenSpec)
	// A golden snapshot in the same directory must be skipped, not checked.
	writeSpec(t, dir, "good.golden.json", "{}")

	results, err := harness.CheckPath(context.Background(), dir)
	require.NoError(t, err)
	require.Len(t, results, 2, "sweep checks the two specs and skips the golden snapshot")

	byPath := make(map[string]harness.Result, len(results))
	for _, r := range results {
		byPath[r.Spec] = r
	}

	assert.Equal(t, harness.OutcomeOK, byPath[goodPath].Outcome, byPath[goodPath].Detail)
	assert.NotEqual(t, harness.OutcomeOK, byPath[brokenPath].Outcome,
		"the deliberately-broken spec must not pass the oracles")
}

func TestCheckPath_SingleFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	goodPath := writeSpec(t, dir, "good.yaml", minimalSpec)

	results, err := harness.CheckPath(context.Background(), goodPath)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, harness.OutcomeOK, results[0].Outcome, results[0].Detail)
}

func TestCheckPath_MissingPathIsError(t *testing.T) {
	t.Parallel()
	_, err := harness.CheckPath(context.Background(), filepath.Join(t.TempDir(), "nope.yaml"))
	require.Error(t, err)
}

func TestCheckPath_NilContextIsError(t *testing.T) {
	t.Parallel()
	//nolint:staticcheck // deliberately passing a nil ctx to exercise the boundary guard.
	_, err := harness.CheckPath(nil, t.TempDir())
	require.Error(t, err)
}

func TestCheckPath_EmptyPathIsError(t *testing.T) {
	t.Parallel()
	_, err := harness.CheckPath(context.Background(), "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "empty path")
}

func TestCheckPath_UnreadableFileIsError(t *testing.T) {
	t.Parallel()
	// A regular file with no read permission stats cleanly (so it is not a
	// directory) but fails to read, so CheckPath returns the read error.
	dir := t.TempDir()
	path := writeSpec(t, dir, "spec.yaml", minimalSpec)
	require.NoError(t, os.Chmod(path, 0o000))
	t.Cleanup(func() { _ = os.Chmod(path, 0o600) })

	_, err := harness.CheckPath(context.Background(), path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "harness: read")
}
