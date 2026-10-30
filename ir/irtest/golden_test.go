// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package irtest_test

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/ir/irtest"
)

// TestWriteGolden_RejectsBadInput pins the two preconditions. The nil-document
// case is the one with teeth: an unguarded WriteGolden encoded nil to "null",
// left that on disk as a golden, and CompareGolden then matched every later nil
// document against it — a snapshot asserting nothing, indistinguishable from a
// passing one.
func TestWriteGolden_RejectsBadInput(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	cases := map[string]struct {
		path    string
		doc     *ir.Document
		wantErr string
	}{
		"empty path":   {path: "", doc: &ir.Document{Name: "x"}, wantErr: "empty path"},
		"nil document": {path: filepath.Join(dir, "nil.golden.json"), doc: nil, wantErr: "nil document"},
		"unstamped document": {path: filepath.Join(dir, "unstamped.golden.json"), doc: &ir.Document{Name: "x"},
			wantErr: `document stamped irVersion "", not ir.IRVersion ` + ir.IRVersion},
		"document stamped with another version": {path: filepath.Join(dir, "foreign.golden.json"),
			doc:     &ir.Document{IRVersion: "0.0.1", Name: "x"},
			wantErr: `document stamped irVersion "0.0.1", not ir.IRVersion ` + ir.IRVersion},
	}
	// Sequential subtests: the file-absence assertion below has to observe the
	// state the nil-document case left, and a parallel subtest runs after its
	// parent's body returns.
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			err := irtest.WriteGolden(tc.path, tc.doc)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.wantErr)
		})
	}

	for _, name := range []string{"nil.golden.json", "unstamped.golden.json", "foreign.golden.json"} {
		_, err := os.Stat(filepath.Join(dir, name))
		assert.ErrorIs(t, err, os.ErrNotExist, "a refused write leaves no golden behind: %s", name)
	}
}

func TestCompareGolden_WritesThenMatches(t *testing.T) {
	// Not parallel: exercises the -update path via WriteGolden.
	dir := t.TempDir()
	path := filepath.Join(dir, "doc.golden.json")
	doc := &ir.Document{IRVersion: ir.IRVersion, Name: "g", Version: "1"}

	// First write the golden explicitly, then compare against it.
	require.NoError(t, irtest.WriteGolden(path, doc))
	irtest.CompareGolden(t, path, doc)

	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.True(t, len(raw) > 0 && raw[len(raw)-1] == '\n', "golden must end in newline")
}

// TestWriteGolden_LeavesTheStampToReadGolden pins the golden form: the
// document's encoding without its irVersion member, which ReadGolden puts
// back, so the round trip returns the document written.
func TestWriteGolden_LeavesTheStampToReadGolden(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "doc.golden.json")
	doc := &ir.Document{IRVersion: ir.IRVersion, Name: "g", Version: "1"}
	require.NoError(t, irtest.WriteGolden(path, doc))

	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "{\n  \"name\": \"g\",\n  \"version\": \"1\",\n  \"docs\": {}\n}\n", string(raw))

	back, err := irtest.ReadGolden(path)
	require.NoError(t, err)
	assert.Empty(t, cmp.Diff(doc, back))
}

// TestReadGolden_Stamps reads goldens the writer could have produced, an
// empty one and one padded with whitespace included, and finds each stamped.
func TestReadGolden_Stamps(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	for name, tc := range map[string]struct {
		golden string
		want   ir.Document
	}{
		"an empty document": {golden: "{}", want: ir.Document{IRVersion: ir.IRVersion}},
		"an empty document padded": {golden: " \t{ \r\n}\n",
			want: ir.Document{IRVersion: ir.IRVersion}},
		"a padded document": {golden: "\n{ \"name\": \"n\" }\n",
			want: ir.Document{IRVersion: ir.IRVersion, Name: "n"}},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			path := filepath.Join(dir, name+".golden.json")
			require.NoError(t, os.WriteFile(path, []byte(tc.golden), 0o644))
			got, err := irtest.ReadGolden(path)
			require.NoError(t, err)
			assert.Empty(t, cmp.Diff(&tc.want, got))
		})
	}
}

// TestReadGolden_Refusals holds ReadGolden to the golden form and to every
// refusal of the Document decoder, which reads what it stamps.
func TestReadGolden_Refusals(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	for name, tc := range map[string]struct {
		golden string // "" writes no file
		want   string
	}{
		"no file":                  {want: "irtest: read golden: open "},
		"an empty file":            {golden: "\n", want: "not a JSON object"},
		"null":                     {golden: "null\n", want: "not a JSON object"},
		"an array":                 {golden: "[]\n", want: "not a JSON object"},
		"a golden storing a stamp": {golden: `{"irVersion":"` + ir.IRVersion + `"}`, want: `duplicate object member name "irVersion"`},
		"an unknown member":        {golden: `{"bogus":1}`, want: `unknown object member name "bogus"`},
		"an unterminated object":   {golden: `{"name":"n"`, want: "unexpected EOF"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			path := filepath.Join(dir, name+".golden.json")
			if tc.golden != "" {
				require.NoError(t, os.WriteFile(path, []byte(tc.golden), 0o644))
			}
			got, err := irtest.ReadGolden(path)
			require.Error(t, err)
			assert.Nil(t, got)
			assert.Contains(t, err.Error(), tc.want)
		})
	}
}

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
		irtest.CompareGoldenOn(rec, goldenPath, doc)
	}()
	<-done
	return rec
}

