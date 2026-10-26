package reader_test

import (
	"context"
	"errors"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/iotest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/engine/reader"
)

// sizedInfo is the FileInfo of a file of mode reporting size bytes.
type sizedInfo struct {
	mode fs.FileMode
	size int64
}

func (i sizedInfo) Name() string       { return "f" }
func (i sizedInfo) Size() int64        { return i.size }
func (i sizedInfo) Mode() fs.FileMode  { return i.mode }
func (i sizedInfo) ModTime() time.Time { return time.Time{} }
func (i sizedInfo) IsDir() bool        { return i.mode.IsDir() }
func (i sizedInfo) Sys() any           { return nil }

// scriptedFiles is a file system of one path, whose Stat answers stat and
// whose Open hands back file, or fails with openErr. It counts the opens, so
// a test can tell a file refused before it was opened.
type scriptedFiles struct {
	stat    fs.FileInfo
	statErr error
	file    *scriptedFile
	openErr error

	opens int
}

func (f *scriptedFiles) Stat(string) (fs.FileInfo, error) { return f.stat, f.statErr }

func (f *scriptedFiles) Open(string) (fs.File, error) {
	f.opens++
	if f.openErr != nil {
		return nil, f.openErr
	}
	return f.file, nil
}

// endless serves 'x' without end, as a device or a file growing faster than it
// is read does, counting the bytes it served.
type endless struct {
	served int
}

func (r *endless) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 'x'
	}
	r.served += len(p)
	return len(p), nil
}

// TestReadSource_RefusesWhatIsNoRegularFileUnopened pins that a FIFO, a device
// and a directory are refused before they are opened, so the open a FIFO waits
// in for a writer is never made and a device that streams without end is never
// read. The file system is a test's, so this holds on every platform.
func TestReadSource_RefusesWhatIsNoRegularFileUnopened(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		mode fs.FileMode
	}{
		{name: "a FIFO", mode: fs.ModeNamedPipe},
		{name: "a character device", mode: fs.ModeDevice | fs.ModeCharDevice},
		{name: "a block device", mode: fs.ModeDevice},
		{name: "a socket", mode: fs.ModeSocket},
		{name: "a directory", mode: fs.ModeDir},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			fsys := &scriptedFiles{stat: sizedInfo{mode: tc.mode}, file: &scriptedFile{body: &endless{}}}

			got, err := reader.ReadSourceFrom(t.Context(), fsys, "spec.yaml", 64)

			require.ErrorIs(t, err, reader.ErrNotRegular)
			assert.Contains(t, err.Error(), "spec.yaml", "the error names the file")
			assert.Nil(t, got)
			assert.Zero(t, fsys.opens, "refused before it was opened")
		})
	}
}

// TestReadSource_JudgesTheFileOpened pins the second judgement: a path that
// named a regular file when it was looked at may name another by the time it is
// opened, so what was opened is refused unread and closed when it is not
// regular, or when Stat cannot say what it is.
func TestReadSource_JudgesTheFileOpened(t *testing.T) {
	t.Parallel()
	statFailed := errors.New("stat failed")
	tests := []struct {
		name   string
		file   *scriptedFile
		wantIs error
	}{
		{name: "a FIFO swapped in", file: &scriptedFile{info: sizedInfo{mode: fs.ModeNamedPipe}},
			wantIs: reader.ErrNotRegular},
		{name: "a file Stat fails on", file: &scriptedFile{statErr: statFailed}, wantIs: statFailed},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			tc.file.body = &endless{}
			fsys := &scriptedFiles{stat: sizedInfo{}, file: tc.file}

			got, err := reader.ReadSourceFrom(t.Context(), fsys, "spec.yaml", 64)

			require.ErrorIs(t, err, tc.wantIs)
			assert.Nil(t, got)
			assert.Zero(t, tc.file.reads, "a file refused is never read")
			assert.Equal(t, 1, tc.file.closes, "and is closed once")
		})
	}
}

// TestReadSource_ReadsOnePastTheLimitAndNoFurther pins what a caller hands to
// detection: a source within the limit whole, and one past it as its first
// limit+1 bytes, read no further, whatever size it reports. Zero and less
// read any size, and math.MaxInt reads a source whole.
func TestReadSource_ReadsOnePastTheLimitAndNoFurther(t *testing.T) {
	t.Parallel()
	const body = "0123456789"
	tests := []struct {
		name  string
		limit int
		want  string
	}{
		{name: "within the limit", limit: 20, want: body},
		{name: "at the limit", limit: 10, want: body},
		{name: "one past the limit", limit: 9, want: body},
		{name: "well past the limit", limit: 3, want: "0123"},
		{name: "zero reads any size", limit: 0, want: body},
		{name: "negative reads any size", limit: -1, want: body},
		{name: "math.MaxInt reads it whole", limit: math.MaxInt, want: body},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			file := &scriptedFile{info: sizedInfo{size: int64(len(body))}, body: strings.NewReader(body)}
			fsys := &scriptedFiles{stat: sizedInfo{size: int64(len(body))}, file: file}

			got, err := reader.ReadSourceFrom(t.Context(), fsys, "spec.yaml", tc.limit)

			require.NoError(t, err)
			assert.Equal(t, tc.want, string(got))
			assert.Equal(t, 1, file.closes)
		})
	}
}

