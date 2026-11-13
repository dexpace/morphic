// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package output

import (
	"bytes"
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"os"

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

// ReplaceDocument writes doc's IR JSON, laid out as layout and ending in a
// newline, to what path names. A regular file, or nothing, is replaced through
// a temporary file renamed over path: path holds the whole previous content or
// the whole document, and a failed marshal or a ctx that ends first leaves it
// be. Renaming needs path's directory to take a new entry, replaces a symlink
// rather than its target, and leaves other hard links the old content. A FIFO
// or a character device, such as /dev/null, is written as a stream; anything
// else is refused (writeStream).
func ReplaceDocument(ctx context.Context, path string, doc *ir.Document, layout Layout) error {
	if err := check(ctx, doc, layout); err != nil {
		return err
	}
	// Stat reads through a symlink, so a link to a stream is written and one to
	// a file is renamed over: /dev/stdout too, where /dev takes the temporary
	// file, as it does for root (#1864).
	if info, err := os.Stat(path); err == nil && !info.Mode().IsRegular() {
		return writeStream(ctx, path, info.Mode(), doc, layout)
	}
	return atomicfile.Replace(ctx, path, func(w io.Writer) error {
		return encode(ctx, w, doc, layout)
	})
}

// destWriter passes writes through to w until ctx is done, and records the
// error that ended them: w's, or ctx's. It is what keeps a destination that
// refused the bytes, or a write that was stopped, distinguishable from a
// document that would not marshal: json.MarshalWrite reports each as one
// error, and they are different things to tell a user about.
type destWriter struct {
	// ctx is the context the encoding runs under, held because Write takes none.
	ctx context.Context
	w   io.Writer
	err error
}

func (d *destWriter) Write(p []byte) (int, error) {
	if err := d.ctx.Err(); err != nil {
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
	dest := &destWriter{ctx: ctx, w: w}

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
