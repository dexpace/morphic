// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package report

import (
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"
)

// printable returns s with each rune strconv.IsPrint refuses spelled as %q
// spells it, a newline as \n and ESC as \x1b, and each byte that is not UTF-8
// as \xNN. A spec writes the keys, references and messages a renderer prints,
// so unescaped one could start a line of its own, or move a terminal's cursor
// or set its title. A backslash stays as written, so text spelling an escape
// reads as one; the JSON report holds each string exactly.
func printable(s string) string {
	if isPrintable(s) {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		switch {
		case r == utf8.RuneError && size == 1:
			fmt.Fprintf(&b, `\x%02x`, s[i])
		case strconv.IsPrint(r):
			b.WriteString(s[i : i+size])
		default:
			quoted := strconv.QuoteRune(r)
			b.WriteString(quoted[1 : len(quoted)-1])
		}
		i += size
	}
	return b.String()
}

// isPrintable reports whether printable leaves s as it is.
func isPrintable(s string) bool {
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		if r == utf8.RuneError && size == 1 || !strconv.IsPrint(r) {
			return false
		}
		i += size
	}
	return true
}
