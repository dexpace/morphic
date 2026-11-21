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
// openapi/overlay-action warning saying why, and one writing `^` or `$` as an
// anchor that may match is an openapi/overlay-anchored-pattern info.
func readSelector(a action, field, selector string) (jsonpath.Query, []ir.Diagnostic) {
	subject := "its target"
	if field != "target" {
		subject = fmt.Sprintf("its %s %q", field, selector)
	}
	query, err := jsonpath.Parse(selector)
	syntax, failed := errors.AsType[*jsonpath.SyntaxError](err)
	if !failed {
		return query, patternNotes(a, subject, query)
	}
	reason := syntax.Reason
	if hint := hintFor(selector, syntax); hint != "" {
		reason += "; it " + hint
	}
	return jsonpath.Query{}, []ir.Diagnostic{diag.Newf(ir.SeverityError, diag.OverlayInvalid, a.overlay,
		"%s: %s does not read as an RFC 9535 query at offset %d: %s", a.label(), subject, syntax.Offset, reason)}
}

// patternNotes reports what each literal pattern query hands match or search
// calls for (jsonpath.PatternNote): a warning for one that matches no string,
// so the call never holds and the selector selects nothing by it, and an info
// for one whose `^` or `$` is read as an anchor where RFC 9485's grammar reads
// the character, which a pattern matching no string is not told again.
func patternNotes(a action, subject string, query jsonpath.Query) []ir.Diagnostic {
	notes := query.PatternNotes()
	if len(notes) == 0 {
		return nil
	}
	out := make([]ir.Diagnostic, 0, len(notes))
	for _, patternNote := range notes {
		if patternNote.Dead {
			out = append(out, diag.Newf(ir.SeverityWarning, diag.OverlayAction, a.overlay,
				"%s: %s writes the pattern %q, which matches no string: %s", a.label(), subject, patternNote.Pattern, patternNote.Reason))
			continue
		}
		out = append(out, diag.Newf(ir.SeverityInfo, diag.OverlayAnchoredPattern, a.overlay,
			"%s: %s writes the pattern %q, whose %s", a.label(), subject, patternNote.Pattern, patternNote.Reason))
	}
	return out
}

// hintFor returns what to say of the selector s that syntaxError reports stops
// being a query, or nothing: a `~` where it stops, or a member name after a dot
// that the shorthand does not admit where the parse stopped in one.
func hintFor(s string, syntaxError *jsonpath.SyntaxError) string {
	switch {
	case syntaxError.Offset < len(s) && s[syntaxError.Offset] == '~':
		return "uses ~, which RFC 9535 does not define"
	case syntaxError.Dot > 0:
		return shorthandHint(s, syntaxError.Dot)
	default:
		return ""
	}
}

// shorthandHint reads the member name written after the dot at i of s, and
// returns what to say of a name the shorthand does not admit. The name runs
// to the first byte that ends one in RFC 9535's grammar, so a byte the
// shorthand does not admit, such as the `-` of every extension key, is read as
// part of the name a writer meant, and the hint names what to write instead.
func shorthandHint(s string, i int) string {
	end := i + 1
	for end < len(s) && !endsName(s[end]) {
		end++
	}
	if name := s[i+1 : end]; name != "" && !isShorthand(name) {
		return fmt.Sprintf("names the member %q after a dot, where RFC 9535 admits only letters, "+
			"digits, underscores and characters past ASCII, starting with no digit; write it %s", name, bracketed(name))
	}
	return ""
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

// isNameChar reports whether c can be part of an RFC 9535 member name: a
// letter, a digit, an underscore, or a byte of a character past ASCII.
func isNameChar(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c >= 0x80
}
