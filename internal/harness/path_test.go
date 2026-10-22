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
	var noContext context.Context // deliberately nil, to exercise the boundary guard
	_, err := harness.CheckPath(noContext, t.TempDir())
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

// brokenOpenAPI declares its version and cannot be read, so detection declines
// it with a diagnostic of its own instead of in silence.
const brokenOpenAPI = "openapi: 3.1.0\ninfo: [unclosed\n"

// swagger2 is recognized by the OpenAPI compiler as swagger@2.0, a format no
// registered compiler serves.
const swagger2 = `{"swagger": "2.0", "info": {"title": "t", "version": "1"}, "paths": {}}`

func TestSpecFiles_ListsWhatARegisteredCompilerClaims(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	// A directory named like a spec is walked into, not listed.
	require.NoError(t, os.Mkdir(filepath.Join(dir, "nested.yaml"), 0o750))
	taken := writeSpec(t, filepath.Join(dir, "nested.yaml"), "taken.yml", testspec.Minimal)
	broken := writeSpec(t, dir, "broken.yaml", brokenOpenAPI)
	unserved := writeSpec(t, dir, "swagger.json", swagger2)
	// None of these is a spec: a file no compiler recognizes, a JSON file that
	// declares nothing, a snapshot named like the spec it holds, and a spec in a
	// dot-directory.
	writeSpec(t, dir, "notes.txt", "not a spec")
	writeSpec(t, dir, "data.json", `{"a": 1}`)
	writeSpec(t, dir, "taken.golden.json", testspec.Minimal)
	require.NoError(t, os.Mkdir(filepath.Join(dir, ".hidden"), 0o750))
	writeSpec(t, filepath.Join(dir, ".hidden"), "spec.yaml", testspec.Minimal)

	files, err := harness.SpecFiles(context.Background(), dir)
	require.NoError(t, err)
	assert.Equal(t, []string{broken, taken, unserved}, files, "in walk order, the nested spec between its neighbours")

	results, err := harness.CheckPath(context.Background(), dir)
	require.NoError(t, err)
	checked := make([]string, len(results))
	for i, r := range results {
		checked[i] = r.Spec
	}
	assert.Equal(t, files, checked, "CheckPath checks what SpecFiles lists, in that order")
}

// TestSpecFiles_ReadsPastTheExtension pins that listing asks the compilers and
// not the file name: a spec is listed whatever it is called, and a name that
// looks like one lists nothing the compilers do not claim.
func TestSpecFiles_ReadsPastTheExtension(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	spec := writeSpec(t, dir, "api.txt", testspec.Minimal)
	writeSpec(t, dir, "api.yaml", "just: words\n")

	files, err := harness.SpecFiles(context.Background(), dir)
	require.NoError(t, err)
	assert.Equal(t, []string{spec}, files)
}

// TestSpecFiles_FollowsLinksToFilesAndSkipsWhatItCannotRead covers the entries
// that are not plain files. A link to a spec is a spec and a root that is a link
// to a directory is walked. A link to a directory is not descended into, and a
// socket is never opened: reading one may not return.
func TestSpecFiles_FollowsLinksToFilesAndSkipsWhatItCannotRead(t *testing.T) {
	t.Parallel()
	// A short prefix rather than t.TempDir: a unix socket path is capped near
	// 104 bytes, and t.TempDir spells this test's whole name into it.
	dir, err := os.MkdirTemp("", "harness")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dir) })

	target := writeSpec(t, dir, "target.txt", testspec.Minimal)
	link := filepath.Join(dir, "link.yaml")
	require.NoError(t, os.Symlink(target, link))
	require.NoError(t, os.Symlink(dir, filepath.Join(dir, "loop")))
	ln, err := net.Listen("unix", filepath.Join(dir, "sock.yaml"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = ln.Close() })

	root := filepath.Join(t.TempDir(), "root")
	require.NoError(t, os.Symlink(dir, root))

	files, err := harness.SpecFiles(context.Background(), root)
	require.NoError(t, err)
	assert.Equal(t, []string{
		filepath.Join(root, "link.yaml"),
		filepath.Join(root, "target.txt"),
	}, files)
}

func TestSpecFiles_ListsARegularFileWhateverItsExtension(t *testing.T) {
	t.Parallel()
	notes := writeSpec(t, t.TempDir(), "notes.txt", "x")

	files, err := harness.SpecFiles(context.Background(), notes)
	require.NoError(t, err)
	assert.Equal(t, []string{notes}, files)
}

func TestSpecFiles_Refusals(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, path, want string
	}{
		{"an empty path", "", "harness: empty path"},
		{"a path that does not exist", filepath.Join(t.TempDir(), "nope"), "harness: stat"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			files, err := harness.SpecFiles(context.Background(), tc.path)
			require.Error(t, err)
			assert.Nil(t, files)
			assert.Contains(t, err.Error(), tc.want)
		})
	}
}

func TestSpecFiles_NilContextIsRefused(t *testing.T) {
	t.Parallel()
	var noContext context.Context // deliberately nil, to exercise the boundary guard
	files, err := harness.SpecFiles(noContext, t.TempDir())
	require.Error(t, err)
	assert.Nil(t, files)
	assert.Contains(t, err.Error(), "nil context")
}

// TestSpecFiles_CanceledContextStopsTheWalk holds the walk to its caller's
// deadline: listing reads every file, so it is as long as the tree it is given.
func TestSpecFiles_CanceledContextStopsTheWalk(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeSpec(t, dir, "spec.yaml", testspec.Minimal)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	files, err := harness.SpecFiles(ctx, dir)
	require.ErrorIs(t, err, context.Canceled)
	assert.Nil(t, files)
}
