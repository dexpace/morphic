package ir

import (
	"fmt"
	"strings"
)

// BigVal is an arbitrary-precision numeric value carried as its decimal string
// form. The IR never stores float64 (the TypeSpec Numeric lesson); [Decimal] is
// the one reading of it.
//
// A BigVal is always a JSON-valid numeric literal: NewBigVal canonicalizes
// non-JSON but numerically valid spellings (a leading dot as in ".5", a
// trailing dot as in "5.", a leading "+", a redundant leading zero as in "09")
// into JSON form, leaving every significant digit, the exponent, and its case
// untouched — so a stored value round-trips through JSON unchanged.
type BigVal string

// NewBigVal validates s as a decimal or scientific-notation numeric literal and
// returns it in canonical JSON form. It rejects the empty string, hex, a
// binary (p/P) exponent, NaN and infinities with [ErrNotDecimal], and a nonzero
// value whose leading digit carries a power of ten past [MaxDecimalExp] with
// [ErrExponentRange]. It never rounds or reformats the significant digits: a
// high-precision decimal and an exponential literal are preserved verbatim,
// and only JSON-invalid affixes are normalized.
func NewBigVal(s string) (BigVal, error) {
	l, _, err := readDecimal(s)
	if err != nil {
		return "", fmt.Errorf("bigval: %q: %w", s, err)
	}
	return BigVal(jsonSpelling(l)), nil
}

// jsonSpelling rewrites the JSON-invalid affixes of a scanned literal into JSON
// form without altering its value: it drops a leading "+", gives the integer
// part its one leading digit (".5" → "0.5", "09" → "9"), and drops a trailing
// dot ("5." → "5", "5.e3" → "5e3"). Significant digits, the exponent and its
// case are left as written. A leading zero is JSON-invalid rather than merely
// unusual, so keeping it would break BigVal's round-trip guarantee.
func jsonSpelling(l decimalLiteral) string {
	var b strings.Builder
	if l.neg {
		b.WriteByte('-')
	}
	if whole := strings.TrimLeft(l.intPart, "0"); whole != "" {
		b.WriteString(whole)
	} else {
		b.WriteByte('0')
	}
	if l.frac != "" {
		b.WriteByte('.')
		b.WriteString(l.frac)
	}
	b.WriteString(l.exponent)
	return b.String()
}

// Decimal reads v as a [Decimal]. A BigVal built by [NewBigVal] always reads;
// one that was not, such as a value a decoder produced, reads or is refused as
// [ParseDecimal] says.
func (v BigVal) Decimal() (Decimal, error) { return ParseDecimal(string(v)) }

// String returns the literal decimal form.
func (v BigVal) String() string { return string(v) }
