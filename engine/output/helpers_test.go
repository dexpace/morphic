package output_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/engine/output"
	"github.com/dexpace/morphic/ir"
)

// errDisk is the failure the refusing writers return.
var errDisk = errors.New("disk gone")

// layouts names both layouts for the tests that run each.
var layouts = []struct {
	name   string
	layout output.Layout
}{
	{"compact", output.Compact},
	{"indented", output.Indented},
}

// refusingWriter fails every write.
type refusingWriter struct{}

func (refusingWriter) Write([]byte) (int, error) { return 0, errDisk }

// fillingWriter keeps what it is given until it holds at least stopAt bytes,
// and then refuses every write, as a full destination does. refusals counts
// the writes it refused. With stopAt the document's length, the write it
// refuses is the newline encode writes after the document, however many writes
// the encoder split the document itself into.
type fillingWriter struct {
	stopAt   int
	refusals int
	buf      bytes.Buffer
}

func (w *fillingWriter) Write(p []byte) (int, error) {
	if w.buf.Len() >= w.stopAt {
		w.refusals++
		return 0, errDisk
	}
	return w.buf.Write(p)
}

// cancellingWriter keeps what it is given and ends its context once it holds
// at least stopAt bytes, as a stop arriving while the encoder writes does.
// late counts the writes it is handed once the context has ended.
type cancellingWriter struct {
	cancel context.CancelFunc
	stopAt int
	late   int
	buf    bytes.Buffer
}

// newCancellingWriter returns a context and the cancellingWriter that ends it
// once it holds at least stopAt bytes, the context ending at once when stopAt
// is 0.
func newCancellingWriter(t *testing.T, stopAt int) (context.Context, *cancellingWriter) {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)
	w := &cancellingWriter{cancel: cancel, stopAt: stopAt}
	if w.due() {
		cancel()
	}
	return ctx, w
}

// due reports whether the writer holds the stopAt bytes that end its context.
func (w *cancellingWriter) due() bool { return w.buf.Len() >= w.stopAt }

func (w *cancellingWriter) Write(p []byte) (int, error) {
	if w.due() {
		w.late++
	}
	n, err := w.buf.Write(p)
	if w.due() {
		w.cancel()
	}
	return n, err
}

// badDoc returns a document that cannot be marshalled: its Unmodeled holds a
// RawValue of malformed bytes, which the encoder refuses when it reaches it.
func badDoc() *ir.Document {
	return &ir.Document{
		Name:      "Bad",
		Unmodeled: ir.Unmodeled{"openapi:x": {Value: ir.RawValue("{invalid")}},
	}
}

// bigBadDoc is manyTypesDoc holding badDoc's malformed value, which the
// encoder reaches only after the types. The types take the encoder many
// writes, so an encoder writing straight to its destination has written part
// of the document by the time it fails.
func bigBadDoc() *ir.Document {
	doc := manyTypesDoc()
	doc.Unmodeled = badDoc().Unmodeled
	return doc
}

// orderedDoc returns a stamped document whose registry and lists would come out
// in another order than the one written if the encoder did not fix it: a
// dozen types named out of alphabetical order, and tags in an order of their own.
func orderedDoc() *ir.Document {
	types := ir.TypeRegistry{}
	for _, name := range []string{"Zeta", "Kappa", "Alpha", "Omega", "Mid", "Beta",
		"Sigma", "Delta", "Pi", "Eta", "Rho", "Gamma"} {
		id := ir.TypeID("t/" + name)
		types[id] = &ir.Model{ID: id}
	}
	return &ir.Document{
		IRVersion: ir.IRVersion,
		Name:      "Ordered",
		Version:   "1",
		Types:     types,
		TagDefs:   []ir.TagDef{{Name: "zebra"}, {Name: "apple"}},
	}
}

// manyTypesDoc returns a stamped document of enough types that the encoder
// writes it in many writes: encoded, it is far larger than any buffer the
// encoder keeps between calls, so no first write holds it whole.
func manyTypesDoc() *ir.Document {
	types := ir.TypeRegistry{}
	for i := range 2000 {
		id := ir.TypeID(fmt.Sprintf("t/Type%04d", i))
		types[id] = &ir.Model{ID: id}
	}
	return &ir.Document{IRVersion: ir.IRVersion, Name: "Many", Version: "1", Types: types}
}

// written returns what WriteDocument writes for doc in layout.
func written(t *testing.T, doc *ir.Document, layout output.Layout) []byte {
	t.Helper()
	var buf bytes.Buffer
	require.NoError(t, output.WriteDocument(t.Context(), &buf, doc, layout))
	return buf.Bytes()
}

// contentOf returns the content of the file at path.
func contentOf(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	return string(raw)
}

// entriesIn lists the names in dir.
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
