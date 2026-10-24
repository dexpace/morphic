package irtest_test

import (
	"errors"
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

// runRootFrom drives rootFrom in a goroutine so a Goexit from Fatalf unwinds
// only that goroutine, then returns the recorder and what rootFrom returned.
func runRootFrom(getwd func() (string, error)) (*recordingT, string) {
	rec := &recordingT{}
	var got string
	done := make(chan struct{})
	go func() {
		defer close(done)
		got = irtest.RootFrom(rec, getwd)
	}()
	<-done
	return rec, got
}

// TestRootFrom_RefusesAWorkingDirectoryItCannotRead pins the first failure:
// the walk has nowhere to start, so the test stops rather than guessing one.
func TestRootFrom_RefusesAWorkingDirectoryItCannotRead(t *testing.T) {
	t.Parallel()
	rec, got := runRootFrom(func() (string, error) { return "", errors.New("gone") })
	assert.True(t, rec.aborted)
	assert.Contains(t, rec.fatalf, "reading the working directory: gone")
	assert.Empty(t, got)
	assert.GreaterOrEqual(t, rec.helperCalls, 1, "rootFrom marks itself a helper")
}

// TestRootFrom_RefusesADirectoryOutsideAnyModule pins the second: a walk that
// reaches the filesystem root without a go.mod stops there.
func TestRootFrom_RefusesADirectoryOutsideAnyModule(t *testing.T) {
	t.Parallel()
	outside := t.TempDir()
	rec, got := runRootFrom(func() (string, error) { return outside, nil })
	assert.True(t, rec.aborted)
	assert.Contains(t, rec.fatalf, "no go.mod in or above the working directory")
	assert.Empty(t, got)
	assert.GreaterOrEqual(t, rec.helperCalls, 1, "rootFrom marks itself a helper")
}
