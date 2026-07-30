package irtest_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// eolProbes are paths whose line endings the golden comparison depends on: an IR
// snapshot, the spec it was compiled from, and a Go source file standing for the
// rest of the tree. Each is required to exist, so a rename cannot quietly turn
// the checks below into assertions about nothing.
var eolProbes = []string{
	"testdata/golden/openapi/petstore.golden.json",
	"testdata/conformance/openapi/named-types.yaml",
	"ir/irtest/golden.go",
}

// TestGitAttributes_PinsLineEndings asserts the repository carries the
// .gitattributes that keeps CompareGolden's byte comparison meaningful.
//
// CompareGolden compares the file on disk against JSON encodeGolden always
// writes with "\n", so a checkout that rewrote the fixtures to CRLF fails every
// golden and conformance test with a whole-file diff unrelated to the IR — the
// default on Windows, where core.autocrlf=true. Nothing else in the suite would
// notice the pin's removal: CI is Linux-only and checks out LF regardless
// (GitHub #48).
func TestGitAttributes_PinsLineEndings(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	raw, err := os.ReadFile(filepath.Join(root, ".gitattributes"))
	require.NoError(t, err, ".gitattributes must exist at the repository root")

	var pins []string
	for _, line := range strings.Split(string(raw), "\n") {
		if trimmed := strings.TrimSpace(line); trimmed != "" && !strings.HasPrefix(trimmed, "#") {
			pins = append(pins, trimmed)
		}
	}
	require.NotEmpty(t, pins, ".gitattributes carries only comments")
	assert.True(t, strings.Contains(strings.Join(pins, "\n"), "eol=lf"),
		".gitattributes must pin line endings to LF, got rules %q", pins)
}

// TestGitAttributes_ResolveToLF asks git what it would check the fixtures out
// as, rather than reading the patterns and reasoning about them. It is the half
// that survives a reformulation of the rules: any set of patterns resolving
// eol=lf for these paths passes, and any that leaves one unpinned fails however
// it is written.
func TestGitAttributes_ResolveToLF(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	for _, rel := range eolProbes {
		require.FileExists(t, filepath.Join(root, rel))
	}

	args := append([]string{"check-attr", "eol", "--"}, eolProbes...)
	cmd := exec.Command("git", args...)
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		t.Skipf("git check-attr unavailable (%v); the pin itself is checked by TestGitAttributes_PinsLineEndings", err)
	}

	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	require.Len(t, lines, len(eolProbes), "git reports one line per queried path")
	for i, line := range lines {
		assert.Equal(t, eolProbes[i]+": eol: lf", line,
			"git must check %s out with LF endings", eolProbes[i])
	}
}

// repoRoot walks up from this file to the directory holding go.mod.
func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	require.True(t, ok, "runtime.Caller failed")
	dir := filepath.Dir(file)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		require.NotEqual(t, parent, dir, "reached filesystem root without finding go.mod")
		dir = parent
	}
}
