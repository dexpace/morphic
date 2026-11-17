// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test

import (
	"fmt"
	"testing"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
)

// TestCompile_ADisjointReportNamesAFraction holds that the report of an allOf
// whose conjuncts share no value names a conjunct admitting only numbers with
// a fraction, "number that is no integer", alone and beside a string, at both
// versions, in both orders of the entries.
func TestCompile_ADisjointReportNamesAFraction(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		fraction, other, fractionWords, otherWords string
	}{
		{"{enum: [1.5]}", "{type: integer}", "number that is no integer", "integer"},
		{"{enum: [1.5, a]}", "{type: boolean}", "string or number that is no integer", "boolean"},
	} {
		for _, version := range bothVersions {
			for _, swapped := range []bool{false, true} {
				entries, at := tc.fraction+", "+tc.other, [2]int{0, 1}
				if swapped {
					entries, at = tc.other+", "+tc.fraction, [2]int{1, 0}
				}
				t.Run(fmt.Sprintf("%s/%s/entries-swapped-%t", tc.fraction, version, swapped), func(t *testing.T) {
					t.Parallel()
					_, diags := compileAnnotationSpec(t, "words", admissionSpec(version, "", "C: {allOf: ["+entries+"]}"))
					requireErrorsAndWarnings(t, diags, []wantFinding{{
						code: diag.DegradedConstruct, pointer: "/components/schemas/C",
						contains: []string{named(at[0], tc.fractionWords), named(at[1], tc.otherWords)},
					}})
				})
			}
		}
	}
}

// named is how a report of two conjuncts names the entry at index, followed by
// what follows it in the list, so a word cannot pass for a longer one.
func named(index int, words string) string {
	return fmt.Sprintf("allOf/%d: %s%s", index, words, [2]string{",", ")"}[index])
}