// badExtDoc marshals with an error: an invalid jsontext.Value in Unmodeled
// makes encodeGolden's json.Marshal fail.
func badExtDoc() *ir.Document {
	return &ir.Document{IRVersion: ir.IRVersion, Unmodeled: ir.Unmodeled{"x": {Value: ir.RawValue("{invalid")}}}
}

// withUpdate sets the -update flag for the duration of fn and restores it.
func withUpdate(t *testing.T, v bool, fn func()) {
	t.Helper()
	prev := *irtest.UpdateFlag
	*irtest.UpdateFlag = v
	defer func() { *irtest.UpdateFlag = prev }()
	fn()
}

func TestCompareGolden_UpdateWritesAndReturns(t *testing.T) {
	// Not parallel: toggles the shared -update flag.
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "doc.golden.json")
	doc := &ir.Document{IRVersion: ir.IRVersion, Name: "u"}

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
	doc := &ir.Document{IRVersion: ir.IRVersion, Name: "x"}

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
	doc := &ir.Document{IRVersion: ir.IRVersion, Name: "different"}

	withUpdate(t, false, func() {
		rec := runCompare(path, doc)
		require.False(t, rec.aborted, "a diff mismatch reports via Errorf, not Fatalf")
		assert.Contains(t, rec.errorf, "golden mismatch")
	})
}

func TestWriteGolden_MarshalError(t *testing.T) {
	t.Parallel()
	err := irtest.WriteGolden(filepath.Join(t.TempDir(), "doc.json"), badExtDoc())
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

	err := irtest.WriteGolden(path, &ir.Document{IRVersion: ir.IRVersion, Name: "x"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "mkdir for golden")
}

func TestWriteGolden_WriteError(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	// The target path is itself a directory, so WriteFile cannot write to it.
	path := filepath.Join(dir, "asdir")
	require.NoError(t, os.Mkdir(path, 0o755))

	err := irtest.WriteGolden(path, &ir.Document{IRVersion: ir.IRVersion, Name: "x"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "write golden")
}

// TestCompareGolden_RejectsBadInput drives the preconditions. They sit ahead of
// the -update branch, so neither case needs the flag toggled to reach them.
//
// The nil-document case is a regression test with a written-out failure mode: the
// golden on disk is the "null" an unguarded WriteGolden produces, and an
// unguarded compareGolden encodes nil to the same four bytes and reports a clean
// match. The abort is the only thing separating that from a real comparison.
//
// The stamp cases would compare clean against the golden on disk, which
// leaves the stamp out, but for the refusal.
func TestCompareGolden_RejectsBadInput(t *testing.T) {
	dir := t.TempDir()
	nullGolden := filepath.Join(dir, "null.golden.json")
	require.NoError(t, os.WriteFile(nullGolden, []byte("null\n"), 0o644))
	golden := filepath.Join(dir, "doc.golden.json")
	require.NoError(t, irtest.WriteGolden(golden, &ir.Document{IRVersion: ir.IRVersion, Name: "x"}))

	cases := map[string]struct {
		path      string
		doc       *ir.Document
		wantFatal string
	}{
		"empty path":   {path: "", doc: &ir.Document{Name: "x"}, wantFatal: "empty golden path"},
		"nil document": {path: nullGolden, doc: nil, wantFatal: "nil document"},
		"unstamped document": {path: golden, doc: &ir.Document{Name: "x"},
			wantFatal: `document stamped irVersion "", not ir.IRVersion ` + ir.IRVersion},
		"document stamped with another version": {path: golden, doc: &ir.Document{IRVersion: "0.0.1", Name: "x"},
			wantFatal: `document stamped irVersion "0.0.1", not ir.IRVersion ` + ir.IRVersion},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			rec := runCompare(tc.path, tc.doc)
			require.True(t, rec.aborted, "a precondition failure must abort via Fatalf")
			assert.Contains(t, rec.fatalf, tc.wantFatal)
			assert.Empty(t, rec.errorf, "the abort happens before any comparison")
		})
	}
}

// TestCompareGolden_UpdateRefusesAnUnstampedDocument holds the stamp check ahead
// of -update, so rewriting goldens cannot store a snapshot of a document that
// was never stamped.
func TestCompareGolden_UpdateRefusesAnUnstampedDocument(t *testing.T) {
	// Not parallel: toggles the shared -update flag.
	path := filepath.Join(t.TempDir(), "doc.golden.json")

	withUpdate(t, true, func() {
		rec := runCompare(path, &ir.Document{Name: "x"})
		require.True(t, rec.aborted, "an unstamped document must abort via Fatalf")
		assert.Contains(t, rec.fatalf, "not ir.IRVersion")
	})
	_, err := os.Stat(path)
	assert.ErrorIs(t, err, os.ErrNotExist, "a refused update leaves no golden behind")
}

func TestCompareGolden_HelperIsCalled(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "doc.golden.json")
	require.NoError(t, os.WriteFile(path, []byte("{}\n"), 0o644))

	withUpdate(t, false, func() {
		rec := runCompare(path, &ir.Document{IRVersion: ir.IRVersion})
		assert.GreaterOrEqual(t, rec.helperCalls, 1, "compareGolden marks itself a helper")
	})
}
