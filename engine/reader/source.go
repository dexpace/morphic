// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package reader

import (
	"context"
	"errors"
)

// ReadSource reads the file at path, a file path and never a URL, as a source
// is read for compilers.Registry.Detect: whole, or for a file holding more than
// limit bytes, its first limit+1, which is as far as a compiler held to limit
// reads before declining it (compilers.Compiler.SourceLimit). A limit of zero
// or less reads any size.
//
// It reads a regular file and refuses anything else, as Disk does, before
// opening it: opening a FIFO waits for a writer, and a device can stream
// without end.
func ReadSource(ctx context.Context, path string, limit int) ([]byte, error) {
	return readSource(ctx, osFiles{}, path, limit)
}

// readSource is ReadSource over fsys.
func readSource(ctx context.Context, fsys files, path string, limit int) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	f, err := openRegular(fsys, path)
	if err != nil {
		return nil, err
	}
	info, err := judgeOpened(path, f)
	if err != nil {
		return nil, errors.Join(err, f.Close())
	}
	data, err := readUpTo(ctx, path, f, onePast(limit), info.Size())
	if err = errors.Join(err, f.Close()); err != nil {
		return nil, err
	}
	return data, nil
}
