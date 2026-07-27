package harness_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/internal/harness"
	"github.com/dexpace/morphic/internal/testspec"
)

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
	goodPath := writeSpec(t, dir, "good.yaml", testspec.Minimal)
	brokenPath := writeSpec(t, dir, "broken.yaml", testspec.BadHeader)
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
	goodPath := writeSpec(t, dir, "good.yaml", testspec.Minimal)

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
	if os.Geteuid() == 0 {
		t.Skip("root bypasses permission bits, so a chmod 0o000 file stays readable")
	}
	// A regular file with no read permission stats cleanly (so it is not a
	// directory) but fails to read, so CheckPath returns the read error.
	dir := t.TempDir()
	path := writeSpec(t, dir, "spec.yaml", testspec.Minimal)
	require.NoError(t, os.Chmod(path, 0o000))
	t.Cleanup(func() { _ = os.Chmod(path, 0o600) })

	_, err := harness.CheckPath(context.Background(), path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "harness: read")
}
