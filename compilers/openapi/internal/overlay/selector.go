// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package overlay

import (
	"errors"
	"fmt"
	"strings"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/jsonpath"
	"github.com/dexpace/morphic/ir"
)

// readSelector reads selector, the action's field named field, as the RFC 9535
// query the specification makes every target and copy. Text RFC 9535 does not
// read is an openapi/invalid-overlay error naming the offset at which it stops
// being a query and why, with a hint for the two spellings other dialects
// read: `~`, and a member name after a dot that the shorthand does not admit.
// A pattern the query writes that matches no string is an
// openapi/overlay-action warning saying why.
func readSelector(a action, field, selector string) (jsonpath.Query, []ir.Diagnostic) {
	subject := "its target"
	if field != "target" {
		subject = fmt.Sprintf("its %s %q", field, selector)
	}
	q, err := jsonpath.Parse(selector)
	syntax, failed := errors.AsType[*jsonpath.SyntaxError](err)
	if !failed {
		return q, deadPatterns(a, subject, q)
	}
	reason := syntax.Reason
	if hint := hintFor(selector); hint != "" {
		reason += "; it " + hint
	}
	return jsonpath.Query{}, []ir.Diagnostic{diag.Newf(ir.SeverityError, diag.OverlayInvalid, a.overlay,
		"%s: %s does not read as an RFC 9535 query at offset %d: %s", a.label(), subject, syntax.Offset, reason)}
}

// deadPatterns warns of each pattern q hands match or search that matches no
// string, so the call never holds and the selector selects nothing by it.
func deadPatterns(a action, subject string, q jsonpath.Query) []ir.Diagnostic {
	dead := q.DeadPatterns()
	if len(dead) == 0 {
		return nil
	}
	out := make([]ir.Diagnostic, 0, len(dead))
	for _, d := range dead {
		out = append(out, diag.Newf(ir.SeverityWarning, diag.OverlayAction, a.overlay,
			"%s: %s writes the pattern %q, which matches no string: %s", a.label(), subject, d.Pattern, d.Reason))
	}
	return out
}

// hintFor reads a selector's text, telling its string literals from the rest
// by RFC 9535's grammar, for the first `~` or the first member name after a
// dot that the shorthand does not admit, and returns what to say of it, or
// nothing. It reads the text alone, not a parse.
func hintFor(s string) string {
	var quote byte // the quote of the string literal the scan is in, 0 outside one
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case quote != 0 && c == '\\':
			i++
		case quote != 0:
			if c == quote {
				quote = 0
			}
		case c == '\'' || c == '"':
			quote = c
		case c == '~':
			return "uses ~, which RFC 9535 does not define"
		case c == '.':
			hint, end := shorthandHint(s, i)
			if hint != "" {
				return hint
			}
			i = end
		case startsNumber(s, i):
			i = numberEnd(s, i) - 1
		}
	}
	return ""
}

// shorthandHint reads the member name written after the dot, or the two
// dots, at i of s, and returns what to say of a name the shorthand does not
// admit, and the index of the last byte it read. The name runs to the first
// byte that ends one in RFC 9535's grammar, so a byte the shorthand does not
// admit, such as the `-` of every extension key, is read as part of the name
// a writer meant, and the hint names what to write instead.
func shorthandHint(s string, i int) (string, int) {
	start := i + 1
	if start < len(s) && s[start] == '.' {
		start++
	}
	end := start
	for end < len(s) && !endsName(s[end]) {
		end++
	}
	if name := s[start:end]; name != "" && !isShorthand(name) {
		return fmt.Sprintf("names the member %q after a dot, where RFC 9535 admits only letters, "+
			"digits, underscores and characters past ASCII, starting with no digit; write it %s", name, bracketed(name)), end
	}
	return "", end - 1
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

// isNameChar reports whether c can be part of an RFC 9535 member name: a
// letter, a digit, an underscore, or a byte of a character past ASCII.
func isNameChar(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c >= 0x80
}
