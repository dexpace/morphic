package barrier

import (
	"fmt"
	"reflect"
	"strings"
	"unicode"
	"unicode/utf8"
)

// maxText bounds the text of a panic value in a message. The value belongs to
// code that obeys no limit, and a message is read on one line.
const maxText = 4 << 10

// scanLimit bounds how much of a panic value's text oneLine reads, so the work
// is bounded by what is kept and not by what the value wrote.
const scanLimit = 4 * maxText

// noText stands in for a panic value whose text is empty or only white space,
// so the message still says that something panicked.
const noText = "(no text)"

// elidedMarker ends a message the bound cut. It carries no count: a count would
// make the message depend on the whole text again.
const elidedMarker = "… (elided)"

// describe renders the panic value r without trusting it to render itself.
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

// oneLine makes text fit a message: well-formed UTF-8, on one line, free of
// control characters a terminal would act on, and at most maxText bytes, cut on
// a rune boundary and marked when it was cut.
func oneLine(text string) string {
	text, readCut := clip(text, scanLimit)
	text = strings.ToValidUTF8(text, string(utf8.RuneError))
	text = strings.Join(strings.Fields(text), " ")
	text = strings.Map(visible, text)
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

// visible maps a control character, which Fields has left only where it is not
// white space, to U+FFFD, and keeps every other rune.
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
