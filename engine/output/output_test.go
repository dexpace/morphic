package output_test

import (
	"bytes"
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/engine/output"
	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/ir/irtest"
)

// TestWriteDocument_IndentedIsTheGoldenEncoding pins the Indented layout against
// an encoder this package does not share: it is byte for byte what
// irtest.WriteGolden writes for the same document, indentation and trailing
// newline included, once the irVersion member a golden leaves out is taken from
// it. Comparing it with Compact cannot show this, since both go through one
// encoder.
func TestWriteDocument_IndentedIsTheGoldenEncoding(t *testing.T) {
	t.Parallel()
	doc := orderedDoc()
	golden := filepath.Join(t.TempDir(), "doc.golden.json")
	require.NoError(t, irtest.WriteGolden(golden, doc))

	got := string(written(t, doc, output.Indented))

	stamp := fmt.Sprintf("  %q: %q,\n", "irVersion", ir.IRVersion)
	require.True(t, strings.HasPrefix(got, "{\n"+stamp), "the stamp opens the document: %s", got)
	assert.Equal(t, 1, strings.Count(got, stamp), "the stamp is written once")
	assert.Empty(t, cmp.Diff(contentOf(t, golden), strings.Replace(got, stamp, "", 1)),
		"the output must be the golden encoding with the stamp")
}

// TestWriteDocument_BothLayoutsEndInOneNewline pins the terminator: a newline
// after the document, written once. json.MarshalWrite ends no value with one,
// and a second would make a file that is not what the other layout's bytes plus
// a newline are. Compact, whose strings cannot hold a raw newline, is one line.
func TestWriteDocument_BothLayoutsEndInOneNewline(t *testing.T) {
	t.Parallel()
	for _, tt := range layouts {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := string(written(t, orderedDoc(), tt.layout))

			assert.True(t, strings.HasSuffix(got, "}\n"), "the document is followed by a newline: %q", got[len(got)-8:])
			if tt.layout == output.Compact {
				assert.Equal(t, 1, strings.Count(got, "\n"), "compact JSON is one line plus its newline")
			}
		})
	}
}

// TestWriteDocument_CompactIsTheIndentedFormWithoutWhitespace is the
// determinism check the two layouts have to survive: compacting the indented
// output reproduces the compact one byte for byte, so removing whitespace is all
// that differs. It also pins the order itself, members in the document's field
// order, map keys sorted and slices as given, and that a second call agrees with
// the first.
func TestWriteDocument_CompactIsTheIndentedFormWithoutWhitespace(t *testing.T) {
	t.Parallel()
	doc := orderedDoc()
	compact := written(t, doc, output.Compact)
	indented := written(t, doc, output.Indented)

	flattened := jsontext.Value(bytes.TrimSuffix(indented, []byte("\n"))).Clone()
	require.NoError(t, flattened.Compact())

	assert.Empty(t, cmp.Diff(flattened.String()+"\n", string(compact)),
		"compacting the indented output must reproduce the compact one exactly")
	assert.Empty(t, cmp.Diff(string(compact), string(written(t, doc, output.Compact))),
		"two calls over one document must write the same bytes")
	order := []string{`"irVersion"`, `"name"`, `"docs"`, `"types"`, `"t/Alpha"`, `"t/Beta"`, `"t/Zeta"`,
		`"tagDefs"`, `"zebra"`, `"apple"`}
	last := -1
	for _, member := range order {
		at := bytes.Index(compact, []byte(member))
		require.Greater(t, at, last, "%s must follow the member before it", member)
		last = at
	}
}

// TestWriteDocument_BadDocumentWritesNothing pins that the writer gets a whole
// document or nothing. The description runs past the encoder's internal buffer,
// so an encoder writing straight to w would already have flushed part of the
// document there by the time it reaches the malformed value.
func TestWriteDocument_BadDocumentWritesNothing(t *testing.T) {
	t.Parallel()
	for _, tt := range layouts {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var w bytes.Buffer

			err := output.WriteDocument(&w, bigBadDoc(), tt.layout)

			require.Error(t, err)
			assert.ErrorContains(t, err, "marshal ir document")
			assert.Error(t, errors.Unwrap(err), "the encoder's own error stays behind the message")
			assert.Empty(t, w.String(), "a document that will not marshal leaves the writer untouched")
		})
	}
}

