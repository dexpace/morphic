// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package nodeview

import "strings"

// CoreInteger is an integer as YAML 1.2's core schema spells one,
// [-+]?[0-9]+, 0o[0-7]+ or 0x[0-9a-fA-F]+
// (docs/parser-library.md#scalar-resolution), split as written and never
// materialized, so a reader of any length of digits pays for what it builds.
type CoreInteger struct {
	// Digits are the digits written after the sign or the prefix.
	Digits string
	// Base is 10, 8 or 16.
	Base int
	// Negative says a minus sign is written, which only base ten takes.
	Negative bool
}

// ParseCoreInteger reads text as a CoreInteger, and false for text the core
// schema reads as no integer. It is the one reading of that grammar: the
// reader's tag, the number readers and the rule table's check take it here.
func ParseCoreInteger(text string) (CoreInteger, bool) {
	c := CoreInteger{Digits: text, Base: 10}
	switch {
	case strings.HasPrefix(text, "0o"):
		c.Digits, c.Base = text[2:], 8
	case strings.HasPrefix(text, "0x"):
		c.Digits, c.Base = text[2:], 16
	case strings.HasPrefix(text, "-"):
		c.Digits, c.Negative = text[1:], true
	case strings.HasPrefix(text, "+"):
		c.Digits = text[1:]
	}
	if c.Digits == "" {
		return CoreInteger{}, false
	}
	for _, b := range []byte(c.Digits) {
		if !isDigit(b, c.Base) {
			return CoreInteger{}, false
		}
	}
	return c, true
}

// Sign returns -1, 0 or +1 as the integer c is negative, zero or positive.
func (c CoreInteger) Sign() int {
	switch {
	case strings.Trim(c.Digits, "0") == "":
		return 0
	case c.Negative:
		return -1
	default:
		return 1
	}
}

// isDigit reports whether b is a digit of base, which is 8, 10 or 16.
func isDigit(b byte, base int) bool {
	switch {
	case b >= '0' && b <= '7':
		return true
	case b == '8' || b == '9':
		return base >= 10
	case (b >= 'a' && b <= 'f') || (b >= 'A' && b <= 'F'):
		return base == 16
	default:
		return false
	}
}
