// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package ir_test

import (
	"cmp"
	"errors"
	"math/big"
	"testing"

	"github.com/dexpace/morphic/ir"
)

// maxRationalExp is the largest power of ten the rational oracle is asked to
// build. math/big refuses 1e1000001 outright and takes real time on a
// millionth of that, so past this the sign and exponent oracle answers alone.
const maxRationalExp = 4096

// decimalSeeds returns every spelling the decimal fuzz targets start from: the
// spellings of one value, the adversarial grammar cases and the canonical forms
// NewBigVal stores unchanged. Taking them from the slices their own tests keep
// means a spelling added there reaches here.
func decimalSeeds() []string {
	seeds := append([]string{}, decimalSpellings...)
	seeds = append(seeds, bigValAdversarialSeeds...)
	return append(seeds, bigValDecimalForms...)
}

// FuzzParseDecimal_ReadsWhatNewBigValKeeps searches for a literal the two
// readers split on: NewBigVal and ParseDecimal must refuse the same inputs for
// the same reason, and what NewBigVal stores must read to the value its input
// read to. A BigVal is a JSON spelling of the value, so storing it a second
// time changes nothing.
func FuzzParseDecimal_ReadsWhatNewBigValKeeps(f *testing.F) {
	for _, seed := range decimalSeeds() {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, s string) {
		v, newErr := ir.NewBigVal(s)
		d, parseErr := ir.ParseDecimal(s)
		if (newErr == nil) != (parseErr == nil) {
			t.Fatalf("NewBigVal(%q) error %v, ParseDecimal error %v", s, newErr, parseErr)
		}
		if newErr != nil {
			for _, sentinel := range []error{ir.ErrNotDecimal, ir.ErrExponentRange} {
				if errors.Is(newErr, sentinel) != errors.Is(parseErr, sentinel) {
					t.Fatalf("%q: NewBigVal says %v, ParseDecimal says %v", s, newErr, parseErr)
				}
			}
			return
		}
		kept, err := ir.ParseDecimal(string(v))
		if err != nil || kept != d {
			t.Fatalf("NewBigVal(%q) = %q reads to %v (%v), but the input reads to %v", s, v, kept, err, d)
		}
		again, err := ir.NewBigVal(string(v))
		if err != nil || again != v {
			t.Fatalf("NewBigVal(%q) = %q, and storing it again gives %q (%v)", s, v, again, err)
		}
	})
}

// FuzzDecimal_OrdersAsRationalsDo holds Decimal's order to two oracles. Where
// math/big builds both values as rationals, Cmp, Sign and IsInt must agree with
// the rationals'. Everywhere, including past what a rational builds, the order
// must follow sign, then exponent, then the digits read as a fraction. Between
// them every value is checked against something that does not share its code.
func FuzzDecimal_OrdersAsRationalsDo(f *testing.F) {
	seeds := decimalSeeds()
	for i, a := range seeds {
		f.Add(a, seeds[(i+1)%len(seeds)])
	}

	f.Fuzz(func(t *testing.T, a, b string) {
		da, errA := ir.ParseDecimal(a)
		db, errB := ir.ParseDecimal(b)
		if errA != nil || errB != nil {
			t.Skip("not both decimal literals in range")
		}

		order := da.Cmp(db)
		if order != -db.Cmp(da) {
			t.Fatalf("%q against %q: %d, and the other way round %d", a, b, order, db.Cmp(da))
		}
		byCmp, byEq, byText := order == 0, da == db, da.String() == db.String()
		if byCmp != byEq || byCmp != byText {
			t.Fatalf("%q and %q: Cmp %t, == %t, String %t", a, b, byCmp, byEq, byText)
		}
		if want := exponentOracle(t, da, db); order != want {
			t.Fatalf("%q against %q: Cmp %d, sign/exponent oracle %d", a, b, order, want)
		}
		checkAgainstRationals(t, a, b, da, db)
	})
}

// exponentOracle orders two decimals by sign, then exponent, then the digits as
// the fraction 0.digits, without Decimal.Cmp.
func exponentOracle(t *testing.T, a, b ir.Decimal) int {
	t.Helper()
	if a.Sign() != b.Sign() {
		return cmp.Compare(a.Sign(), b.Sign())
	}
	if a.Sign() == 0 {
		return 0
	}
	if a.Exponent() != b.Exponent() {
		return a.Sign() * cmp.Compare(a.Exponent(), b.Exponent())
	}
	ra, okA := new(big.Rat).SetString("0." + a.Digits())
	rb, okB := new(big.Rat).SetString("0." + b.Digits())
	if !okA || !okB {
		t.Fatalf("digits %q and %q do not read as fractions", a.Digits(), b.Digits())
	}
	return a.Sign() * ra.Cmp(rb)
}

// checkAgainstRationals compares da and db with the rationals the two spellings
// build, when math/big builds both at a size worth the time.
func checkAgainstRationals(t *testing.T, a, b string, da, db ir.Decimal) {
	t.Helper()
	ra, okA := rationalOf(a, da)
	rb, okB := rationalOf(b, db)
	if !okA || !okB {
		return
	}
	if got, want := da.Cmp(db), ra.Cmp(rb); got != want {
		t.Fatalf("%q against %q: Cmp %d, rationals %d", a, b, got, want)
	}
	for _, side := range []struct {
		in string
		d  ir.Decimal
		r  *big.Rat
	}{{a, da, ra}, {b, db, rb}} {
		if side.d.Sign() != side.r.Sign() || side.d.IsInt() != side.r.IsInt() {
			t.Fatalf("%q: Sign %d IsInt %t, rational Sign %d IsInt %t",
				side.in, side.d.Sign(), side.d.IsInt(), side.r.Sign(), side.r.IsInt())
		}
	}
}

// rationalOf builds the rational NewBigVal's spelling of x denotes, and reports
// false when the value is past maxRationalExp powers of ten from one.
func rationalOf(x string, d ir.Decimal) (*big.Rat, bool) {
	if d.Exponent() > maxRationalExp || d.Exponent() < -maxRationalExp {
		return nil, false
	}
	v, err := ir.NewBigVal(x)
	if err != nil {
		return nil, false
	}
	return new(big.Rat).SetString(string(v))
}
