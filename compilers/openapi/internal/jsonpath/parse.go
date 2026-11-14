// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package jsonpath

import (
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"
)

// MaxQueryBytes bounds the text Parse reads. A query names positions of a
// document, so a real one is a line long; the bound is what keeps one written
// to be long from costing a parse, and the tree it builds, of any size.
const MaxQueryBytes = 1 << 16

// maxDepth bounds how deep each kind of nesting the parser recurses through
// goes: parentheses, filters within filters, and function calls within calls.
// Every recursion of the parser, and of an evaluation over what it built,
// passes through one of them, so together they bound both.
const maxDepth = 64

// maxIndex is the largest magnitude an index or a slice bound may have: RFC
// 9535 section 2.1 holds them to the interoperable integers of I-JSON.
const maxIndex = 1<<53 - 1

// nesting names a kind of nesting the parser counts against maxDepth.
type nesting uint8

const (
	nestFilter nesting = iota
	nestParen
	nestCall
)

// SyntaxError reports text that is no RFC 9535 query: the byte offset at which
// it stops being one, and why.
type SyntaxError struct {
	Reason string
	Offset int
}

// Error returns the reason with its offset.
func (e *SyntaxError) Error() string {
	return fmt.Sprintf("jsonpath: at offset %d: %s", e.Offset, e.Reason)
}

// scanner reads the lexical parts of one query's text: blank space, names,
// numbers and string literals, at an offset it advances.
type scanner struct {
	text string
	pos  int
}

// parser reads one query's grammar over its scanner. open counts the nestings
// of each kind the parse is inside.
type parser struct {
	scanner
	open [nestCall + 1]int
}

// Parse reads text as an RFC 9535 query. A *SyntaxError says where it stops
// being one and why.
func Parse(text string) (Query, error) {
	if len(text) > MaxQueryBytes {
		return Query{}, &SyntaxError{Offset: MaxQueryBytes, Reason: fmt.Sprintf("a query is at most %d bytes", MaxQueryBytes)}
	}
	if bad := firstInvalid(text); bad < len(text) {
		return Query{}, &SyntaxError{Offset: bad, Reason: "the text is not UTF-8"}
	}
	p := &parser{text: text}
	if !p.eat('$') {
		return Query{}, p.fail("a query starts with $")
	}
	segments, err := p.segments()
	if err != nil {
		return Query{}, err
	}
	if p.pos < len(text) {
		return Query{}, p.fail(fmt.Sprintf("%q cannot follow the query before it", p.rest()))
	}
	return Query{segments: segments}, nil
}

// firstInvalid returns the offset of the first byte of text that is no part
// of a UTF-8 character, or its length when every byte is.
func firstInvalid(text string) int {
	i := 0
	for i < len(text) {
		r, size := utf8.DecodeRuneInString(text[i:])
		if r == utf8.RuneError && size == 1 {
			break
		}
		i += size
	}
	return i
}

// fail returns a *SyntaxError at the parser's offset.
func (s *scanner) fail(reason string) *SyntaxError {
	return &SyntaxError{Offset: s.pos, Reason: reason}
}

// rest returns a short quotation of the text from the parser's offset.
func (s *scanner) rest() string {
	r, _ := utf8.DecodeRuneInString(s.text[s.pos:])
	return string(r)
}

// peek returns the byte at the parser's offset, or 0 at the end.
func (s *scanner) peek() byte {
	if s.pos < len(s.text) {
		return s.text[s.pos]
	}
	return 0
}

// eat consumes c if it is next, and reports whether it was.
func (s *scanner) eat(c byte) bool {
	if s.peek() == c {
		s.pos++
		return true
	}
	return false
}

// eatText consumes text if it is next, and reports whether it was.
func (s *scanner) eatText(text string) bool {
	if strings.HasPrefix(s.text[s.pos:], text) {
		s.pos += len(text)
		return true
	}
	return false
}

// blank consumes RFC 9535's blank space: spaces, tabs, line feeds and carriage
// returns. It reports whether there was any.
func (s *scanner) blank() bool {
	start := s.pos
	for s.pos < len(s.text) && strings.IndexByte(" \t\n\r", s.text[s.pos]) >= 0 {
		s.pos++
	}
	return s.pos > start
}

// enter counts one more nesting of kind k, refusing one past maxDepth.
func (p *parser) enter(k nesting) error {
	if p.open[k] == maxDepth {
		return p.fail(fmt.Sprintf("the query nests %s past the depth of %d the compiler reads", [...]string{
			"filters", "parentheses", "function calls"}[k], maxDepth))
	}
	p.open[k]++
	return nil
}

