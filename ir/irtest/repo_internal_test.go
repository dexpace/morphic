package irtest

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
)

// runRootFrom drives rootFrom in a goroutine so a Goexit from Fatalf unwinds
// only that goroutine, then returns the recorder and what rootFrom returned.
func runRootFrom(getwd func() (string, error)) (*recordingT, string) {
	rec := &recordingT{}
	var got string
	done := make(chan struct{})
	go func() {
		defer close(done)
		got = rootFrom(rec, getwd)
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
}
