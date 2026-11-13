// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

//go:build unix

package output_test

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/engine/output"
)

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
