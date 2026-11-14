// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package doctree

import (
	"context"
	"errors"
	"io"

	yaml "go.yaml.in/yaml/v3"
)

// MaxStreamDocuments bounds how many documents a read parses to find the one it
// takes, and how many it counts past it, which a finding about a tail names.
// Each is parsed into a tree that is never lowered, so the bound caps what a
// stream can cost beyond the byte budget it already fits.
const MaxStreamDocuments = 1024

// firstWithContent reads documents from dec until one holds content, and
// returns it. A leading document that decodes to null, such as a bare `---`, is
// what a file assembled from fragments begins with; taking it as the document
// would refuse the input behind it.
//
// A stream with no such document yields the first one it holds, a null its
// reader refuses, or a node of no kind when it holds none. A decoder error
// before content is the input's. The search reads at most MaxStreamDocuments
// documents; past it the first one read stands.
func firstWithContent(dec *yaml.Decoder) (*yaml.Node, error) {
	var first *yaml.Node
	for range MaxStreamDocuments {
		var doc yaml.Node
		err := dec.Decode(&doc)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		if holdsContent(&doc) {
			return &doc, nil
		}
		if first == nil {
			first = &doc
		}
	}
	if first == nil {
		return &yaml.Node{}, nil
	}
	return first, nil
}

// readTail reads the documents that follow the one firstWithContent took,
// counting those that hold content: at most MaxStreamDocuments, then one more
// decode, so a stream that continues past the bound is seen. A document that
// holds nothing, a bare separator or an explicit null, is what a trailing `---`
// produces, and skipping it drops nothing, on the reading firstWithContent
// skips a leading one by.
func readTail(dec *yaml.Decoder) Tail {
	var t Tail
	for range MaxStreamDocuments {
		var doc yaml.Node
		err := dec.Decode(&doc)
		if errors.Is(err, io.EOF) {
			return t
		}
		if err != nil {
			t.Unparsed = true
			return t
		}
		if holdsContent(&doc) {
			if t.Line == 0 {
				t.Line, t.Column = doc.Content[0].Line, doc.Content[0].Column
			}
			t.Dropped++
		}
	}
	var past yaml.Node
	err := dec.Decode(&past)
	switch {
	case errors.Is(err, io.EOF):
	case err != nil:
		t.Unparsed = true
	default:
		t.Capped = true
	}
	return t
}

// holdsContent reports whether a decoded document wraps a root that is not the
// null scalar an empty document decodes to. The parser wraps exactly one root
// in every document it parses; the length check is the guard, not a case.
func holdsContent(doc *yaml.Node) bool {
	if doc.Kind != yaml.DocumentNode || len(doc.Content) != 1 {
		return false
	}
	root := doc.Content[0]
	return root.Kind != yaml.ScalarNode || root.ShortTag() != "!!null"
}

// contextReader reads r until ctx is done, then fails with ctx's error, so a
// parser reading through it stops within one of its reads. The parser reads a
// stream in small chunks as it parses, where a parse of bytes in hand runs to
// its end. It holds ctx because io.Reader's Read takes none, for the one
// decode it is made for.
type contextReader struct {
	ctx context.Context
	r   io.Reader
}

// Read implements io.Reader.
func (c contextReader) Read(p []byte) (int, error) {
	if err := c.ctx.Err(); err != nil {
		return 0, err
	}
	return c.r.Read(p)
}

// stopped returns ctx's error once ctx is done, and err otherwise: what a read
// that failed while its context was done reports, since the parser quotes a
// read that failed in an error of its own, and its context is what stopped it.
func stopped(ctx context.Context, err error) error {
	if cerr := ctx.Err(); cerr != nil {
		return cerr
	}
	return err
}