// TestWriteDocument_RefusingWriter pins that a writer which refuses the bytes
// is reported as that, with its own error behind it, and not as a document that
// would not marshal.
func TestWriteDocument_RefusingWriter(t *testing.T) {
	t.Parallel()
	for _, tt := range layouts {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := output.WriteDocument(refusingWriter{}, orderedDoc(), tt.layout)

			require.ErrorIs(t, err, errDisk)
			assert.EqualError(t, err, "write ir document: disk gone")
		})
	}
}

// badCalls are the calls both functions refuse before encoding anything: no
// document, and a layout the package does not define. Writing an unknown layout
// as Compact would hide the caller's mistake.
var badCalls = []struct {
	name   string
	doc    *ir.Document
	layout output.Layout
	want   string
}{
	{"no document, compact", nil, output.Compact, "output: no document to write"},
	{"no document, indented", nil, output.Indented, "output: no document to write"},
	{"the layout after the last", orderedDoc(), output.Layout(2), "output: unknown layout 2"},
	{"a layout far past the last", orderedDoc(), output.Layout(7), "output: unknown layout 7"},
	{"a negative layout", orderedDoc(), output.Layout(-1), "output: unknown layout -1"},
}

// TestWriteDocument_RefusesAnUnusableCall pins that a refused call writes
// nothing to w. Encoding a nil document would write "null", and an unknown
// layout would write the document as one of the two, so only the empty writer
// shows the refusal came first.
func TestWriteDocument_RefusesAnUnusableCall(t *testing.T) {
	t.Parallel()
	for _, tt := range badCalls {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var w bytes.Buffer

			err := output.WriteDocument(&w, tt.doc, tt.layout)

			assert.EqualError(t, err, tt.want)
			assert.Empty(t, w.String())
		})
	}
}

// TestReplaceDocument_WritesTheLayoutItIsAsked pins the file's bytes against
// what WriteDocument writes for the same layout, over a stale destination: the
// zero Layout is Compact, so an unset layout writes the artifact format, and
// Indented writes the form a person reads.
func TestReplaceDocument_WritesTheLayoutItIsAsked(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		layout output.Layout
		same   output.Layout
	}{
		{"the zero layout", output.Layout(0), output.Compact},
		{"compact", output.Compact, output.Compact},
		{"indented", output.Indented, output.Indented},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dest := filepath.Join(t.TempDir(), "ir.json")
			require.NoError(t, os.WriteFile(dest, []byte("stale\n"), 0o644))
			doc := orderedDoc()

			require.NoError(t, output.ReplaceDocument(dest, doc, tt.layout))

			assert.Empty(t, cmp.Diff(string(written(t, doc, tt.same)), contentOf(t, dest)))
			assert.Equal(t, []string{"ir.json"}, entriesIn(t, filepath.Dir(dest)),
				"no temporary file may survive a successful replace")
		})
	}
}

// TestReplaceDocument_BadDocumentLeavesDestinationUntouched pins both halves of
// encoding into a temporary file instead of marshalling ahead of it: the file
// exists before the document is known to marshal, and a document that does not
// must leave nothing behind and the destination as it was, byte for byte.
func TestReplaceDocument_BadDocumentLeavesDestinationUntouched(t *testing.T) {
	t.Parallel()
	const existing = `{"name":"Existing"}` + "\n"
	for _, tt := range layouts {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dest := filepath.Join(t.TempDir(), "ir.json")
			require.NoError(t, os.WriteFile(dest, []byte(existing), 0o644))

			err := output.ReplaceDocument(dest, bigBadDoc(), tt.layout)

			require.Error(t, err)
			assert.ErrorContains(t, err, "marshal ir document")
			assert.Equal(t, existing, contentOf(t, dest), "a failed marshal must not truncate the destination")
			assert.Equal(t, []string{"ir.json"}, entriesIn(t, filepath.Dir(dest)),
				"a failed encode must leave no temporary file beside the destination")
		})
	}
}

