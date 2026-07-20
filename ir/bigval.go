package ir

import (
	"fmt"
	"math/big"
)

// BigVal is an arbitrary-precision numeric value carried as its decimal string
// form. The IR never stores float64 (the TypeSpec Numeric lesson); helpers may
// convert through math/big at the boundary.
type BigVal string

// NewBigVal validates s as a decimal or scientific-notation numeric literal and
// returns it as a BigVal. It rejects the empty string, hex, NaN, and infinities.
func NewBigVal(s string) (BigVal, error) {
	if s == "" {
		return "", fmt.Errorf("bigval: empty numeric literal")
	}
	// big.Float's Parse with base 10 accepts decimal and e/E exponent forms
	// only; it rejects hex (base 10 is forced), NaN, Infinity, and separators.
	if _, _, err := big.ParseFloat(s, 10, 0, big.ToNearestEven); err != nil {
		return "", fmt.Errorf("bigval: parse %q: %w", s, err)
	}
	return BigVal(s), nil
}

// String returns the literal decimal form.
func (v BigVal) String() string { return string(v) }
