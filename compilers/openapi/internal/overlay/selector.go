// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package overlay

import (
	"fmt"
	"strconv"
	"strings"
	"unicode/utf16"

	soaoverlay "github.com/speakeasy-api/openapi/overlay"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/ir"
)

// readSelector reads selector, the action's field named field, as the RFC 9535
// query the specification makes every target and copy, through doc's own
// constructor, which doc's dialect makes the library's RFC 9535 parser. Where
// that parser and RFC 9535 part, what RFC 9535 refuses (`~`, a member name
// after a dot its shorthand does not admit, an unescaped control character, a
// malformed escape) is an openapi/invalid-overlay error, and what the parser
// refuses or misreads though RFC 9535 reads it
// (docs/parser-library.md#jsonpath-rfc9535) is an openapi/overlay-unsupported
// one, each naming it. Any other parse failure is invalid, quoting the parser.
func readSelector(doc *soaoverlay.Overlay, a action, field, selector string) (soaoverlay.Queryable, []ir.Diagnostic) {
	facts := scanSelector(selector)
	path, err := doc.NewPath(selector, nil)
	subject := "its target"
	if field != "target" {
		subject = fmt.Sprintf("its %s %q", field, selector)
	}
	refuse := func(code, reason string) (soaoverlay.Queryable, []ir.Diagnostic) {
		return nil, []ir.Diagnostic{diag.Newf(ir.SeverityError, code, a.overlay, "%s: %s %s", a.label(), subject, reason)}
	}
	switch {
	case facts.foreign != "":
		return refuse(diag.OverlayInvalid, facts.foreign)
	case err != nil && facts.unread != "":
		return refuse(diag.OverlayUnsupported, facts.unread)
	case err != nil:
		return refuse(diag.OverlayInvalid, "does not parse as a JSONPath query: "+firstLine(err))
	case facts.misread != "":
		return refuse(diag.OverlayUnsupported, facts.misread)
	}
	return path, nil
}

// selectorFacts is what scanSelector finds in a selector's text, each the
// reason the first construct of its class gives, or empty.
type selectorFacts struct {
	// foreign names a construct RFC 9535 refuses and the parser accepts.
	foreign string
	// unread names one RFC 9535 reads and the parser refuses.
	unread string
	// misread names one the parser accepts and reads otherwise than RFC 9535.
	misread string
}

// note records reason in the field of f that slot names, unless an earlier
// construct of that class already did.
func (f *selectorFacts) note(slot *string, reason string) {
	if *slot == "" {
		*slot = reason
	}
}

// firstLine is the first line of err's message: the parser's own reason, with
// the line and column it names. The lines after it repeat the selector, which
// the finding already quotes, under a caret no one line can align.
func firstLine(err error) string {
	line, _, _ := strings.Cut(err.Error(), "\n")
	return strings.TrimSpace(line)
}

// scanSelector reads a selector's text, telling its string literals, member
// names and numbers from the rest by RFC 9535's grammar, for the constructs on
// which the parser library's JSONPath parser parts from RFC 9535: `~`, which
// it reads (docs/parser-library.md#overlay-jsonpath-dialect), a member name
// after a dot that RFC 9535 does not admit there, which the parser refuses
// without saying why, and the ones docs/parser-library.md#jsonpath-rfc9535
// records. It reads the text alone, not a parse, so it answers for a selector
// neither reading accepts too; readSelector decides what each answer means.
func scanSelector(s string) selectorFacts {
	var f selectorFacts
	var quote byte // the quote of the string literal the scan is in, 0 outside one
	for i := 0; i < len(s); i++ {
		if quote != 0 {
			i, quote = scanInString(&f, s, i, quote)
			continue
		}
		switch c := s[i]; {
		case c == '\'' || c == '"':
			quote = c
		case c == '~':
			f.note(&f.foreign, "uses ~, which RFC 9535 does not define")
		case c == '.':
			i = scanShorthand(&f, s, i)
		case startsNumber(s, i):
			i = numberEnd(s, i) - 1
		default:
			if fn, ok := callAt(s, i); ok {
				f.note(&f.misread, fmt.Sprintf("calls %s(), which the compiler's JSONPath evaluator answers true "+
					"for a node lacking the value it tests", fn))
			}
		}
	}
	return f
}

// scanShorthand reads the member name written after the dot, or the two dots,
// at i of s, and returns the index of the last byte it read. The name runs to
// the first byte that ends one in RFC 9535's grammar, so a byte the shorthand
// does not admit, such as the `-` of every extension key, is read as part of
// the name a writer meant, and the finding names what to write instead.
func scanShorthand(f *selectorFacts, s string, i int) int {
	start := i + 1
	if start < len(s) && s[start] == '.' {
		start++
	}
	end := start
	for end < len(s) && !endsName(s[end]) {
		end++
	}
	switch name := s[start:end]; {
	case name == "":
	case !isShorthand(name):
		f.note(&f.foreign, fmt.Sprintf("names the member %q after a dot, where RFC 9535 admits only letters, "+
			"digits, underscores and characters past ASCII, starting with no digit; write it %s", name, bracketed(name)))
	case name == "true" || name == "false" || name == "null":
		f.note(&f.unread, fmt.Sprintf("names the member %q after a dot, which the compiler's JSONPath parser "+
			"cannot read; write it %s", name, bracketed(name)))
	}
	return end - 1
}

