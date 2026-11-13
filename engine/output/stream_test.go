// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package output_test

import (
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/engine/output"
)

// TestWriteStream_RefusesWhatIsNoStream pins the kinds of file writeStream
// refuses before it opens anything, each named: those a test cannot make, a
// block device among them, are handed in as a mode.
func TestWriteStream_RefusesWhatIsNoStream(t *testing.T) {
	t.Parallel()
	tests := []struct {
		mode fs.FileMode
		kind string
	}{
		{fs.ModeDir, "a directory"},
		{fs.ModeSocket, "a socket"},
		{fs.ModeDevice, "a block device"},
		{fs.ModeIrregular, "no regular file, FIFO or character device"},
		{fs.ModeSymlink, "no regular file, FIFO or character device"},
	}
	for _, tt := range tests {
		t.Run(tt.kind, func(t *testing.T) {
			t.Parallel()
			// A path that is not there shows nothing was opened: an open would
			// fail on it with another error.
			dest := filepath.Join(t.TempDir(), "absent")

			err := output.WriteStream(t.Context(), dest, tt.mode, orderedDoc(), output.Compact)

			assert.EqualError(t, err, fmt.Sprintf("output %q is %s, which holds no document", dest, tt.kind))
			assert.NoFileExists(t, dest)
		})
	}
}

// statResult is a file whose Stat answers as a test says.
type statResult struct {
	mode fs.FileMode
	err  error
}

func (s statResult) Stat() (fs.FileInfo, error) { return modeInfo(s.mode), s.err }

// modeInfo is a FileInfo holding a mode and nothing else.
type modeInfo fs.FileMode

func (modeInfo) Name() string        { return "f" }
func (modeInfo) Size() int64         { return 0 }
func (m modeInfo) Mode() fs.FileMode { return fs.FileMode(m) }
func (modeInfo) ModTime() time.Time  { return time.Time{} }
func (m modeInfo) IsDir() bool       { return fs.FileMode(m).IsDir() }
func (modeInfo) Sys() any            { return nil }

// TestStillStream_RefusesWhatChangedAsItWasOpened pins the check after the
// open: a file that is no longer a FIFO or a character device, or whose kind
// cannot be read, is refused, so a regular file swapped in is never written
// in place.
func TestStillStream_RefusesWhatChangedAsItWasOpened(t *testing.T) {
	t.Parallel()
	boom := errors.New("boom")
	tests := []struct {
		name string
		file statResult
		want string
	}{
		{"a FIFO", statResult{mode: fs.ModeNamedPipe}, ""},
		{"a character device", statResult{mode: fs.ModeDevice | fs.ModeCharDevice}, ""},
		{"a regular file", statResult{}, `output "p" stopped being a FIFO or a character device as it was opened`},
		{"a directory", statResult{mode: fs.ModeDir}, `output "p" stopped being a FIFO or a character device as it was opened`},
		{"no answer", statResult{mode: fs.ModeNamedPipe, err: boom}, `stat output "p": boom`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := output.StillStream("p", tt.file)
			if tt.want == "" {
				assert.NoError(t, err)
				return
			}
			assert.EqualError(t, err, tt.want)
		})
	}
}
