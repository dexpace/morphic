package ir

import (
	"fmt"
	"math/big"
	"strings"
)

// BigVal is an arbitrary-precision numeric value carried as its decimal string
// form. The IR never stores float64 (the TypeSpec Numeric lesson); helpers may
// convert through math/big at the boundary.
//
// A BigVal is always a JSON-valid numeric literal: NewBigVal canonicalizes
// non-JSON but numerically valid spellings (a leading dot as in ".5", a
// trailing dot as in "5.", a leading "+", a redundant leading zero as in "09")
// into JSON form, leaving every significant digit, the exponent, and its case
// untouched — so a stored value round-trips through JSON unchanged.
type BigVal string

// NewBigVal validates s as a decimal or scientific-notation numeric literal and
// returns it in canonical JSON form. It rejects the empty string, hex, a
// binary (p/P) exponent, NaN, and infinities. It never rounds or reformats the
// significant digits: an out-of-float64-range magnitude, a high-precision
// decimal, and an exponential literal are all preserved verbatim (only
// JSON-invalid affixes are normalized).
func NewBigVal(s string) (BigVal, error) {
	if s == "" {
		return "", fmt.Errorf("bigval: empty numeric literal")
	}
	if !isDecimalLiteral(s) {
		return "", fmt.Errorf("bigval: not a decimal numeric literal %q", s)
	}
	// isDecimalLiteral has already restricted s to the decimal/e-exponent
	// grammar the doc comment above promises, so big.ParseFloat's own base-10
	// grammar — wider than that, and in particular willing to read a p/P
	// binary exponent regardless of base — is only ever exercised on a string
	// of that narrower shape. What ParseFloat still decides is magnitude: a
	// syntactically fine decimal can still overflow to infinity through its
	// exponent (an "1e2000000000" parses but reports IsInf), which is what the
	// check below catches; the textual "NaN"/"Inf" spellings never reach here
	// at all, since isDecimalLiteral requires at least one digit.
	f, _, err := big.ParseFloat(s, 10, 0, big.ToNearestEven)
	if err != nil {
		return "", fmt.Errorf("bigval: parse %q: %w", s, err)
	}
	if f.IsInf() {
		return "", fmt.Errorf("bigval: non-finite numeric literal %q", s)
	}
	return BigVal(canonicalDecimal(s)), nil
}

// isDecimalLiteral reports whether s is exactly an optionally-signed decimal
// mantissa with an optional e/E exponent — sign? mantissa (e sign? digits)?,
// where mantissa is digits, digits "." digits*, or "." digits — and nothing
// else. This is the grammar BigVal documents ("decimal or scientific-notation
// numeric literal"); math/big's own base-10 parser accepts a strictly wider
// one — hex is already excluded by forcing base 10, but a p/P binary exponent
// is not (1p4 means 1×2⁴ = 16 in that grammar, not the unrelated decimal its
// digits and dot suggest) — so this runs as an explicit pre-parse gate rather
// than trusting ParseFloat's grammar at the call site. It delegates to
// scanMantissa and scanExponent, one cursor pass each, so that no single
// function has to hold the whole grammar's branching at once.
func isDecimalLiteral(s string) bool {
	i, n := 0, len(s)
	if i < n && (s[i] == '+' || s[i] == '-') {
		i++
	}

	afterMantissa, hasMantissa := scanMantissa(s, i)
	if !hasMantissa {
		return false // no digit anywhere: "", "+", ".", "abc", "Inf", ...
	}

	afterExponent, exponentOK := scanExponent(s, afterMantissa)
	if !exponentOK {
		return false // "e"/"E" with no exponent digits
	}
	return afterExponent == n // unconsumed suffix rejects it: "0x10", "1p4", ...
}

// scanMantissa consumes an unsigned decimal mantissa — digits, digits "."
// digits*, or "." digits — starting at s[i], where i is assumed already past
// any leading sign. It reports the index just past what it consumed and
// whether that was a mantissa at all: a bare "." with no digit on either side
// consumes input (the dot) but is not one.
func scanMantissa(s string, i int) (end int, ok bool) {
	n := len(s)

	intStart := i
	for i < n && isASCIIDigit(s[i]) {
		i++
	}
	hasInt := i > intStart

	hasFrac := false
	if i < n && s[i] == '.' {
		i++
		fracStart := i
		for i < n && isASCIIDigit(s[i]) {
			i++
		}
		hasFrac = i > fracStart
	}

	return i, hasInt || hasFrac
}

// scanExponent consumes an optional e/E exponent — (e|E) sign? digits* —
// starting at s[i]. Absent entirely, it reports i unchanged and ok true, a
// mantissa with no exponent being perfectly valid; present with no digit
// after the e/E (and optional sign), it reports ok false.
func scanExponent(s string, i int) (end int, ok bool) {
	n := len(s)
	if i >= n || (s[i] != 'e' && s[i] != 'E') {
		return i, true
	}
	i++
	if i < n && (s[i] == '+' || s[i] == '-') {
		i++
	}
	expStart := i
	for i < n && isASCIIDigit(s[i]) {
		i++
	}
	return i, i > expStart
}

// isASCIIDigit reports whether b is a decimal digit. BigVal's grammar is ASCII
// only, so a byte scan is exact here and needs no rune decoding.
func isASCIIDigit(b byte) bool { return b >= '0' && b <= '9' }

// canonicalDecimal rewrites the JSON-invalid affixes of an already-validated
// numeric literal into JSON form without altering its value: it drops a leading
// "+", normalizes the integer part's leading zeros (".5" → "0.5", "09" → "9"),
// and drops a trailing dot (\"5.\" → \"5\", "5.e3" → "5e3"). Significant digits,
// the exponent, and its case are left exactly as written. The transform is
// idempotent, so a value already in canonical form is returned unchanged.
func canonicalDecimal(s string) string {
	mantissa, exponent := s, ""
	if i := strings.IndexAny(s, "eE"); i >= 0 {
		mantissa, exponent = s[:i], s[i:]
	}

	sign := ""
	if len(mantissa) > 0 && (mantissa[0] == '+' || mantissa[0] == '-') {
		if mantissa[0] == '-' {
			sign = "-"
		}
		mantissa = mantissa[1:]
	}

	mantissa = strings.TrimSuffix(canonicalIntegerPart(mantissa), ".")

	return sign + mantissa + exponent
}

// canonicalIntegerPart gives an unsigned mantissa the single leading digit JSON
// requires: it drops zeros that carry no value ("09" → "9", "00.5" → "0.5") and
// supplies the "0" a leading-dot or all-zero mantissa lacks (".5" → "0.5",
// "00" → "0"). A leading zero is JSON-invalid rather than merely unusual, so
// leaving it would break BigVal's round-trip guarantee.
func canonicalIntegerPart(mantissa string) string {
	digits := strings.TrimLeft(mantissa, "0")
	if digits == "" || digits[0] == '.' {
		return "0" + digits
	}
	return digits
}

// String returns the literal decimal form.
func (v BigVal) String() string { return string(v) }
