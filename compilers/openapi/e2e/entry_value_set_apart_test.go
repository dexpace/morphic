// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test

import (
	"regexp"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
)

// disjointNamesAnywhere matches what a disjoint report names, each conjunct by
// where it is written, which the spellings and orders of a case part on, and
// which the cases assert apart (entryWarnings).
var disjointNamesAnywhere = regexp.MustCompile(`share no value \([^)]*\)`)

// entryWarnings is the one warning each position draws whose value set shares
// no member with its target's, that its conjuncts share no value, and none
// for any other case of entryValueSets.
func entryWarnings(vs entryValueSet) []wantFinding {
	if !vs.apart {
		return nil
	}
	out := make([]wantFinding, 0, len(conjunctionPositions))
	for _, at := range conjunctionPositions {
		out = append(out, wantFinding{code: string(diag.DegradedConstruct), pointer: string(at),
			contains: []string{"share no value (", ": 1 value", ": 2 values", "the enum it lowered as"}})
	}
	return out
}
