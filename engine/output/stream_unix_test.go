// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

//go:build unix

package output_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/engine/output"
)

// newFIFO makes a FIFO named ir.fifo in a fresh directory and returns its
// path.
func newFIFO(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "ir.fifo")
	require.NoError(t, syscall.Mkfifo(path, 0o600))
	return path
}

// openReader opens the FIFO at path for reading without waiting for a writer,
// so a writer finds it read. The document a test writes fits the FIFO's
// buffer, so the write ends before anything is read.
func openReader(t *testing.T, path string) *os.File {
	t.Helper()
	r, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, r.Close()) })
	return r
}

// TestReplaceDocument_AStreamIsWrittenAsStdoutIs pins that a FIFO, reached by
// its own name or through a symlink, receives the document as WriteDocument
// writes it, its name left as it was: renaming a file over it would leave its
// reader waiting and the FIFO gone.
func TestReplaceDocument_AStreamIsWrittenAsStdoutIs(t *testing.T) {
	t.Parallel()
	for _, viaLink := range []bool{false, true} {
		t.Run(fmt.Sprintf("through a symlink %t", viaLink), func(t *testing.T) {
			t.Parallel()
			fifo := newFIFO(t)
			r := openReader(t, fifo)
			dest := fifo
			if viaLink {
				dest = filepath.Join(filepath.Dir(fifo), "link")
				require.NoError(t, os.Symlink(fifo, dest))
			}
			doc := orderedDoc()

			require.NoError(t, output.ReplaceDocument(t.Context(), dest, doc, output.Indented))

			got, err := io.ReadAll(r)
			require.NoError(t, err)
			assert.Equal(t, string(written(t, doc, output.Indented)), string(got))
			info, err := os.Lstat(dest)
			require.NoError(t, err)
			assert.Equal(t, viaLink, info.Mode()&fs.ModeSymlink != 0, "the name stays what it was")
			assert.ElementsMatch(t, map[bool][]string{false: {"ir.fifo"}, true: {"ir.fifo", "link"}}[viaLink],
				entriesIn(t, filepath.Dir(fifo)), "no temporary file is made beside a stream")
		})
	}
}

// TestReplaceDocument_ACharacterDeviceIsWritten pins that /dev/null takes the
// document, where publishing by rename would need /dev to take a temporary file.
func TestReplaceDocument_ACharacterDeviceIsWritten(t *testing.T) {
	t.Parallel()
	require.NoError(t, output.ReplaceDocument(t.Context(), os.DevNull, orderedDoc(), output.Compact))

	info, err := os.Lstat(os.DevNull)
	require.NoError(t, err)
	assert.NotZero(t, info.Mode()&fs.ModeCharDevice, "the device stays a device")
}

// TestReplaceDocument_AFIFONoProcessReadsIsRefused pins that a write waits for
// no reader that may never come, as opening the FIFO plainly would: it fails
// at once, naming why, and leaves the FIFO as it was.
func TestReplaceDocument_AFIFONoProcessReadsIsRefused(t *testing.T) {
	t.Parallel()
	fifo := newFIFO(t)

	err := output.ReplaceDocument(t.Context(), fifo, orderedDoc(), output.Compact)

	assert.EqualError(t, err, fmt.Sprintf("open output %q: no process has the FIFO open for reading", fifo))
	info, statErr := os.Lstat(fifo)
	require.NoError(t, statErr)
	assert.NotZero(t, info.Mode()&fs.ModeNamedPipe)
	assert.Equal(t, []string{"ir.fifo"}, entriesIn(t, filepath.Dir(fifo)))
}

// TestReplaceDocument_AStreamUnderADoneContextGetsNothing pins that a write
// stopped before the document is encoded leaves the reader nothing but the end
// of the stream, as stdout gets.
func TestReplaceDocument_AStreamUnderADoneContextGetsNothing(t *testing.T) {
	t.Parallel()
	fifo := newFIFO(t)
	r := openReader(t, fifo)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	err := output.ReplaceDocument(ctx, fifo, orderedDoc(), output.Compact)

	require.ErrorIs(t, err, context.Canceled)
	got, readErr := io.ReadAll(r)
	require.NoError(t, readErr)
	assert.Empty(t, got)
}

// TestReplaceDocument_FilesAreStillReplaced pins what a destination that is a
// file, or names none, gets: a regular file in its place, through the rename,
// whatever the name held. A symlink is replaced, not followed, a read-only file
// keeps its mode, and none leaves a temporary file behind.
func TestReplaceDocument_FilesAreStillReplaced(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		setup func(t *testing.T, dir, dest string)
		mode  fs.FileMode
	}{
		{"a read-only file", func(t *testing.T, _, dest string) {
			t.Helper()
			require.NoError(t, os.WriteFile(dest, []byte("PREVIOUS\n"), 0o444))
			require.NoError(t, os.Chmod(dest, 0o444))
		}, 0o444},
		{"a symlink to nowhere", func(t *testing.T, dir, dest string) {
			t.Helper()
			require.NoError(t, os.Symlink(filepath.Join(dir, "gone"), dest))
		}, 0},
		{"a symlink to a file", func(t *testing.T, dir, dest string) {
			t.Helper()
			require.NoError(t, os.WriteFile(filepath.Join(dir, "gone"), []byte("PREVIOUS\n"), 0o600))
			require.NoError(t, os.Symlink(filepath.Join(dir, "gone"), dest))
		}, 0o600},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			dest := filepath.Join(dir, "ir.json")
			tt.setup(t, dir, dest)
			before := entriesIn(t, dir)
			doc := orderedDoc()

			require.NoError(t, output.ReplaceDocument(t.Context(), dest, doc, output.Compact))

			info, err := os.Lstat(dest)
			require.NoError(t, err)
			assert.True(t, info.Mode().IsRegular(), "a regular file takes the name")
			if tt.mode != 0 {
				assert.Equal(t, tt.mode, info.Mode().Perm(), "the mode the destination had")
			}
			assert.Equal(t, string(written(t, doc, output.Compact)), contentOf(t, dest))
			assert.ElementsMatch(t, before, entriesIn(t, dir))
		})
	}
}

