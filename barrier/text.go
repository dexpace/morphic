// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package barrier

import (
	"fmt"
	"reflect"
	"strings"
	"unicode"
	"unicode/utf8"
)

// maxText bounds the text of a value in a message. The value belongs to code
// that obeys no limit, and a message is read on one line. It is generous
// because the errors worth quoting are lists, such as a validator's sentence
// per finding, and a list cut to its first entry says less than a reader needs.
const maxText = 4 << 10

// scanLimit bounds how much of a value's text render reads, so the work is
// bounded by what is kept and not by what the value wrote.
const scanLimit = 4 * maxText

// noText stands in for a value whose text is empty or only white space, so a
// message quoting it still says something.
const noText = "(no text)"

// elidedMarker ends a text the bound cut. It carries no count: a count would
// make the text depend on the whole value again.
const elidedMarker = "… (elided)"

// Line renders v, what code the caller does not own returned or panicked with,
// as text a one-line message can quote: each line's white space collapsed to
// single spaces, and the lines joined with "; ", or with a space after one
// that ends in ":", ";", ",", ".", "!" or "?". It is well-formed UTF-8, never
// empty, cut on a rune boundary and marked past 4 KiB, and keeps other control
// characters for the renderer to escape.
//
// An error, a Stringer, a string, a number and a bool render as they say; any
// other value, and one whose text panics, is named by its type.
func Line(v any) string {
	return render(describe(v), asIs)
}

// describe renders the value r without trusting it to render itself.
// An error and a Stringer say what they say, a string is itself, and a number
// or a bool prints as it is. Anything else is named by its type alone: fmt
// follows a map or a slice into itself without end, and a stack overflow is
// not one a barrier can recover.
func describe(r any) (text string) {
	defer func() {
		if recover() != nil {
			text = fmt.Sprintf("%T whose text panicked", r)
		}
	}()

	switch v := r.(type) {
	case error:
		return v.Error()
	case fmt.Stringer:
		return v.String()
	case string:
		return v
	default:
		if r != nil && isScalar(reflect.TypeOf(r).Kind()) {
			return fmt.Sprint(r)
		}
		return fmt.Sprintf("%T", r)
	}
}

// isScalar reports whether a value of kind k prints as itself in finite space.
func isScalar(k reflect.Kind) bool {
	switch k {
	case reflect.Bool, reflect.String,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr,
		reflect.Float32, reflect.Float64, reflect.Complex64, reflect.Complex128:
		return true
	default:
		return false
	}
}

// render makes text fit a message: well-formed UTF-8, its lines joined, each
// rune mapped through glyph, and at most maxText bytes, cut on a rune boundary
// and marked when it was cut.
func render(text string, glyph func(rune) rune) string {
	text, readCut := clip(text, scanLimit)
	text = strings.ToValidUTF8(text, string(utf8.RuneError))
	text = strings.Map(glyph, joinLines(text))
	text, keptCut := clip(text, maxText)

	switch {
	case readCut || keptCut:
		return text + elidedMarker
	case text == "":
		return noText
	default:
		return text
	}
}

// spaceAfter holds the marks after which the next line follows a space rather
// than "; ": a colon heads what follows, and the others already end a clause,
// which a semicolon after them would end twice.
const spaceAfter = ":;,.!?"

// joinLines puts text on one line: the white space within each line collapsed
// to single spaces, blank lines dropped, and the rest joined with "; ", or with
// a space after a line ending in a mark of spaceAfter. It stops once past
// maxText, so the work is bounded by what clip keeps.
func joinLines(text string) string {
	var out strings.Builder
	for line := range strings.FieldsFuncSeq(text, isLineBreak) {
		sep := "; "
		switch s := out.String(); {
		case s == "":
			sep = ""
		case strings.IndexByte(spaceAfter, s[len(s)-1]) >= 0:
			sep = " "
		}
		for word := range strings.FieldsSeq(line) {
			if out.Len() > maxText {
				return out.String()
			}
			out.WriteString(sep)
			out.WriteString(word)
			sep = " "
		}
	}
	return out.String()
}

// isLineBreak reports whether r ends a line, as Unicode's mandatory breaks do:
// line feed, carriage return, the vertical tab and form feed, next line, and
// the line and paragraph separators.
func isLineBreak(r rune) bool {
	switch r {
	case '\n', '\r', '\v', '\f', '\u0085', '\u2028', '\u2029':
		return true
	default:
		return false
	}
}

// asIs keeps r, for a text whose renderer escapes what it must.
func asIs(r rune) rune { return r }

// visible maps a control character, which joinLines has left only where it is
// neither white space nor a line break, to U+FFFD, and keeps every other rune.
func visible(r rune) rune {
	if unicode.IsControl(r) {
		return utf8.RuneError
	}
	return r
}

// clip returns s cut to at most n bytes, on a rune boundary, and whether it cut.
func clip(s string, n int) (string, bool) {
	if len(s) <= n {
		return s, false
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n], true
}
