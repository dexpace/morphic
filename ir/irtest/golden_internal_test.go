package irtest

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
)

// recordingT is a stub testingT that records Fatalf/Errorf instead of aborting
// the real test. Fatalf mimics *testing.T by aborting the calling goroutine via
// runtime.Goexit, so code after the call does not run.
type recordingT struct {
	helperCalls int
	fatalf      string
	errorf      string
	aborted     bool
}

func (r *recordingT) Helper() { r.helperCalls++ }

func (r *recordingT) Fatalf(format string, args ...any) {
	r.fatalf = fmt.Sprintf(format, args...)
	r.aborted = true
	runtime.Goexit()
}

func (r *recordingT) Errorf(format string, args ...any) {
	r.errorf = fmt.Sprintf(format, args...)
}

// runCompare drives compareGolden in a goroutine so a Goexit from Fatalf unwinds
// only that goroutine, then returns the populated recorder.
func runCompare(goldenPath string, doc *ir.Document) *recordingT {
	rec := &recordingT{}
	done := make(chan struct{})
	go func() {
		defer close(done)
		compareGolden(rec, goldenPath, doc)
	}()
	<-done
	return rec
}

// badExtDoc marshals with an error: an invalid RawMessage in Extensions makes
// encoding/json fail during (Marshal|MarshalIndent).
func badExtDoc() *ir.Document {
	return &ir.Document{Extensions: ir.Extensions{"x": json.RawMessage("{invalid")}}
}

// withUpdate sets the -update flag for the duration of fn and restores it.
func withUpdate(t *testing.T, v bool, fn func()) {
	t.Helper()
	prev := *update
	*update = v
	defer func() { *update = prev }()
	fn()
}

func TestCompareGolden_UpdateWritesAndReturns(t *testing.T) {
	// Not parallel: toggles the shared -update flag.
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "doc.golden.json")
	doc := &ir.Document{IRVersion: "0.1.0", Name: "u"}

	withUpdate(t, true, func() {
		rec := runCompare(path, doc)
		require.False(t, rec.aborted, "successful update must not abort")
		assert.Empty(t, rec.fatalf)
		assert.Empty(t, rec.errorf)
	})

	written, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.NotEmpty(t, written)
}

func TestCompareGolden_UpdateWriteErrorFatals(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "doc.golden.json")

	withUpdate(t, true, func() {
		rec := runCompare(path, badExtDoc())
		require.True(t, rec.aborted, "write failure must abort via Fatalf")
		assert.Contains(t, rec.fatalf, "update golden:")
	})
}

func TestCompareGolden_MissingGoldenFatals(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "absent.golden.json")
	doc := &ir.Document{Name: "x"}

	withUpdate(t, false, func() {
		rec := runCompare(path, doc)
		require.True(t, rec.aborted, "missing golden must abort via Fatalf")
		assert.Contains(t, rec.fatalf, "run with -update to create")
	})
}

func TestCompareGolden_MarshalErrorFatals(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "doc.golden.json")
	require.NoError(t, os.WriteFile(path, []byte("{}\n"), 0o644))

	withUpdate(t, false, func() {
		rec := runCompare(path, badExtDoc())
		require.True(t, rec.aborted, "marshal failure must abort via Fatalf")
		assert.Contains(t, rec.fatalf, "marshal document:")
	})
}

func TestCompareGolden_MismatchErrorf(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "doc.golden.json")
	require.NoError(t, os.WriteFile(path, []byte("{}\n"), 0o644))
	doc := &ir.Document{Name: "different"}

	withUpdate(t, false, func() {
		rec := runCompare(path, doc)
		require.False(t, rec.aborted, "a diff mismatch reports via Errorf, not Fatalf")
		assert.Contains(t, rec.errorf, "golden mismatch")
	})
}

func TestWriteGolden_MarshalError(t *testing.T) {
	t.Parallel()
	err := WriteGolden(filepath.Join(t.TempDir(), "doc.json"), badExtDoc())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "marshal golden")
}

func TestWriteGolden_MkdirError(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	// A regular file where a parent directory is expected makes MkdirAll fail.
	blocker := filepath.Join(dir, "file")
	require.NoError(t, os.WriteFile(blocker, []byte("x"), 0o644))
	path := filepath.Join(blocker, "sub", "doc.json")

	err := WriteGolden(path, &ir.Document{Name: "x"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "mkdir for golden")
}

func TestWriteGolden_WriteError(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	// The target path is itself a directory, so WriteFile cannot write to it.
	path := filepath.Join(dir, "asdir")
	require.NoError(t, os.Mkdir(path, 0o755))

	err := WriteGolden(path, &ir.Document{Name: "x"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "write golden")
}

func TestCompareGolden_HelperIsCalled(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "doc.golden.json")
	require.NoError(t, os.WriteFile(path, []byte("{}\n"), 0o644))

	withUpdate(t, false, func() {
		rec := runCompare(path, &ir.Document{})
		assert.GreaterOrEqual(t, rec.helperCalls, 1, "compareGolden marks itself a helper")
	})
}