// TestReplaceDocument_AFileInAReadOnlyDirectoryIsRefused pins the one thing a
// rename cannot do that a write in place can: replace a writable file whose
// directory takes no new entry. The error says why, and nothing changes.
func TestReplaceDocument_AFileInAReadOnlyDirectoryIsRefused(t *testing.T) {
	t.Parallel()
	if os.Geteuid() == 0 {
		t.Skip("root bypasses directory permission checks, so the replace would succeed")
	}
	dir := t.TempDir()
	dest := filepath.Join(dir, "ir.json")
	require.NoError(t, os.WriteFile(dest, []byte("PREVIOUS\n"), 0o644))
	require.NoError(t, os.Chmod(dir, 0o555))
	t.Cleanup(func() { assert.NoError(t, os.Chmod(dir, 0o755)) })

	err := output.ReplaceDocument(t.Context(), dest, orderedDoc(), output.Compact)

	require.ErrorIs(t, err, fs.ErrPermission)
	assert.ErrorContains(t, err, fmt.Sprintf("create the temporary file beside output %q", dest))
	assert.Equal(t, "PREVIOUS\n", contentOf(t, dest))
	assert.Equal(t, []string{"ir.json"}, entriesIn(t, dir))
}

// TestWriteStream_WhatChangedBeforeTheOpenIsLeftAlone pins the two ways the
// name a stream was seen at can change before writeStream opens it: gone, and
// a regular file now, which is not written in place, as its previous content
// shows. Each is handed the mode the earlier look saw.
func TestWriteStream_WhatChangedBeforeTheOpenIsLeftAlone(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	gone := filepath.Join(dir, "gone")
	file := filepath.Join(dir, "ir.json")
	require.NoError(t, os.WriteFile(file, []byte("PREVIOUS\n"), 0o644))

	err := output.WriteStream(t.Context(), gone, fs.ModeDevice|fs.ModeCharDevice, orderedDoc(), output.Compact)
	require.ErrorIs(t, err, fs.ErrNotExist)
	assert.ErrorContains(t, err, fmt.Sprintf("open output %q", gone))

	err = output.WriteStream(t.Context(), file, fs.ModeNamedPipe, orderedDoc(), output.Compact)
	assert.EqualError(t, err, fmt.Sprintf("output %q stopped being a FIFO or a character device as it was opened", file))
	assert.Equal(t, "PREVIOUS\n", contentOf(t, file))
	assert.Equal(t, []string{"ir.json"}, entriesIn(t, dir))
}

// TestReplaceDocument_AStreamWaitsForItsReader pins that a document larger
// than a FIFO holds is written whole, the write waiting for the reader to make
// room as one to stdout does, rather than failing once the FIFO is full: the
// FIFO is opened without waiting, which leaves a write unwilling to wait too
// unless the file is put back in blocking mode.
func TestReplaceDocument_AStreamWaitsForItsReader(t *testing.T) {
	t.Parallel()
	fifo := newFIFO(t)
	doc := manyTypesDoc()
	want := written(t, doc, output.Indented)
	require.Greater(t, len(want), 1<<17, "the document must outgrow a FIFO's buffer, 64 KiB by default")
	// Opened to read and write, the reader never sees the end of the stream,
	// so it reads exactly the document's length, in a reading mode that waits.
	r, err := os.OpenFile(fifo, os.O_RDWR, 0)
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, r.Close()) })
	got := make([]byte, len(want))
	done := make(chan error, 1)
	go func() {
		_, err := io.ReadFull(r, got)
		done <- err
	}()

	err = output.ReplaceDocument(t.Context(), fifo, doc, output.Indented)
	if err != nil {
		// Release the reader, which waits for bytes no write will send.
		_, writeErr := r.Write(make([]byte, len(want)))
		assert.NoError(t, writeErr)
	}

	require.NoError(t, <-done)
	require.NoError(t, err)
	assert.Equal(t, string(want), string(got))
}

// TestOpenStream_AFileThatCannotWaitIsClosed pins that a stream left unable to
// wait for room is refused, and closed rather than leaked. Not parallel: no
// other test may take the descriptor number before it is looked at.
func TestOpenStream_AFileThatCannotWaitIsClosed(t *testing.T) {
	var fd int
	boom := errors.New("boom")

	f, err := output.OpenStream(os.DevNull, fs.ModeDevice|fs.ModeCharDevice, func(got int, nonblocking bool) error {
		fd = got
		assert.False(t, nonblocking, "a write must wait")
		return boom
	})

	assert.Nil(t, f)
	require.ErrorIs(t, err, boom)
	assert.ErrorContains(t, err, fmt.Sprintf("open output %q", os.DevNull))
	var st syscall.Stat_t
	assert.ErrorIs(t, syscall.Fstat(fd, &st), syscall.EBADF, "the descriptor is closed")
}