// TestReadSource_BoundsASourceWithoutEnd pins the read itself, not only what is
// handed back: a regular file that reports no size, as a file the kernel makes
// up does, and serves without end is read to one byte past the limit and no
// further.
func TestReadSource_BoundsASourceWithoutEnd(t *testing.T) {
	t.Parallel()
	const limit = 10 << 20
	body := &endless{}
	file := &scriptedFile{info: sizedInfo{}, body: body}

	got, err := reader.ReadSourceFrom(t.Context(), &scriptedFiles{stat: sizedInfo{}, file: file}, "spec.yaml", limit)

	require.NoError(t, err)
	assert.Len(t, got, limit+1)
	assert.Equal(t, limit+1, body.served, "nothing past the byte that shows the limit crossed is read")
}

// TestReadSource_StopsWhenTheContextIsDone pins cancellation before anything
// is opened and between two reads of a source: either way the read ends with
// the context's error and no bytes, and an opened file is closed.
func TestReadSource_StopsWhenTheContextIsDone(t *testing.T) {
	t.Parallel()
	t.Run("before the open", func(t *testing.T) {
		t.Parallel()
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		fsys := &scriptedFiles{stat: sizedInfo{}, file: &scriptedFile{body: &endless{}}}

		got, err := reader.ReadSourceFrom(ctx, fsys, "spec.yaml", 0)

		require.ErrorIs(t, err, context.Canceled)
		assert.Nil(t, got)
		assert.Zero(t, fsys.opens)
	})
	t.Run("between reads", func(t *testing.T) {
		t.Parallel()
		ctx, cancel := context.WithCancel(t.Context())
		body := &cancelling{cancel: cancel}
		file := &scriptedFile{info: sizedInfo{}, body: body}

		got, err := reader.ReadSourceFrom(ctx, &scriptedFiles{stat: sizedInfo{}, file: file}, "spec.yaml", 0)

		require.ErrorIs(t, err, context.Canceled)
		assert.Nil(t, got)
		assert.Equal(t, 1, file.reads, "the read stopped at the first ask after the cancel")
		assert.Equal(t, 1, file.closes)
	})
}

// cancelling serves 'x' without end and cancels its context on its first read,
// as a cancel landing while a long file is read does.
type cancelling struct {
	endless
	cancel context.CancelFunc
}

func (r *cancelling) Read(p []byte) (int, error) {
	r.cancel()
	return r.endless.Read(p)
}

// TestReadSource_ReportsWhatTheFileSystemSays pins the failures that are the
// file system's: a path Stat cannot read is left to the open, whose error names
// what is wrong, and a failed read and a failed close are each reported.
func TestReadSource_ReportsWhatTheFileSystemSays(t *testing.T) {
	t.Parallel()
	readFailed := errors.New("read failed")
	closeFailed := errors.New("close failed")
	tests := []struct {
		name   string
		fsys   *scriptedFiles
		wantIs []error
	}{
		{name: "a missing file", fsys: &scriptedFiles{statErr: fs.ErrNotExist, openErr: fs.ErrNotExist},
			wantIs: []error{fs.ErrNotExist}},
		{name: "a read that fails", fsys: &scriptedFiles{stat: sizedInfo{},
			file: &scriptedFile{info: sizedInfo{}, body: iotest.ErrReader(readFailed)}},
			wantIs: []error{readFailed}},
		{name: "a close that fails", fsys: &scriptedFiles{stat: sizedInfo{},
			file: &scriptedFile{info: sizedInfo{}, body: strings.NewReader("x"), closeErr: closeFailed}},
			wantIs: []error{closeFailed}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := reader.ReadSourceFrom(t.Context(), tc.fsys, "spec.yaml", 0)

			for _, want := range tc.wantIs {
				require.ErrorIs(t, err, want)
			}
			assert.Nil(t, got, "a failed read hands back no bytes")
		})
	}
}

// TestReadSource_ReadsARealFile runs ReadSource over the operating system's
// file system: a file is read whole within its limit and cut one byte past
// it, and a missing one reports os.Open's own error.
func TestReadSource_ReadsARealFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := writeFile(t, dir, "spec.yaml", strings.Repeat("y", 1000))

	whole, err := reader.ReadSource(t.Context(), path, 1000)
	require.NoError(t, err)
	assert.Len(t, whole, 1000)

	cut, err := reader.ReadSource(t.Context(), path, 10)
	require.NoError(t, err)
	assert.Len(t, cut, 11)

	_, err = reader.ReadSource(t.Context(), filepath.Join(dir, "missing.yaml"), 0)
	require.ErrorIs(t, err, os.ErrNotExist)
	var pathErr *fs.PathError
	assert.ErrorAs(t, err, &pathErr, "os.Open's error, naming the file")
}
