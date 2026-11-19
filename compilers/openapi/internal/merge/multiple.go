// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package merge

import (
	"math/big"
	"strconv"

	"github.com/dexpace/morphic/ir"
)

// stepFactorsPerDigit bounds the powers of two and of five in a step's
// coefficient: a number of n decimal digits has fewer than 4n of either.
const stepFactorsPerDigit = 4

// isMultipleOf reports whether decimal is an integral multiple of irDecimal,
// exactly. Read decimal as vc·10^vl and irDecimal as mc·10^ml: the coefficients
// are the digits, and vl and ml the exponents of their last digits.
//
// If vl < ml, decimal is never a multiple, since mc·10^(ml−vl) divides vc only
// if 10 divides vc, and it does not. Otherwise decimal is a multiple iff mc
// divides vc·10^k, with k = min(vl−ml, 4·len(irDecimal.Digits())): mc's powers
// of 2 and 5 are each below 4 per digit, and the cofactor coprime to 10 is
// unaffected by the shift. ok is false only for a zero step, which divides
// nothing.
func isMultipleOf(decimal, irDecimal ir.Decimal) (multiple, ok bool) {
	if irDecimal.Sign() == 0 {
		return false, false
	}
	if decimal.Sign() == 0 {
		return true, true
	}
	shift := lowestExponent(decimal) - lowestExponent(irDecimal)
	if shift < 0 {
		return false, true
	}
	k := min(shift, int64(stepFactorsPerDigit*len(irDecimal.Digits())))
	return new(big.Int).Rem(scaled(decimal, k), coefficient(irDecimal)).Sign() == 0, true
}

// maxStepDigits bounds the significant digits of the two steps commonStep folds
// (styleguide bounded-everything rule): the cost of their common multiple grows
// with the square of them, and a step of this many digits is no step a document
// means. Past it the two are reported and kept, as a keyword with no order is.
const maxStepDigits = 1024

// commonStep returns the step whose multiples are exactly the multiples of both
// bigVal and irBigVal: one literal written twice, whichever of them is a
// multiple of the other, or else their least common multiple, a number neither
// wrote. It returns false where no such step can be read: a literal no Decimal
// holds, a step that is not positive, a pair past maxStepDigits, or a multiple
// past what a Decimal holds.
func commonStep(bigVal, irBigVal ir.BigVal) (ir.BigVal, bool) {
	if bigVal == irBigVal {
		return bigVal, true
	}
	a, errA := bigVal.Decimal()
	b, errB := irBigVal.Decimal()
	if errA != nil || errB != nil || a.Sign() <= 0 || b.Sign() <= 0 || len(a.Digits())+len(b.Digits()) > maxStepDigits {
		return "", false
	}
	if multiple, ok := isMultipleOf(a, b); ok && multiple {
		return bigVal, true
	}
	if multiple, ok := isMultipleOf(b, a); ok && multiple {
		return irBigVal, true
	}
	lcm, err := ir.NewBigVal(leastCommonMultiple(a, b))
	return lcm, err == nil
}

// leastCommonMultiple spells the least common multiple of two positive numbers.
// Read each as a coefficient and the exponent of its last digit, and count both
// in units of the lower exponent: a's coefficient grows by 10^shift. Past the
// powers of 2 and 5 in b's coefficient, fewer than 4 per digit, a further power
// of ten only scales the result, so k of them are multiplied out and the other
// shift-k stay in the exponent, which keeps a wide shift from being built.
func leastCommonMultiple(a, b ir.Decimal) string {
	if lowestExponent(a) < lowestExponent(b) {
		a, b = b, a
	}
	shift := lowestExponent(a) - lowestExponent(b)
	k := min(shift, int64(stepFactorsPerDigit*len(b.Digits())))
	x, y := scaled(a, k), coefficient(b)
	gcd := new(big.Int).GCD(nil, nil, x, y)
	lcm := x.Mul(x.Quo(x, gcd), y).String()
	if exp := lowestExponent(a) - k; exp != 0 {
		lcm += "e" + strconv.FormatInt(exp, 10)
	}
	return lcm
}

// scaled returns decimal's coefficient times 10^k.
func scaled(decimal ir.Decimal, k int64) *big.Int {
	c := coefficient(decimal)
	return c.Mul(c, new(big.Int).Exp(big.NewInt(10), big.NewInt(k), nil))
}

// coefficient returns decimal's significant digits as an integer.
func coefficient(decimal ir.Decimal) *big.Int {
	c, _ := new(big.Int).SetString(decimal.Digits(), 10) // Digits is a digit run, by Decimal's invariant
	return c
}

// lowestExponent returns the power of ten decimal's last significant digit
// carries. decimal is not zero, and the result stays inside int64 because
// Decimal bounds the leading digit's exponent by ir.MaxDecimalExp.
func lowestExponent(decimal ir.Decimal) int64 {
	return decimal.Exponent() - int64(len(decimal.Digits())) + 1
}
