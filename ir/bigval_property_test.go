package ir_test

import (
	"encoding/json"
	"testing"

	"github.com/dexpace/morphic/ir"
)

// bigValAdversarialSeeds seeds the fuzzer with the spellings that make the
// grammar's hard cases reachable at all, following the same rationale as
// naming_property_test.go's adversarialRunes: a fuzzer exploring bytes at
// random is unlikely to construct any one of these on its own, and GitHub #45
// lived in exactly two of them (a binary p/P exponent, a JSON-invalid leading
// zero) that no table test happened to include.
//
// Each group names why it is here:
//   - p/P exponents: what #45 reported. math/big's own base-10 parser accepts
//     one regardless of base ("1p4" is 1×2⁴ = 16, not the decimal its digits
//     suggest), so these are the regression this property exists to catch.
//   - leading zeros: the other half of #45, already fixed by
//     canonicalIntegerPart — kept here so the property covers both classes
//     the issue reported, not just the one still open when it was filed.
//   - JSON-invalid affixes a valid literal can carry: a leading dot, a
//     trailing dot, a leading "+" — canonicalDecimal's whole job.
//   - other bases and separators a decimal grammar must not read as its own:
//     hex, binary, octal, digit-group underscores.
//   - the non-numeric spellings math/big parses specially: the "Inf"/"NaN"
//     family, which a grammar requiring a digit rejects for a different
//     reason than #45 but must still reject.
//   - magnitudes at the edge of what a *Float can represent, so the
//     downstream IsInf check stays exercised by this corpus too.
//   - malformed exponents and bare punctuation: no digit anywhere, or an
//     "e"/"E" with nothing on one side of it.
var bigValAdversarialSeeds = []string{
	"1p4", "2.5p-2", "5.p3", "1P4", "5P3", "0p0",
	"05", "+05", "007", "-012", "00.5", "05e2", "-09", "007e2", "00",
	".5", "-.5", "5.", "5.e3", "+5", "0.",
	"0x10", "0b101", "0o17", "1_000", "1,5", "1.2.3",
	"", "abc", "NaN", "Infinity", "Inf", "+Inf", "-Inf", ".inf",
	"1e400", "1e2000000000", "1e999999999999999999", "1.8e308",
	"-", "+", ".", "e5", "1e", "1e+", "--5", " 1", "1 ",
}

// FuzzNewBigVal_AcceptedFormsAreJSONValid pins BigVal's documented contract —
// "a BigVal is always a JSON-valid numeric literal" — as a claim about any
// accepted input rather than about the rows
// TestNewBigVal_CanonicalizesToJSONForm's table thought to list.
//
// The distinction is the point, same as naming's fuzz target: a table pins
// what the answer for one input should be, which is the right shape for that
// test's cases; this instead pins what has to hold of *any* accepted input,
// which is the shape GitHub #45 asked for directly ("A property test
// asserting json.Valid([]byte(v.String())) over accepted inputs would pin
// the whole contract"). NewBigVal("1p4") satisfied every row of the old
// table simply by not being one, while still failing this property — the
// same structural gap through which the issue's other half (a leading zero
// like "05") would have gone unnoticed too, had it not already been fixed by
// the time this property was written.
//
// What the gate runs is the seed corpus, though: `go test` executes a fuzz
// target's seeds and does not search. So the standing coverage is exactly the
// spellings below plus the two tables', and a class absent from all three is
// unprotected until someone runs `-fuzz` — which is why the seeds are chosen
// adversarially rather than drawn from real specs, the same reasoning
// naming_property_test.go records.
//
// A rejected input carries no claim: NewBigVal is not required to accept
// everything, only to never accept something json.Valid would refuse. What
// each refusal reason is remains the tables' job, in bigval_test.go.
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
		if !json.Valid([]byte(v.String())) {
			t.Fatalf("NewBigVal(%q) = %q, which json.Valid rejects", s, v.String())
		}
	})
}
