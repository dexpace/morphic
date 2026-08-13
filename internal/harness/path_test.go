package harness_test

import (
	"context"
	"net"
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

// TestCheckPath_UnreadableFileIsError drives CheckPath's single-file branch at a
// path that stats cleanly as a non-directory and still cannot be read.
//
// The unreadable thing is a unix socket rather than a chmod 0o000 regular file,
// and the difference is the point. Permission bits are advisory to root, so the
// permission form had to skip under euid 0 — which left CheckPath's `return nil,
// err` uncovered there, and a checkout that fails the 100% gate for anyone
// building as root, as a container commonly does. Refusing to open a socket for
// reading is not a permission check, so no euid bypasses it.
func TestCheckPath_UnreadableFileIsError(t *testing.T) {
	t.Parallel()
	// A short prefix rather than t.TempDir: a unix socket path is capped near
	// 104 bytes, and t.TempDir spells this test's whole name into it.
	dir, err := os.MkdirTemp("", "harness")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dir) })

	path := filepath.Join(dir, "spec.yaml")
	ln, err := net.Listen("unix", path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = ln.Close() })

	// Establish that the fixture reaches the branch it is written for: a path
	// that failed to stat, or that stat called a directory, would leave
	// CheckPath before ever calling checkFile.
	info, err := os.Stat(path)
	require.NoError(t, err)
	require.False(t, info.IsDir())

	_, err = harness.CheckPath(context.Background(), path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "harness: read")
}
