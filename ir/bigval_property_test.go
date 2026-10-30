// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package ir_test

import (
	"encoding/json/jsontext"
	"testing"

	"github.com/dexpace/morphic/ir"
)

// bigValAdversarialSeeds seeds the fuzzer with the spellings that make the
// grammar's hard cases reachable at all, as naming_property_test.go's
// adversarialRunes does: a fuzzer exploring bytes at random is unlikely to
// construct any of these itself, and #45 lived in two of them that no table
// test included. Each group's reason sits beside it.
var bigValAdversarialSeeds = []string{
	// p/P exponents, what #45 reported: math/big's base-10 parser accepts one
	// regardless of base ("1p4" is 1×2⁴ = 16, not the decimal its digits
	// suggest), so these are the regression this property exists to catch.
	"1p4", "2.5p-2", "5.p3", "1P4", "5P3", "0p0",
	// Leading zeros, the other half of #45, which jsonSpelling drops, kept so
	// the property covers both classes the issue reported.
	"05", "+05", "007", "-012", "00.5", "05e2", "-09", "007e2", "00",
	// JSON-invalid affixes a valid literal can carry (a leading dot, a trailing
	// dot, a leading "+"): the rest of jsonSpelling's job.
	".5", "-.5", "5.", "5.e3", "+5", "0.",
	// Other bases and separators a decimal grammar must not read as its own.
	"0x10", "0b101", "0o17", "1_000", "1,5", "1.2.3",
	// Non-numeric spellings, among them the "Inf"/"NaN" family math/big parses
	// specially: a grammar requiring a digit rejects them for a different
	// reason than #45 but must still reject them.
	"", "abc", "NaN", "Infinity", "Inf", "+Inf", "-Inf", ".inf",
	// Magnitudes at the edge of the exponent range, on either side of it, and
	// one that crosses it only through its digit count, so the bound this corpus
	// reaches is the value's rather than the spelling's.
	"1e400", "1e2000000000", "1e999999999999999999", "1.8e308",
	"1e1000000000000000000", "0.01e1000000000000000000",
	// Malformed exponents and bare punctuation: no digit anywhere, or an
	// "e"/"E" with nothing on one side of it.
	"-", "+", ".", "e5", "1e", "1e+", "--5", " 1", "1 ",
}

// FuzzNewBigVal_AcceptedFormsAreJSONValid pins BigVal's contract, "a BigVal is
// always a JSON-valid numeric literal", for any accepted input rather than for
// the rows TestNewBigVal_CanonicalizesToJSONForm's table lists (#45).
//
// A rejected input carries no claim: NewBigVal need only never accept what
// json.Valid would refuse. The seeds carry most of the weight, since an
// ordinary `go test` runs them without searching and the gate's search is
// bounded to seconds (scripts/fuzz.sh). They are chosen adversarially, not
// drawn from real specs, for the reason bigValAdversarialSeeds gives.
func FuzzNewBigVal_AcceptedFormsAreJSONValid(f *testing.F) {
	for _, seed := range bigValAdversarialSeeds {
		f.Add(seed)
	}
	for _, form := range bigValDecimalForms {
		f.Add(form)
	}

	f.Fuzz(func(t *testing.T, s string) {
		v, err := ir.NewBigVal(s)
		if err != nil {
			return // NewBigVal rejected s; it makes no promise about a rejection.
		}
		if !jsontext.Value(v.String()).IsValid() {
			t.Fatalf("NewBigVal(%q) = %q, which jsontext.Value.IsValid rejects", s, v.String())
		}
	})
}
