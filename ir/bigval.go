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
// source spellings that are numerically valid but not valid JSON (a leading dot
// as in ".5", a trailing dot as in "5.", a leading "+") into their JSON form
// without touching a single significant digit, so every stored value round-trips
// through JSON unchanged. Digits, exponent form, and case are preserved exactly;
// only the non-JSON affixes are rewritten.
type BigVal string

// NewBigVal validates s as a decimal or scientific-notation numeric literal and
// returns it in canonical JSON form. It rejects the empty string, hex, NaN, and
// infinities. It never rounds or reformats the significant digits: an
// out-of-float64-range magnitude, a high-precision decimal, and an exponential
// literal are all preserved verbatim (only JSON-invalid affixes are normalized).
func NewBigVal(s string) (BigVal, error) {
	if s == "" {
		return "", fmt.Errorf("bigval: empty numeric literal")
	}
	// big.ParseFloat with base 10 accepts decimal and e/E exponent forms only;
	// it rejects hex (base 10 is forced), NaN, and digit separators. It does
	// accept the "Inf" spellings, so reject those explicitly below.
	f, _, err := big.ParseFloat(s, 10, 0, big.ToNearestEven)
	if err != nil {
		return "", fmt.Errorf("bigval: parse %q: %w", s, err)
	}
	if f.IsInf() {
		return "", fmt.Errorf("bigval: non-finite numeric literal %q", s)
	}
	return BigVal(canonicalDecimal(s)), nil
}

// canonicalDecimal rewrites the JSON-invalid affixes of an already-validated
// numeric literal into JSON form without altering its value: it drops a leading
// "+", inserts the "0" of a leading-dot mantissa (".5" → "0.5"), and drops a
// trailing dot (\"5.\" → \"5\", "5.e3" → "5e3"). Significant digits, the
// exponent, and its case are left exactly as written. The transform is
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

	if strings.HasPrefix(mantissa, ".") {
		mantissa = "0" + mantissa
	}
	mantissa = strings.TrimSuffix(mantissa, ".")

	return sign + mantissa + exponent
}

// String returns the literal decimal form.
func (v BigVal) String() string { return string(v) }