// endsName reports whether c ends a member name written after a dot: a byte
// that opens or closes a selector, a segment, a string, a filter or a call,
// separates them, compares or joins, `~`, or blank space.
func endsName(c byte) bool {
	return strings.IndexByte(".[]()*,?=!<>&|'\"~ \t\n\r", c) >= 0
}

// isShorthand reports whether name is a member-name-shorthand by RFC 9535's
// grammar: a letter, an underscore or a character past ASCII, then any number
// of those and digits.
func isShorthand(name string) bool {
	for i := range len(name) {
		if !isNameChar(name[i]) || i == 0 && name[i] >= '0' && name[i] <= '9' {
			return false
		}
	}
	return true
}

// bracketed spells name as the name selector that selects it, quoted in
// single quotes, with a backslash and a quote in it escaped.
func bracketed(name string) string {
	return "['" + strings.NewReplacer(`\`, `\\`, `'`, `\'`).Replace(name) + "']"
}

// startsNumber reports whether a number literal starts at i of s: a digit, or
// a minus before one. A number's own dot, as in 1.5, is then read with it,
// never as a member name's. A member name's digits are read with the name
// after its dot, so any other digit is a number's, an exponent's included.
func startsNumber(s string, i int) bool {
	if s[i] == '-' {
		i++
	}
	return i < len(s) && s[i] >= '0' && s[i] <= '9'
}

// numberEnd returns the index past the integer and fraction of the number
// literal starting at i of s. An exponent needs no reading of its own: it
// holds no dot a member name could follow.
func numberEnd(s string, i int) int {
	digits := func(i int) int {
		for i < len(s) && s[i] >= '0' && s[i] <= '9' {
			i++
		}
		return i
	}
	if s[i] == '-' {
		i++
	}
	i = digits(i)
	if i+1 < len(s) && s[i] == '.' && s[i+1] >= '0' && s[i+1] <= '9' {
		i = digits(i + 1)
	}
	return i
}

// scanInString reads the byte at i of s, inside a string literal opened by
// quote, and returns the index of the last byte it read and the quote the scan
// is in after it: 0 once the literal closes.
func scanInString(f *selectorFacts, s string, i int, quote byte) (int, byte) {
	switch c := s[i]; {
	case c == quote:
		return i, 0
	case c == '\\' && i+1 < len(s):
		switch s[i+1] {
		case 'r':
			f.note(&f.misread, `writes the escape \r, which the compiler's JSONPath parser reads as a line feed`)
		case 'u':
			if r, n := unicodeEscape(s, i); n > 0 {
				f.note(&f.unread, `writes a \u escape, which the compiler's JSONPath parser cannot read; `+spelledOtherwise(r, quote))
				return i + n - 1, quote
			}
			f.note(&f.foreign, `writes a \u escape of other than four hexadecimal digits naming a character, which RFC 9535 refuses`)
		default:
		}
		return i + 1, quote
	case c < 0x20:
		f.note(&f.foreign, "holds a control character unescaped in a string, which RFC 9535 refuses")
	default:
	}
	return i, quote
}

// unicodeEscape returns the character the escape at i of s, a backslash and a
// u, names and its length, when it is one RFC 9535's hexchar admits: four
// hexadecimal digits naming no surrogate, or a high surrogate's followed by an
// escape of a low one. It returns a length of 0 for any other.
func unicodeEscape(s string, i int) (rune, int) {
	v, ok := hex4(s, i+2)
	switch {
	case !ok || v >= 0xDC00 && v <= 0xDFFF:
		return 0, 0
	case v < 0xD800 || v > 0xDBFF:
		return rune(v), 6
	}
	if low, ok := hex4(s, i+8); ok && s[i+6:i+8] == `\u` && low >= 0xDC00 && low <= 0xDFFF {
		return utf16.DecodeRune(rune(v), rune(low)), 12
	}
	return 0, 0
}

// spelledOtherwise says how a string literal quoted by quote writes r without
// a \u escape: the character itself, or the escape RFC 9535 gives it where it
// may not stand alone. A control character with no such escape the compiler's
// parser reads, \r among them, has no spelling it reads.
func spelledOtherwise(r rune, quote byte) string {
	if r == rune(quote) || r == '\\' {
		return `write \` + string(r) + " instead"
	}
	if i := strings.IndexRune("\b\t\n\f", r); i >= 0 {
		return `write \` + "btnf"[i:i+1] + " instead"
	}
	if r < 0x20 {
		return "no other spelling of that character is one it reads"
	}
	return fmt.Sprintf("write the character it names, %#U, itself", r)
}

// hex4 reads the four hexadecimal digits at i of s, and whether there are
// four.
func hex4(s string, i int) (uint64, bool) {
	if i < 0 || i+4 > len(s) {
		return 0, false
	}
	v, err := strconv.ParseUint(s[i:i+4], 16, 16)
	return v, err == nil
}

// callAt reports whether a call of match() or search() starts at i of s, and
// which. RFC 9535 writes a function's name and its parenthesis with nothing
// between them.
func callAt(s string, i int) (string, bool) {
	if i > 0 && isNameChar(s[i-1]) {
		return "", false
	}
	for _, fn := range []string{"match", "search"} {
		if strings.HasPrefix(s[i:], fn+"(") {
			return fn, true
		}
	}
	return "", false
}

// isNameChar reports whether c can be part of an RFC 9535 member name: a
// letter, a digit, an underscore, or a byte of a character past ASCII.
func isNameChar(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c >= 0x80
}
