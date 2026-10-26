package reader_test

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"slices"
	"strings"
	"testing"
	"testing/fstest"
	"testing/iotest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/engine/reader"
)

// scriptedFile is an fs.File whose Stat, Read and Close answer as a test
// scripts them, and which counts what it was asked.
type scriptedFile struct {
	info     fs.FileInfo
	statErr  error
	body     io.Reader
	closeErr error

	reads, closes int
}

func (f *scriptedFile) Stat() (fs.FileInfo, error) { return f.info, f.statErr }

func (f *scriptedFile) Read(p []byte) (int, error) {
	f.reads++
	return f.body.Read(p)
}

func (f *scriptedFile) Close() error {
	f.closes++
	return f.closeErr
}

// infoOf returns the FileInfo of a file with mode.
func infoOf(t *testing.T, mode fs.FileMode) fs.FileInfo {
	t.Helper()
	info, err := fstest.MapFS{"f": {Mode: mode}}.Stat("f")
	require.NoError(t, err)
	return info
}

// TestReadOpened_JudgesTheFileOpened pins the second judgement of a file: what
// a path names can change between the check and the open, so the file opened
// is judged too, refused unread and closed when it is not regular, or when
// Stat cannot say, and every failure of the close is reported with it.
func TestReadOpened_JudgesTheFileOpened(t *testing.T) {
	t.Parallel()
	statFailed := errors.New("stat failed")
	closeFailed := errors.New("close failed")
	tests := []struct {
		name   string
		file   func(t *testing.T) *scriptedFile
		wantIs []error
	}{
		{name: "a directory", file: func(t *testing.T) *scriptedFile {
			return &scriptedFile{info: infoOf(t, fs.ModeDir)}
		}, wantIs: []error{reader.ErrNotRegular}},
		{name: "a FIFO", file: func(t *testing.T) *scriptedFile {
			return &scriptedFile{info: infoOf(t, fs.ModeNamedPipe)}
		}, wantIs: []error{reader.ErrNotRegular}},
		{name: "a device whose close fails", file: func(t *testing.T) *scriptedFile {
			return &scriptedFile{info: infoOf(t, fs.ModeDevice|fs.ModeCharDevice), closeErr: closeFailed}
		}, wantIs: []error{reader.ErrNotRegular, closeFailed}},
		{name: "a file Stat fails on", file: func(*testing.T) *scriptedFile {
			return &scriptedFile{statErr: statFailed}
		}, wantIs: []error{statFailed}},
		{name: "a file Stat and close fail on", file: func(*testing.T) *scriptedFile {
			return &scriptedFile{statErr: statFailed, closeErr: closeFailed}
		}, wantIs: []error{statFailed, closeFailed}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := tc.file(t)
			f.body = strings.NewReader("never read")

			got, err := reader.ReadOpened(t.Context(), "p.yaml", f, 0)

			require.Error(t, err)
			for _, want := range tc.wantIs {
				assert.ErrorIs(t, err, want)
			}
			if slices.Contains(tc.wantIs, reader.ErrNotRegular) {
				assert.Contains(t, err.Error(), "p.yaml", "the file refused")
			}
			assert.Nil(t, got)
			assert.Zero(t, f.reads, "a file refused is never read")
			assert.Equal(t, 1, f.closes, "a file refused is closed once")
		})
	}
}

// TestReadOpened_ReadsAndClosesARegularFile pins the success path and each way
// it can end: the whole regular file is read and closed once, a limit
// refuses a longer one, a failed read and a failed close are each reported,
// and a failed close withholds the bytes read.
func TestReadOpened_ReadsAndClosesARegularFile(t *testing.T) {
	t.Parallel()
	const body = "12345"
	readFailed := errors.New("read failed")
	closeFailed := errors.New("close failed")
	tests := []struct {
		name     string
		body     io.Reader
		closeErr error
		limit    int
		want     string
		wantIs   []error
	}{
		{name: "a whole file", body: strings.NewReader(body), want: body},
		{name: "a file at its limit", body: strings.NewReader(body), limit: 5, want: body},
		{name: "a file over its limit", body: strings.NewReader(body), limit: 4,
			wantIs: []error{compilers.ErrDocumentTooLarge}},
		{name: "a read that fails", body: iotest.ErrReader(readFailed), wantIs: []error{readFailed}},
		{name: "a close that fails", body: strings.NewReader(body), closeErr: closeFailed,
			wantIs: []error{closeFailed}},
		{name: "a read and a close that fail", body: iotest.ErrReader(readFailed), closeErr: closeFailed,
			wantIs: []error{readFailed, closeFailed}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := &scriptedFile{info: infoOf(t, 0), body: tc.body, closeErr: tc.closeErr}

			got, err := reader.ReadOpened(t.Context(), "p.yaml", f, tc.limit)

			assert.Equal(t, 1, f.closes, "closed once, whatever the read did")
			if len(tc.wantIs) > 0 {
				require.Error(t, err)
				for _, want := range tc.wantIs {
					assert.ErrorIs(t, err, want)
				}
				assert.Nil(t, got, "a failed read hands back no bytes")
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, string(got))
		})
	}
}

// TestReadOpened_RefusesAFileItsSizePutsPastTheLimitUnread pins the check a
// file's own size answers: one that reports more bytes than the limit allows is
// refused as too large without a byte read, and closed, so a large file costs a
// stat rather than a read to the limit.
func TestReadOpened_RefusesAFileItsSizePutsPastTheLimitUnread(t *testing.T) {
	t.Parallel()
	f := &scriptedFile{info: sizedInfo{size: 1 << 30}, body: strings.NewReader("never read")}

	got, err := reader.ReadOpened(t.Context(), "p.yaml", f, 1<<20)

	require.ErrorIs(t, err, compilers.ErrDocumentTooLarge)
	assert.Contains(t, err.Error(), "p.yaml")
	assert.Nil(t, got)
	assert.Zero(t, f.reads, "refused by its size, unread")
	assert.Equal(t, 1, f.closes)
}

// TestReadOpened_RefusesAnOpenedDirectory runs the second judgement on a real
// file, a directory opened for reading: refused unread, and the file is
// closed, as os.ErrClosed on a second close shows.
func TestReadOpened_RefusesAnOpenedDirectory(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	f, err := os.Open(dir)
	require.NoError(t, err)

	got, err := reader.ReadOpened(t.Context(), dir, f, 0)

	require.ErrorIs(t, err, reader.ErrNotRegular)
	assert.Contains(t, err.Error(), dir)
	assert.Nil(t, got)
	assert.ErrorIs(t, f.Close(), os.ErrClosed, "the refused file is closed")
}
