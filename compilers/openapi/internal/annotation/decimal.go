package annotation

import (
	"cmp"
	"math/big"
	"strings"

	"github.com/dexpace/morphic/ir"
)

// BigValEqual reports whether two numeric literals denote the same value, so
// that one magnitude spelled two ways — 10, 10.0, 1e1 — is one value.
//
// It reads each literal rather than materializing it, which is what makes it
// total: a rational will not build 1e1000001, and comparing text would call
// 1e1000001 and 10e1000000 two values.
//
// Identical text needs no reading. It is also the whole answer for a pair
// outside the decimal grammar, where differing spellings are reported unequal
// rather than guessed at. No BigVal is outside it today;
// TestBigValGrammarStaysWithinTheDecimalReading holds that true.
func BigValEqual(a, b ir.BigVal) bool {
	if a == b {
		return true
	}
	aDec, aOK := parseDecimalBound(a)
	bDec, bOK := parseDecimalBound(b)
	if !aOK || !bOK {
		return false
	}
	return compareDecimalBounds(aDec, bDec) == 0
}

// decimalBound is a numeric literal split into the pieces an exact comparison
// needs: its sign, its significant digits with the point removed and leading
// zeros stripped, and the power of ten the first of those digits carries.
// digits is empty exactly when the value is zero, so "0", "-0.0" and "0e9" are
// one value.
//
// The split keeps the comparison total: 1e1000001 is legal in a spec and kept
// intact by ir.NewBigVal, but math/big will not build it as a rational, and the
// exponent alone separates such bounds.
type decimalBound struct {
	neg    bool
	digits string
	msdExp *big.Int
}

// parseDecimalBound splits the canonical form ir.NewBigVal returns — an
// optional "-", digits, an optional fraction, an optional e/E exponent — into a
// decimalBound.
//
// It reports false for a literal outside that grammar rather than guessing:
// reading the digits out of "1p4" (which is 16) and ordering the rest would
// keep a bound the source never wrote.
//
// No bound a schema produces is outside it today, since they all come through
// ir.NewBigVal, whose grammar is the narrower. It stays fallible because the
// two grammars live in different packages and have already moved apart once.
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
