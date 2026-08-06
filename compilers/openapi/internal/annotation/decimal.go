package annotation

import (
	"cmp"
	"math/big"
	"strings"

	"github.com/dexpace/morphic/ir"
)

// decimalBound is a numeric literal split into the three pieces an exact
// comparison needs: its sign, its significant digits with the point removed and
// the leading zeros stripped, and the power of ten the first of those digits
// carries. digits is empty exactly when the value is zero, which is what makes
// "0", "-0.0" and "0e9" one value here rather than several.
//
// The split is what keeps the comparison total. Reading a bound as a number
// instead means materializing it, and 1e1000001 — legal in a spec, and kept
// intact by ir.NewBigVal — is a magnitude math/big will not build as a rational
// at all. Nothing about ordering two bounds needs those digits anyway: the
// exponent alone separates them.
type decimalBound struct {
	neg    bool
	digits string
	msdExp *big.Int
}

// parseDecimalBound splits the canonical form ir.NewBigVal returns — an
// optional "-", digits, an optional fraction, an optional e/E exponent — into a
// decimalBound.
//
// It reports false for a literal outside that grammar. Every bound the readers
// here produce comes through ir.NewBigVal, which still stores a binary exponent
// verbatim: "1p4" is kept as written and means 16 (GitHub #45). Reading the
// digits out of one and ordering what is left would put a bound the source never
// wrote into the IR, so it declines rather than guessing.
func parseDecimalBound(v ir.BigVal) (decimalBound, bool) {
	unsigned, neg := strings.CutPrefix(v.String(), "-")

	mantissa, expText := unsigned, "0"
	if i := strings.IndexAny(unsigned, "eE"); i >= 0 {
		mantissa, expText = unsigned[:i], unsigned[i+1:]
	}
	intPart, frac := mantissa, ""
	if i := strings.IndexByte(mantissa, '.'); i >= 0 {
		intPart, frac = mantissa[:i], mantissa[i+1:]
	}

	digits := intPart + frac
	if !isDigits(digits) {
		return decimalBound{}, false
	}
	// A big.Int rather than an int64: the exponent arrives as text the source
	// wrote, and a fixed width that overflowed on it would order the two bounds
	// by a number neither of them has.
	exp, ok := new(big.Int).SetString(expText, 10)
	if !ok {
		return decimalBound{}, false
	}

	// digits reads as an integer scaled by 10**-len(frac), and stripping its
	// leading zeros leaves that integer alone — so what is left is significant
	// digits whose first one carries 10**(exp-len(frac)+len(significant)-1).
	significant := strings.TrimLeft(digits, "0")
	if significant == "" {
		return decimalBound{neg: neg, msdExp: new(big.Int)}, true
	}
	return decimalBound{
		neg:    neg,
		digits: significant,
		msdExp: exp.Add(exp, big.NewInt(int64(len(significant)-len(frac)-1))),
	}, true
}

// sign reports the bound's sign as -1, 0 or +1. Having no digits is checked
// first because a literal can carry a minus and still be zero: "-0.0" is the
// same bound as "0", and reading its sign off the minus would order it below.
func (d decimalBound) sign() int {
	switch {
	case d.digits == "":
		return 0
	case d.neg:
		return -1
	default:
		return 1
	}
}

// compareDecimalBounds returns -1, 0 or +1 as a is less than, equal to, or
// greater than b. The comparison is exact for every literal parseDecimalBound
// accepts, however far apart the two magnitudes are: sign first, then the power
// of ten the leading digit carries, and only then the digits themselves.
func compareDecimalBounds(a, b decimalBound) int {
	if order := cmp.Compare(a.sign(), b.sign()); order != 0 {
		return order
	}
	if a.sign() == 0 {
		return 0
	}

	order := a.msdExp.Cmp(b.msdExp)
	if order == 0 {
		order = compareDigits(a.digits, b.digits)
	}
	if a.neg {
		return -order
	}
	return order
}

// compareDigits orders two digit runs whose leading digits carry the same power
// of ten, reading a run that has ended as the trailing zeros it stands for —
// which is what puts 5e1 and 50 at one value rather than two.
func compareDigits(a, b string) int {
	for i := range max(len(a), len(b)) {
		if order := cmp.Compare(digitAt(a, i), digitAt(b, i)); order != 0 {
			return order
		}
	}
	return 0
}

// digitAt returns s[i], or '0' past the end of s.
func digitAt(s string, i int) byte {
	if i < len(s) {
		return s[i]
	}
	return '0'
}

// isDigits reports whether s is a non-empty run of decimal digits. The empty
// run is not one: a literal with no digits at all denotes no value, and reading
// it as zero would order it against real bounds.
func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := range len(s) {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}