// leave ends a nesting enter counted.
func (p *parser) leave(k nesting) { p.open[k]-- }

// segments reads the segments following a root or current-node identifier, as
// many as follow, each perhaps after blank space.
func (p *parser) segments() ([]segment, error) {
	var out []segment
	for {
		save := p.pos
		p.blank()
		if c := p.peek(); c != '.' && c != '[' {
			p.pos = save
			return out, nil
		}
		s, err := p.segment()
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
}

// segment reads one child or descendant segment, at a dot or a bracket.
func (p *parser) segment() (segment, error) {
	if p.eat('[') {
		return p.bracketed(false)
	}
	p.pos++ // the dot
	descendant := p.eat('.')
	switch {
	case p.eat('*'):
		return segment{selectors: []selector{{kind: selWildcard}}, descendant: descendant}, nil
	case descendant && p.eat('['):
		return p.bracketed(true)
	}
	name, err := p.shorthand()
	if err != nil {
		return segment{}, err
	}
	return segment{selectors: []selector{{kind: selName, name: name}}, descendant: descendant, singular: !descendant}, nil
}

// shorthand reads a member-name-shorthand: a letter, an underscore or a
// character past ASCII but the surrogates, then any of those or digits.
func (s *scanner) shorthand() (string, error) {
	start := s.pos
	for s.pos < len(s.text) {
		r, size := utf8.DecodeRuneInString(s.text[s.pos:])
		if !isNameChar(r) || s.pos == start && r >= '0' && r <= '9' {
			break
		}
		s.pos += size
	}
	if s.pos == start {
		return "", s.fail("a dot is followed by a member name or *")
	}
	return s.text[start:s.pos], nil
}

// isNameChar reports whether r may stand in a member-name-shorthand.
func isNameChar(r rune) bool {
	return r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' ||
		r >= 0x80 && r <= 0xD7FF || r >= 0xE000 && r <= 0x10FFFF
}

// bracketed reads a bracketed selection after its bracket: selectors separated
// by commas, blank space around each. One name or index selector written with
// no blank space is a singular segment (RFC 9535 section 2.3.5.1).
func (p *parser) bracketed(descendant bool) (segment, error) {
	s := segment{descendant: descendant}
	spaced := false
	for {
		spaced = p.blank() || spaced
		sel, err := p.selector()
		if err != nil {
			return segment{}, err
		}
		s.selectors = append(s.selectors, sel)
		spaced = p.blank() || spaced
		if p.eat(']') {
			break
		}
		if !p.eat(',') {
			return segment{}, p.fail("a selector is followed by a comma or ]")
		}
	}
	kind := s.selectors[0].kind
	s.singular = !descendant && !spaced && len(s.selectors) == 1 && (kind == selName || kind == selIndex)
	return s, nil
}

// selector reads one selector of a bracketed selection.
func (p *parser) selector() (selector, error) {
	switch c := p.peek(); {
	case c == '\'' || c == '"':
		name, err := p.stringLiteral()
		return selector{kind: selName, name: name}, err
	case c == '*':
		p.pos++
		return selector{kind: selWildcard}, nil
	case c == '?':
		p.pos++
		return p.filterSelector()
	case c == ':' || c == '-' || c >= '0' && c <= '9':
		return p.indexOrSlice()
	default:
		return selector{}, p.fail("a selector is a name, *, an index, a slice or a filter")
	}
}

// filterSelector reads a filter's logical expression after its ?.
func (p *parser) filterSelector() (selector, error) {
	if err := p.enter(nestFilter); err != nil {
		return selector{}, err
	}
	defer p.leave(nestFilter)
	p.blank()
	expr, err := p.logicalOr()
	if err != nil {
		return selector{}, err
	}
	return selector{kind: selFilter, filter: expr}, nil
}

// indexOrSlice reads an index selector, or a slice selector, whose start is
// optional.
func (p *parser) indexOrSlice() (selector, error) {
	var sl slice
	if p.peek() != ':' {
		start, err := p.integer()
		if err != nil {
			return selector{}, err
		}
		save := p.pos
		p.blank()
		if p.peek() != ':' {
			p.pos = save
			return selector{kind: selIndex, index: start}, nil
		}
		sl.start, sl.hasStart = start, true
	}
	p.pos++ // the first colon
	p.blank()
	if c := p.peek(); c == '-' || c >= '0' && c <= '9' {
		end, err := p.integer()
		if err != nil {
			return selector{}, err
		}
		sl.end, sl.hasEnd = end, true
		p.blank()
	}
	sl.step = 1
	if !p.eat(':') {
		return selector{kind: selSlice, slice: sl}, nil
	}
	p.blank()
	if c := p.peek(); c == '-' || c >= '0' && c <= '9' {
		step, err := p.integer()
		if err != nil {
			return selector{}, err
		}
		sl.step = step
	}
	return selector{kind: selSlice, slice: sl}, nil
}

// integer reads an int: 0, or a nonzero digit after an optional minus, then
// digits, within the interoperable range.
func (s *scanner) integer() (int, error) {
	start := s.pos
	s.eat('-')
	digits := s.pos
	for c := s.peek(); c >= '0' && c <= '9'; c = s.peek() {
		s.pos++
	}
	text := s.text[start:s.pos]
	switch {
	case s.pos == digits:
		return 0, s.fail("an integer has a digit")
	case s.text[digits] == '0' && (s.pos-digits > 1 || digits > start):
		return 0, &SyntaxError{Offset: start, Reason: fmt.Sprintf("the integer %s is spelled with a leading zero or as -0", text)}
	}
	n, err := strconv.ParseInt(text, 10, 64)
	if err != nil || n > maxIndex || n < -maxIndex {
		return 0, &SyntaxError{Offset: start, Reason: fmt.Sprintf("the integer %s is past the range of ±(2^53-1)", text)}
	}
	return int(n), nil
}

// stringLiteral reads a string literal quoted by its first byte, and returns
// the string it spells.
func (s *scanner) stringLiteral() (string, error) {
	quote := s.text[s.pos]
	s.pos++
	var b strings.Builder
	for {
		if s.pos >= len(s.text) {
			return "", s.fail("a string literal is not closed")
		}
		c := s.text[s.pos]
		switch {
		case c == quote:
			s.pos++
			return b.String(), nil
		case c == '\\':
			if err := s.escape(&b, quote); err != nil {
				return "", err
			}
		case c < 0x20:
			return "", s.fail("a string literal holds a control character, which it escapes")
		default:
			r, size := utf8.DecodeRuneInString(s.text[s.pos:])
			b.WriteRune(r)
			s.pos += size
		}
	}
}

// escape reads the escape at the parser's offset in a literal quoted by quote,
// writing the character it names to b.
func (s *scanner) escape(b *strings.Builder, quote byte) error {
	s.pos++ // the backslash
	c := s.peek()
	if i := strings.IndexByte(`bfnrt/\`, c); i >= 0 {
		b.WriteByte("\b\f\n\r\t/\\"[i])
		s.pos++
		return nil
	}
	if c == quote {
		b.WriteByte(quote)
		s.pos++
		return nil
	}
	if c != 'u' {
		return s.fail("a backslash is followed by one of b, f, n, r, t, /, \\, u or the quote")
	}
	r, err := s.unicodeEscape()
	if err != nil {
		return err
	}
	b.WriteRune(r)
	return nil
}

// unicodeEscape reads the u and four hexadecimal digits of an escape, and the
// escape of a low surrogate after one of a high, returning the character they
// name.
func (s *scanner) unicodeEscape() (rune, error) {
	start := s.pos - 1
	v, ok := hex4(s.text, s.pos+1)
	switch {
	case !ok:
		return 0, s.fail(`\u is followed by four hexadecimal digits`)
	case v >= 0xDC00 && v <= 0xDFFF:
		return 0, &SyntaxError{Offset: start, Reason: `a \u escape names a low surrogate with no high one before it`}
	case v < 0xD800 || v > 0xDBFF:
		s.pos += 5
		return rune(v), nil
	}
	low, ok := hex4(s.text, s.pos+7)
	if !ok || s.text[s.pos+5:s.pos+7] != `\u` || low < 0xDC00 || low > 0xDFFF {
		return 0, &SyntaxError{Offset: start, Reason: `a \u escape names a high surrogate with no low one after it`}
	}
	s.pos += 11
	return (rune(v)-0xD800)<<10 + (rune(low) - 0xDC00) + 0x10000, nil
}

// hex4 reads the four hexadecimal digits at i of s, and whether there are
// four.
func hex4(s string, i int) (uint64, bool) {
	if i+4 > len(s) {
		return 0, false
	}
	v, err := strconv.ParseUint(s[i:i+4], 16, 32)
	return v, err == nil
}
