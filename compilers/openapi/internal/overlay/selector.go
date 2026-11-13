// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package overlay

import (
	"fmt"
	"strconv"
	"strings"

	soaoverlay "github.com/speakeasy-api/openapi/overlay"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/ir"
)

// readSelector reads selector, the action's field named field, as the RFC 9535
// query the specification makes every target and copy, through doc's own
// constructor, which doc's dialect makes the library's RFC 9535 parser. It is
// refused where that parser and RFC 9535 part:
//
//  1. what RFC 9535 refuses and the parser accepts or refuses for another
//     reason, the property-name operator `~`, a control character unescaped
//     in a string and a malformed escape, is an openapi/invalid-overlay error;
//  2. what the parser refuses or misreads though RFC 9535 reads it, which
//     docs/parser-library.md#jsonpath-rfc9535 records, is an
//     openapi/overlay-failed error naming it;
//  3. anything else the parser refuses is an openapi/invalid-overlay error
//     quoting it.
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
		return refuse(diag.OverlayFailed, facts.unread)
	case err != nil:
		return refuse(diag.OverlayInvalid, "does not parse as a JSONPath query: "+diag.OneLine(err))
	case facts.misread != "":
		return refuse(diag.OverlayFailed, facts.misread)
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

// scanSelector reads a selector's text, telling its string literals from the
// rest by RFC 9535's grammar, for the constructs on which the parser library's
// JSONPath parser parts from RFC 9535: `~`, which it reads
// (docs/parser-library.md#overlay-jsonpath-dialect), and the ones
// docs/parser-library.md#jsonpath-rfc9535 records. It reads the text alone,
// not a parse, so it answers for a selector neither reading accepts too;
// readSelector decides what each answer means.
func scanSelector(s string) selectorFacts {
	var f selectorFacts
	var quote byte // the quote of the string literal the scan is in, 0 outside one
	for i := 0; i < len(s); i++ {
		if quote != 0 {
			i, quote = scanInString(&f, s, i, quote)
			continue
		}
		switch c := s[i]; c {
		case '\'', '"':
			quote = c
		case '~':
			f.note(&f.foreign, "uses ~, which RFC 9535 does not define")
		case '.':
			if name := shorthandAfter(s, i); name == "true" || name == "false" || name == "null" {
				f.note(&f.unread, fmt.Sprintf("names the member %q after a dot, which the compiler's JSONPath parser "+
					"cannot read; write it ['%s']", name, name))
			}
		default:
			if fn, ok := callAt(s, i); ok {
				f.note(&f.misread, fmt.Sprintf("calls %s(), which the compiler's JSONPath evaluator answers true "+
					"for a node lacking the value it tests", fn))
			}
		}
	}
	return f
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
			if n := unicodeEscape(s, i); n > 0 {
				f.note(&f.unread, `writes a \u escape, which the compiler's JSONPath parser cannot read`)
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

// unicodeEscape returns the length of the escape at i of s, a backslash and a
// u, when it is one RFC 9535's hexchar admits: four hexadecimal digits naming
// no surrogate, or a high surrogate's followed by an escape of a low one. It
// returns 0 for any other.
func unicodeEscape(s string, i int) int {
	v, ok := hex4(s, i+2)
	switch {
	case !ok || v >= 0xDC00 && v <= 0xDFFF:
		return 0
	case v < 0xD800 || v > 0xDBFF:
		return 6
	}
	if low, ok := hex4(s, i+8); ok && s[i+6:i+8] == `\u` && low >= 0xDC00 && low <= 0xDFFF {
		return 12
	}
	return 0
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

// shorthandAfter returns the member name written after the dot, or the two
// dots, at i of s: the run of RFC 9535's name characters that follows them.
func shorthandAfter(s string, i int) string {
	start := i + 1
	if start < len(s) && s[start] == '.' {
		start++
	}
	end := start
	for end < len(s) && isNameChar(s[end]) {
		end++
	}
	return s[start:end]
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
