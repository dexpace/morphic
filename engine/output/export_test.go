package output

import (
	"io"

	"github.com/dexpace/morphic/ir"
)

// Encode exposes encode to the external tests. Its error paths, a destination
// that refuses the bytes and one that refuses only the trailing newline, are
// reachable from outside only through a failing file, which WriteDocument's
// buffer and ReplaceDocument's temporary file hide.
func Encode(w io.Writer, doc *ir.Document, layout Layout) error {
	return encode(w, doc, layout)
}