// TestReplaceDocument_RefusesAnUnusableCall pins that a refused call touches no
// file: an existing destination keeps its bytes and a missing one is not
// created. Past the refusal the document would encode, as "null" or as one of
// the two layouts, and replace the file.
func TestReplaceDocument_RefusesAnUnusableCall(t *testing.T) {
	t.Parallel()
	for _, tt := range badCalls {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			dest := filepath.Join(dir, "ir.json")
			require.NoError(t, os.WriteFile(dest, []byte("PREVIOUS\n"), 0o644))

			existingErr := output.ReplaceDocument(dest, tt.doc, tt.layout)
			missingErr := output.ReplaceDocument(filepath.Join(dir, "missing.json"), tt.doc, tt.layout)

			assert.EqualError(t, existingErr, tt.want)
			assert.EqualError(t, missingErr, tt.want)
			assert.Equal(t, "PREVIOUS\n", contentOf(t, dest))
			assert.Equal(t, []string{"ir.json"}, entriesIn(t, dir))
		})
	}
}

// TestReplaceDocument_MissingParentDirectory pins that a destination in a
// directory that is not there fails at creating the temporary file, naming the
// path the caller gave and wrapping the filesystem's error.
func TestReplaceDocument_MissingParentDirectory(t *testing.T) {
	t.Parallel()
	dest := filepath.Join(t.TempDir(), "missing-dir", "ir.json")

	err := output.ReplaceDocument(dest, orderedDoc(), output.Compact)

	require.ErrorIs(t, err, os.ErrNotExist)
	assert.ErrorContains(t, err, fmt.Sprintf("create output %q", dest))
}

// TestEncode_ErrorPaths pins that both layouts tell the failures apart: a
// document that will not marshal, a destination that refuses the bytes, and one
// that refuses only the trailing newline. json.MarshalWrite reports the first
// two as one error, so without the writer that records what the destination
// said, a disk failure would be reported as a marshal failure.
func TestEncode_ErrorPaths(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		w         func() io.Writer
		doc       func() *ir.Document
		want      string
		wantCause error
	}{
		{"bad document", func() io.Writer { return io.Discard }, badDoc,
			"marshal ir document", nil},
		{"refusing destination", func() io.Writer { return refusingWriter{} }, orderedDoc,
			"write ir document: disk gone", errDisk},
		{"refusing destination before a bad document", func() io.Writer { return refusingWriter{} }, bigBadDoc,
			"write ir document: disk gone", errDisk},
		{"newline refused", func() io.Writer { return &newlineRefusingWriter{} }, orderedDoc,
			"write ir document: disk gone", errDisk},
	}
	for _, tt := range tests {
		for _, l := range layouts {
			t.Run(tt.name+", "+l.name, func(t *testing.T) {
				t.Parallel()

				err := output.Encode(tt.w(), tt.doc(), l.layout)

				require.Error(t, err)
				assert.ErrorContains(t, err, tt.want)
				if tt.wantCause != nil {
					assert.ErrorIs(t, err, tt.wantCause)
					return
				}
				assert.NotContains(t, err.Error(), "write ir document")
			})
		}
	}
}

// TestReplaceDocument_RefusesAnEmptyPath pins that a call naming no file is
// refused before anything is created, rather than encoding the document into
// a temporary file in the working directory and failing at the rename.
func TestReplaceDocument_RefusesAnEmptyPath(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	err := output.ReplaceDocument("", orderedDoc(), output.Compact)

	assert.EqualError(t, err, "replace output: no path to replace")
	assert.Empty(t, entriesIn(t, dir))
}
