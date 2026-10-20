package atomicfile_test

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/engine/internal/atomicfile"
)

// The causes the injected failures return, matched with errors.Is rather than
// by text.
var (
	errFill   = errors.New("fill refused")
	errDisk   = errors.New("disk gone")
	errSync   = errors.New("sync refused")
	errClose  = errors.New("close refused")
	errChmod  = errors.New("chmod refused")
	errRename = errors.New("rename refused")
	errCreate = errors.New("create refused")
)

// trace records the steps of one replace in the order they ran.
type trace struct{ steps []string }

func (tr *trace) add(step string) { tr.steps = append(tr.steps, step) }

// flakyFile is a real temporary file, so cleaning it up shows on disk. It fails
// the steps its fields name and records each step asked of it.
type flakyFile struct {
	*os.File
	writeErr, syncErr, closeErr, chmodErr error
	// chmods records each mode Chmod was asked to set.
	chmods []fs.FileMode
	// onFail runs just before a step returns its injected error, which is how a
	// test makes the cleanup that follows fail too.
	onFail  func()
	trace   *trace
	name    string
	written bytes.Buffer
	closes  int
}

// create is a Replacer.Create that creates exclusively, as the default does,
// and hands back f over the new file.
func (f *flakyFile) create(name string, perm fs.FileMode) (atomicfile.File, error) {
	file, err := os.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, perm)
	if err != nil {
		return nil, err
	}
	f.File, f.name = file, name
	return f, nil
}

func (f *flakyFile) note(step string) {
	if f.trace != nil {
		f.trace.add(step)
	}
}

func (f *flakyFile) fail(err error) error {
	if f.onFail != nil {
		f.onFail()
	}
	return err
}

func (f *flakyFile) Write(p []byte) (int, error) {
	f.note("write")
	if f.writeErr != nil {
		return 0, f.fail(f.writeErr)
	}
	f.written.Write(p)
	return f.File.Write(p)
}

func (f *flakyFile) Chmod(mode fs.FileMode) error {
	f.note("chmod")
	f.chmods = append(f.chmods, mode)
	if f.chmodErr != nil {
		return f.fail(f.chmodErr)
	}
	return f.File.Chmod(mode)
}

func (f *flakyFile) Sync() error {
	f.note("sync")
	if f.syncErr != nil {
		return f.fail(f.syncErr)
	}
	return f.File.Sync()
}

func (f *flakyFile) Close() error {
	f.closes++
	f.note("close")
	err := f.File.Close() // the descriptor is released whatever the test injects
	if f.closeErr != nil {
		return f.fail(f.closeErr)
	}
	return err
}

// createExclusive is the operating system's Create, for a spy that records
// what it was asked and then delegates.
func createExclusive(name string, perm fs.FileMode) (atomicfile.File, error) {
	return os.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, perm)
}

// writing returns a fill that writes data and succeeds. It calls Write itself:
// io.WriteString would take a file's own WriteString, past a wrapper's Write.
func writing(data string) func(io.Writer) error {
	return func(w io.Writer) error {
		_, err := w.Write([]byte(data))
		return err
	}
}

// tempName is the name Replace derives for dest from a drawn suffix.
func tempName(dest string, suffix uint64) string {
	return filepath.Join(filepath.Dir(dest), fmt.Sprintf(".%s.tmp%016x", filepath.Base(dest), suffix))
}

// newDest returns a destination path in a fresh directory, without creating it.
func newDest(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "ir.json")
}

// seed writes content to path with exactly mode, whatever the umask narrows.
func seed(t *testing.T, path, content string, mode fs.FileMode) {
	t.Helper()
	require.NoError(t, os.WriteFile(path, []byte(content), mode))
	require.NoError(t, os.Chmod(path, mode))
}

// contentOf returns the content of the file at path.
func contentOf(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	return string(raw)
}

// modeOf returns the permission bits of the file at path, following links.
func modeOf(t *testing.T, path string) fs.FileMode {
	t.Helper()
	info, err := os.Stat(path)
	require.NoError(t, err)
	return info.Mode().Perm()
}

// entriesIn lists the names in dir, in the order os.ReadDir returns them.
func entriesIn(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}
