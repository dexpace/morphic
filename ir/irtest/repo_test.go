package irtest_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir/irtest"
)

// TestRepoRoot_FindsTheModuleAboveTheWorkingDirectory pins the walk on the
// real tree: the root holds go.mod, and the working directory lies under it.
func TestRepoRoot_FindsTheModuleAboveTheWorkingDirectory(t *testing.T) {
	t.Parallel()
	root := irtest.RepoRoot(t)
	_, err := os.Stat(filepath.Join(root, "go.mod"))
	require.NoError(t, err)
	wd, err := os.Getwd()
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(wd, root), "%s lies under %s", wd, root)
}
