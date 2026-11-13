// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package output

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"syscall"

	"github.com/dexpace/morphic/ir"
)

// writeStream writes doc to path, which holds mode, a kind of file no rename
// replaces. A FIFO or a character device is opened as it is, neither created
// nor truncated, and written as WriteDocument writes a stream: the document
// whole once encoded, or none of it. A FIFO no process reads is refused rather
// than waited for, and a write then waits for the reader, as one to stdout
// does. Any other kind is refused.
func writeStream(ctx context.Context, path string, mode fs.FileMode, doc *ir.Document, layout Layout) error {
	if !isStream(mode) {
		return fmt.Errorf("output %q is %s, which holds no document", path, kindOf(mode))
	}
	f, err := openStream(path, mode, syscall.SetNonblock)
	if err != nil {
		return err
	}
	if err := stillStream(path, f); err != nil {
		return errors.Join(err, f.Close())
	}
	return errors.Join(WriteDocument(ctx, f, doc, layout), f.Close())
}

// openStream opens path, a FIFO or a character device as mode says, to write.
// It opens without waiting: a FIFO no process reads is refused at once, where
// a plain open would wait for a reader that may never come. Nor can a terminal
// become the controlling one of a process without one. setNonblock is
// syscall.SetNonblock, which then lets a write wait for room, as one to stdout
// does, rather than fail once a FIFO is full: the runtime waits only on a file
// it polls, which a FIFO on macOS is not. A test hands in one that fails.
func openStream(path string, mode fs.FileMode, setNonblock func(fd int, nonblocking bool) error) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_WRONLY|syscall.O_NONBLOCK|syscall.O_NOCTTY, 0)
	if mode&fs.ModeNamedPipe != 0 && errors.Is(err, syscall.ENXIO) {
		return nil, fmt.Errorf("open output %q: no process has the FIFO open for reading", path)
	}
	if err != nil {
		return nil, fmt.Errorf("open output %q: %w", path, err)
	}
	if err := setNonblock(int(f.Fd()), false); err != nil {
		return nil, errors.Join(fmt.Errorf("open output %q: %w", path, err), f.Close())
	}
	return f, nil
}

// isStream reports whether mode is a FIFO's or a character device's.
func isStream(mode fs.FileMode) bool {
	return mode&fs.ModeNamedPipe != 0 || mode&fs.ModeCharDevice != 0
}

// kindOf names the kind of file mode is, for a refusal: one that is not a
// regular file or a stream.
func kindOf(mode fs.FileMode) string {
	switch {
	case mode.IsDir():
		return "a directory"
	case mode&fs.ModeSocket != 0:
		return "a socket"
	case mode&fs.ModeDevice != 0:
		return "a block device"
	default:
		return "no regular file, FIFO or character device"
	}
}

// statter is what stillStream asks of an opened file.
type statter interface {
	Stat() (fs.FileInfo, error)
}

// stillStream refuses f, opened as path, unless it is a FIFO or a character
// device: what path names can change between the look that chose to write it
// as a stream and the open, and a regular file written in place would be left
// half old.
func stillStream(path string, f statter) error {
	info, err := f.Stat()
	if err != nil {
		return fmt.Errorf("stat output %q: %w", path, err)
	}
	if !isStream(info.Mode()) {
		return fmt.Errorf("output %q stopped being a FIFO or a character device as it was opened", path)
	}
	return nil
}
