package merge

import (
	"math/big"

	"github.com/dexpace/morphic/ir"
)

// stepFactorsPerDigit bounds the powers of two and of five in a step's
// coefficient: a number of n decimal digits has fewer than 4n of either.
const stepFactorsPerDigit = 4

// isMultipleOf reports whether v is an integral multiple of m, exactly. Read v
// as vc·10^vl and m as mc·10^ml: the coefficients are the digits, and vl and ml
// the exponents of their last digits.
//
// If vl < ml, v is never a multiple, since mc·10^(ml−vl) divides vc only if 10
// divides vc, and it does not. Otherwise v is a multiple iff mc divides
// vc·10^k, with k = min(vl−ml, 4·len(m.Digits())): mc's powers of 2 and 5 are
// each below 4 per digit, and the cofactor coprime to 10 is unaffected by the
// shift. ok is false only for a zero step, which divides nothing.
func isMultipleOf(v, m ir.Decimal) (multiple, ok bool) {
	if m.Sign() == 0 {
		return false, false
	}
	if v.Sign() == 0 {
		return true, true
	}
	shift := lowestExponent(v) - lowestExponent(m)
	if shift < 0 {
		return false, true
	}
	k := min(shift, int64(stepFactorsPerDigit*len(m.Digits())))
	vc, _ := new(big.Int).SetString(v.Digits(), 10) // Digits is a digit run, by Decimal's invariant
	mc, _ := new(big.Int).SetString(m.Digits(), 10) // and so is this one
	vc.Mul(vc, new(big.Int).Exp(big.NewInt(10), big.NewInt(k), nil))
	return new(big.Int).Rem(vc, mc).Sign() == 0, true
}

// lowestExponent returns the power of ten d's last significant digit carries.
// d is not zero, and the result stays inside int64 because Decimal bounds the
// leading digit's exponent by ir.MaxDecimalExp.
func lowestExponent(d ir.Decimal) int64 {
	return d.Exponent() - int64(len(d.Digits())) + 1
}
