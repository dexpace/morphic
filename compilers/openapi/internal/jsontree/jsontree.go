// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package jsontree

import (
	"bytes"
	"context"
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"io"
	"strconv"
	"unicode/utf16"

	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
)

// bom is the UTF-8 byte order mark, which the YAML parser skips and counts as
// no column.
const bom = "\xef\xbb\xbf"

// escapeLen is the length of a \uXXXX escape.
const escapeLen = 6

// ErrNotJSON reports data that is not one JSON text: nothing but whitespace,
// text that is not JSON, or more than one value. The caller reads it as YAML.
var ErrNotJSON = errors.New("data is not one JSON text")

// LoneSurrogateError reports a \uXXXX escape naming half of a UTF-16
// surrogate pair, not paired with the other half it needs: beside no other
// half, or beside one out of its order. RFC 8259 §8.2 leaves what such an
// escape means unspecified, and no value of the IR's could hold it.
type LoneSurrogateError struct {
	// Escape is the six bytes as written, in the case written.
	Escape string
	// Line and Column are where the escape begins: lines from one, columns
	// counted in characters from one.
	Line, Column int
}

// Error implements error.
func (e *LoneSurrogateError) Error() string {
	return fmt.Sprintf("line %d, column %d: the escape %s is half of a UTF-16 surrogate pair, "+
		"written without its other half, so it names no character", e.Line, e.Column, e.Escape)
}

// Read returns the document node of data, when data is one JSON text, shaped as
// the YAML parser shapes it (docs/parser-library.md#json-as-yaml): objects and
// arrays in flow style, strings double-quoted, and numbers, true, false and null
// plain, tagged as the parser resolves them
// (docs/parser-library.md#scalar-resolution), a name written twice kept
// (docs/parser-library.md#duplicate-key). Each node holds the line and column
// the text writes it at.
//
// Any other data is ErrNotJSON. An escape naming half of a surrogate pair is a
// *LoneSurrogateError. A done ctx ends Read, within nodeview.AskEvery tokens,
// with its error. Each token consumes input and the decoder bounds depth.
func Read(ctx context.Context, data []byte) (*yaml.Node, error) {
	data = bytes.TrimPrefix(data, []byte(bom))
	dec := jsontext.NewDecoder(bytes.NewReader(data), jsontext.AllowDuplicateNames(true))
	pos := cursor{data: data, line: 1, column: 1}
	doc := &yaml.Node{Kind: yaml.DocumentNode}
	stack := []*yaml.Node{doc}

	for tokens := 0; ; tokens++ {
		if tokens%nodeview.AskEvery == 0 {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
		}
		start := skipSeparators(data, int(dec.InputOffset()))
		tok, err := dec.ReadToken()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, refusal(err, data, start, &pos)
		}
		if kind := tok.Kind(); kind == '}' || kind == ']' {
			stack = stack[:len(stack)-1]
			continue
		}

		n := newNode(tok)
		n.Line, n.Column = pos.at(start)
		top := stack[len(stack)-1]
		top.Content = append(top.Content, n)
		if top == doc {
			doc.Line, doc.Column = n.Line, n.Column
		}
		if n.Kind != yaml.ScalarNode {
			stack = append(stack, n)
		}
	}

	if len(doc.Content) != 1 {
		return nil, ErrNotJSON
	}
	return doc, nil
}

// newNode returns the node for an opening or a scalar token, with the shape
// the YAML parser gives it.
func newNode(tok jsontext.Token) *yaml.Node {
	switch tok.Kind() {
	case '{':
		return &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map", Style: yaml.FlowStyle}
	case '[':
		return &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq", Style: yaml.FlowStyle}
	case '"':
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Style: yaml.DoubleQuotedStyle, Value: tok.String()}
	default:
		// A number, true, false or null, written plain. The parser resolves the
		// tag from the text, so the node asks the parser's resolver.
		n := &yaml.Node{Kind: yaml.ScalarNode, Value: tok.String()}
		n.Tag = n.ShortTag()
		return n
	}
}

// skipSeparators returns the offset of the first byte at or past i that is not
// JSON whitespace, a comma or a colon: where the token the decoder reads next
// begins, since the decoder reports an offset just past the last token.
func skipSeparators(data []byte, i int) int {
	for i < len(data) {
		switch data[i] {
		case ' ', '\t', '\n', '\r', ',', ':':
			i++
		default:
			return i
		}
	}
	return i
}

// refusal classifies err, which the decoder gave on the token beginning at
// start: a *LoneSurrogateError when the token is a string and the decoder
// stopped at a surrogate escape in it, ErrNotJSON wrapping err otherwise. It
// reads the bytes at the decoder's offset, never the decoder's message.
func refusal(err error, data []byte, start int, pos *cursor) error {
	var syntax *jsontext.SyntacticError
	if errors.As(err, &syntax) && start < len(data) && data[start] == '"' {
		if off := int(syntax.ByteOffset); surrogateEscapeAt(data, off) {
			line, column := pos.at(off)
			return &LoneSurrogateError{Escape: string(data[off : off+escapeLen]), Line: line, Column: column}
		}
	}
	return fmt.Errorf("%w: %w", ErrNotJSON, err)
}

// surrogateEscapeAt reports whether data holds, at off, a backslash, a u and
// four hexadecimal digits that read as a code unit from D800 to DFFF.
func surrogateEscapeAt(data []byte, off int) bool {
	if off+escapeLen > len(data) || data[off] != '\\' || data[off+1] != 'u' {
		return false
	}
	unit, err := strconv.ParseUint(string(data[off+2:off+escapeLen]), 16, 16)
	return err == nil && utf16.IsSurrogate(rune(unit))
}

// cursor turns byte offsets into the line and column the YAML parser gives a
// node there: lines from one, columns counted in characters from one. Offsets
// asked for in increasing order cost one pass over the data and never a scan
// back to the start of a line, since a minified spec is a single line.
//
// A line ends at LF, at CR, or at CR LF taken as one. A YAML 1.1 parser also
// ends one at U+0085, U+2028 and U+2029, which JSON tools and editors do not
// (docs/compilers/openapi.md#yaml-decoding).
type cursor struct {
	data         []byte
	off          int
	line, column int
}

// at returns the line and column of the byte at off. An offset before the last
// one asked for starts again from the start of the data.
func (c *cursor) at(off int) (line, column int) {
	if off < c.off {
		*c = cursor{data: c.data, line: 1, column: 1}
	}
	for ; c.off < off; c.off++ {
		switch b := c.data[c.off]; {
		case b == '\r' && c.beforeLF():
			// The LF that follows ends the line.
		case b == '\n' || b == '\r':
			c.line++
			c.column = 1
		case b&0xC0 != 0x80:
			c.column++
		}
	}
	return c.line, c.column
}

// beforeLF reports whether the byte at the cursor is followed by an LF.
func (c *cursor) beforeLF() bool {
	return c.off+1 < len(c.data) && c.data[c.off+1] == '\n'
}
