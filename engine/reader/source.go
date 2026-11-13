// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package reader

import (
	"context"
	"errors"
	"path/filepath"
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

// SpecLocation returns where the spec at path lies, as a compile reads the
// references in it against (compilers.Source.Location): path made absolute
// against the working directory, the one read of it, the symbolic links of its
// directory resolved where they resolve. One directory reached by two
// spellings, the working directory's among them, is then one location, and
// what a reference names depends on neither. The file's own name is kept, so a
// spec that is a link reads its references beside the link. A URL lies at no
// path, so it returns "" for one.
func SpecLocation(path string) (string, error) {
	return specLocation(path, filepath.Abs)
}

// specLocation is SpecLocation with the reading of the working directory handed in,
// absolute, so a test can fail it, which no system reliably does.
func specLocation(path string, absolute func(string) (string, error)) (string, error) {
	if path == "" {
		return "", errEmptyLocation
	}
	if _, isURL := asURL(path); isURL {
		return "", nil
	}
	abs, err := absolute(path)
	if err != nil {
		return "", err
	}
	dir, err := filepath.EvalSymlinks(filepath.Dir(abs))
	if err != nil {
		return abs, nil
	}
	return filepath.Join(dir, filepath.Base(abs)), nil
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
