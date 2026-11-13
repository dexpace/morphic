// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package output

import (
	"context"
	"io"
	"io/fs"

	"github.com/dexpace/morphic/ir"
)

// Encode exposes encode to the external tests. Its error paths, a destination
// that refuses the bytes and one that refuses only the trailing newline, are
// reachable from outside only through a failing file, which WriteDocument's
// buffer and ReplaceDocument's temporary file hide.
func Encode(ctx context.Context, w io.Writer, doc *ir.Document, layout Layout) error {
	return encode(ctx, w, doc, layout)
}

// OpenStream exposes openStream, so a test can fail putting the file back in
// blocking mode.
var OpenStream = openStream

// WriteStream exposes writeStream, so a test can hand it a kind of file it
// cannot make, such as a block device.
var WriteStream = writeStream

// StillStream exposes stillStream, so a test can hand it a file whose Stat
// fails, or that changed kind as it was opened.
func StillStream(path string, f interface{ Stat() (fs.FileInfo, error) }) error {
	return stillStream(path, f)
}
