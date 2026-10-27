package load

import (
	"context"
	"fmt"
	"io/fs"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi/internal/budget"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/compilers/openapi/internal/overlay"
)

// describingOverlay sets the source's info description, so a test can see the
// overlay it read was applied.
const describingOverlay = "overlay: 1.0.0\ninfo: {title: o, version: \"1\"}\n" +
	"actions:\n  - target: $.info\n    update: {description: from the overlay}\n"

// namedOverlay is the loader's options for an overlay the caller named and did
// not read, read through r under the byte budget limit.
func namedOverlay(r compilers.DocumentReader, limit budget.Limit) Options {
	return Options{Overlay: &overlay.Options{Path: "patch.yaml"}, OverlaySrcIndex: 1, Reader: r,
		MaxSourceBytes: limit}
}

// TestLoad_ReadsANamedOverlayThroughTheReader pins where an overlay named by
// path is read: through the caller's reader, in the compile, held to the byte
// budget and confined nowhere, since the caller named it and the spec did not,
// under the compile's own context. What it read is what is applied.
func TestLoad_ReadsANamedOverlayThroughTheReader(t *testing.T) {
	t.Parallel()
	r := &fakeReader{data: []byte(describingOverlay)}
	opts := namedOverlay(r, budget.AtMost(4096))
	opts.Within = "/confined/to/here"
	ctx := context.WithValue(t.Context(), ctxKey{}, "the compile's")

	doc, diags, err := Load(ctx, 0, openapitest.SourceOf(minimal31), opts)

	require.NoError(t, err)
	require.NotNil(t, doc, "%+v", diags)
	assert.Equal(t, []compilers.DocumentRequest{{Location: "patch.yaml", Limit: 4096}}, r.seen())
	assert.Equal(t, []any{"the compile's"}, r.contexts())
	assert.True(t, doc.Overlay.Applied())
	assert.Equal(t, "from the overlay", doc.Doc.Info.GetDescription())
}

// TestLoad_AnOverlayHandedInBytesIsNotRead pins the other way an overlay
// arrives: bytes the caller read, which the reader is never asked for.
func TestLoad_AnOverlayHandedInBytesIsNotRead(t *testing.T) {
	t.Parallel()
	r := &fakeReader{err: fs.ErrNotExist}
	opts := overlayOf(describingOverlay)
	opts.Reader = r

	doc, diags, err := Load(t.Context(), 0, openapitest.SourceOf(minimal31), opts)

	require.NoError(t, err)
	require.NotNil(t, doc, "%+v", diags)
	assert.Empty(t, r.seen())
	assert.Equal(t, "from the overlay", doc.Doc.Info.GetDescription())
}

// TestLoad_ANamedOverlayPastTheBudgetIsRefused pins the budget on an overlay
// read through the reader: refused under its own index with the words an
// overlay handed in bytes gets, whether the reader refused it as too large or
// broke its contract and handed back more than the limit.
func TestLoad_ANamedOverlayPastTheBudgetIsRefused(t *testing.T) {
	t.Parallel()
	const limit = 64
	tests := []struct {
		name   string
		reader *fakeReader
	}{
		{name: "the reader refuses it", reader: &fakeReader{
			err: fmt.Errorf("patch.yaml: %w (%d bytes)", compilers.ErrDocumentTooLarge, limit)}},
		{name: "the reader hands back too much", reader: &fakeReader{data: []byte(describingOverlay)}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Greater(t, len(describingOverlay), limit)

			doc, diags, err := Load(t.Context(), 0, openapitest.SourceOf(minimal31), namedOverlay(tc.reader, budget.AtMost(limit)))

			require.NoError(t, err, "a document past its budget is a diagnostic, not a Go error")
			assert.Nil(t, doc)
			require.Len(t, diags, 1, "%+v", diags)
			assert.Equal(t, diag.BudgetExceeded, diags[0].Code)
			assert.Equal(t, 1, diags[0].Provenance.Source, "the overlay is what crossed it")
			assert.Equal(t, "overlay document is past the 64-byte budget", diags[0].Message)
		})
	}
}

// TestLoad_ANamedOverlayThatCannotBeReadIsAGoError pins the failures that are
// the caller's, as an unreadable spec is: a file that is not there, a read the
// context stopped, no reader to read it through, no path, and a URL, which no
// setting may take the compile to, each a Go error, with no document and no
// diagnostic. No path and a URL are refused before the reader is asked.
func TestLoad_ANamedOverlayThatCannotBeReadIsAGoError(t *testing.T) {
	t.Parallel()
	const urlRefusal = "names a URL; an overlay is read from a local file"
	tests := []struct {
		name    string
		path    string
		reader  *fakeReader
		wantIs  error
		wantMsg string
		unread  bool
	}{
		{name: "a missing file", path: "patch.yaml", reader: &fakeReader{err: fs.ErrNotExist},
			wantIs: fs.ErrNotExist, wantMsg: "openapi: read overlay patch.yaml"},
		{name: "a stopped read", path: "patch.yaml", reader: &fakeReader{err: context.Canceled},
			wantIs: context.Canceled, wantMsg: "openapi: read overlay patch.yaml"},
		{name: "no reader", path: "patch.yaml",
			wantMsg: "openapi: overlay patch.yaml holds no bytes, and no reader was supplied to read it"},
		{name: "no path", reader: &fakeReader{data: []byte(describingOverlay)},
			wantMsg: "openapi: the overlay holds no bytes and names no file to read", unread: true},
		{name: "an http URL", path: "http://example.com/patch.yaml",
			reader: &fakeReader{data: []byte(describingOverlay)}, wantMsg: urlRefusal, unread: true},
		{name: "an https URL spelled in capitals", path: "HTTPS://example.com/patch.yaml",
			reader: &fakeReader{data: []byte(describingOverlay)}, wantMsg: urlRefusal, unread: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			opts := namedOverlay(nil, budget.Unbounded())
			if tc.reader != nil {
				opts.Reader = tc.reader
			}
			opts.Overlay.Path = tc.path

			doc, diags, err := Load(t.Context(), 0, openapitest.SourceOf(minimal31), opts)

			require.Error(t, err)
			if tc.wantIs != nil {
				require.ErrorIs(t, err, tc.wantIs)
			}
			assert.Contains(t, err.Error(), tc.wantMsg)
			assert.Nil(t, doc)
			assert.Empty(t, diags)
			if tc.unread {
				assert.Empty(t, tc.reader.seen(), "refused before the reader is asked")
			}
		})
	}
}
