package irtest_test

import (
	"os"
	"path/filepath"
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
