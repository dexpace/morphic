// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package ircases_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/internal/ircases"
)

// TestNearMisses_AreOneEditFromTheWords pins what each kind of input adds: an
// empty form has no last byte to drop, only a form holding a separator has one
// to change, and an edit that leaves the form as it was is no miss.
func TestNearMisses_AreOneEditFromTheWords(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		words string
		want  []string
	}{
		{"empty", "", []string{"_", "_", "a", "\xff"}},
		{"one word", "ab", []string{"ab_", "_ab", "aba", "ab\xff", "AB", "a"}},
		{"two words", "a_b", []string{"a_b_", "_a_b", "a_ba", "a_b\xff", "A_B", "a_", "ab", "a__b", "a-b"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.ElementsMatch(t, tc.want, ircases.NearMisses(tc.words))
		})
	}
}

// TestCanonical_IsFreshOnEachCall pins that a caller's edit to its rows never
// reaches the next caller, which a shared slice would let through.
func TestCanonical_IsFreshOnEachCall(t *testing.T) {
	t.Parallel()
	rows := ircases.Canonical()
	rows[0].Want = "edited"
	assert.NotEqual(t, "edited", ircases.Canonical()[0].Want)
}

// TestHasCanonical_IsFreshOnEachCall pins the same of the comparison rows.
func TestHasCanonical_IsFreshOnEachCall(t *testing.T) {
	t.Parallel()
	rows := ircases.HasCanonical()
	rows[0].Words = "edited"
	assert.NotEqual(t, "edited", ircases.HasCanonical()[0].Words)
}

// TestDecimalForms_IsFreshOnEachCall pins the same of the canonical forms.
func TestDecimalForms_IsFreshOnEachCall(t *testing.T) {
	t.Parallel()
	forms := ircases.DecimalForms()
	forms[0] = "edited"
	assert.NotEqual(t, "edited", ircases.DecimalForms()[0])
}

// TestDecimalSpellings_IsFreshOnEachCall pins the same of the spellings.
func TestDecimalSpellings_IsFreshOnEachCall(t *testing.T) {
	t.Parallel()
	spellings := ircases.DecimalSpellings()
	spellings[0] = "edited"
	assert.NotEqual(t, "edited", ircases.DecimalSpellings()[0])
}
