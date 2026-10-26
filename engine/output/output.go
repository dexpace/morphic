// Package output writes what a pipeline run produced: the IR document, to a
// stream or to a file it replaces atomically. It is the engine's writer, so a
// driver publishes a document exactly as the CLI does without encoding or
// replacing anything itself. A write stops when its context is done, and
// leaves no partial document behind.
package output

import (
	"bytes"
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"

	"github.com/dexpace/morphic/engine/internal/atomicfile"
	"github.com/dexpace/morphic/ir"
)

// Layout is how an IR document's JSON is laid out.
type Layout int

const (
	// Compact writes the document with no insignificant whitespace.
	Compact Layout = iota
	// Indented writes the document indented two spaces a level, for a person
	// to read. It is byte for byte what irtest's goldens hold, but for the
	// irVersion member a golden leaves out.
	Indented
)

// errNoDocument reports a call handed no document to write.
var errNoDocument = errors.New("output: no document to write")

// errNoWriter reports a WriteDocument call handed no writer.
var errNoWriter = errors.New("output: no writer to write to")

// errNoContext reports a call handed no context to write under.
var errNoContext = errors.New("output: no context to write under")

// check refuses a call with no context, no document or a layout this package
// does not define; an unknown layout written as either would hide the caller's
// mistake.
func check(ctx context.Context, doc *ir.Document, layout Layout) error {
	if ctx == nil {
		return errNoContext
	}
	if doc == nil {
		return errNoDocument
	}
	if layout != Compact && layout != Indented {
		return fmt.Errorf("output: unknown layout %d", layout)
	}
	return nil
}

// WriteDocument writes doc's IR JSON to w, laid out as layout and ending in a
// newline. The document is encoded whole before any byte reaches w, so w
// receives all of it or none: a document that will not marshal writes nothing,
// and neither does a ctx done before the encoding ends, which stops it at its
// next flush. A nil w or ctx is refused.
func WriteDocument(ctx context.Context, w io.Writer, doc *ir.Document, layout Layout) error {
	if w == nil {
		return errNoWriter
	}
	if err := check(ctx, doc, layout); err != nil {
		return err
	}
	var buf bytes.Buffer
	if err := encode(ctx, &buf, doc, layout); err != nil {
		return err
	}
	if _, err := w.Write(buf.Bytes()); err != nil {
		return fmt.Errorf("write ir document: %w", err)
	}
	return nil
}

// ReplaceDocument replaces the file at path with doc's IR JSON, laid out as
// layout and ending in a newline, through a temporary file renamed over it:
// path holds the whole previous content or the whole document, and a document
// that will not marshal, or a ctx that ends first, leaves it be.
//
// Publishing by rename needs path's directory to take a new entry, so
// /dev/null and a read-only directory fail. A symlink or FIFO at path is
// replaced, other hard links keep the old content, a name near the
// filesystem's limit can fail, and the directory is not synced.
func ReplaceDocument(ctx context.Context, path string, doc *ir.Document, layout Layout) error {
	if err := check(ctx, doc, layout); err != nil {
		return err
	}
	return atomicfile.Replace(ctx, path, func(w io.Writer) error {
		return encode(ctx, w, doc, layout)
	})
}

// destWriter passes writes through to w until stopped reports an error, and
// records the error that ended them: w's, or stopped's. It is what keeps a
// destination that refused the bytes, or a write that was stopped,
// distinguishable from a document that would not marshal: json.MarshalWrite
// reports each as one error, and they are different things to tell a user
// about.
type destWriter struct {
	w io.Writer
	// stopped is the Err method of the context the write runs under.
	stopped func() error
	err     error
}

func (d *destWriter) Write(p []byte) (int, error) {
	if err := d.stopped(); err != nil {
		d.err = err
		return 0, err
	}
	n, err := d.w.Write(p)
	if err != nil {
		d.err = err
	}
	return n, err
}

// encode streams doc's IR JSON to w, ending in a newline, and stops at the
// first write after ctx is done.
//
// Both layouts go through json.MarshalWrite, which writes the encoded bytes
// straight to w rather than building them in a slice first; indenting is an
// encode option on that same call, not a separate route. MarshalWrite ends no
// value with a newline, so encode writes one, through the same writer.
func encode(ctx context.Context, w io.Writer, doc *ir.Document, layout Layout) error {
	dest := &destWriter{w: w, stopped: ctx.Err}

	var err error
	if layout == Indented {
		err = json.MarshalWrite(dest, doc, jsontext.WithIndent("  "))
	} else {
		err = json.MarshalWrite(dest, doc)
	}
	if err != nil {
		if dest.err != nil {
			return fmt.Errorf("write ir document: %w", dest.err)
		}
		return fmt.Errorf("marshal ir document: %w", err)
	}
	if _, err := dest.Write([]byte("\n")); err != nil {
		return fmt.Errorf("write ir document: %w", err)
	}
	return nil
}
