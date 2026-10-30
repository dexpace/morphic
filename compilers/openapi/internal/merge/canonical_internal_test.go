// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package merge

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestCanonicalValue_DeclinesANumberOutsideTheGrammar pins that a number with no
// canonical decimal has no spelling at all. Reading "1p4" as the zero Decimal
// would spell it "0" and make it a duplicate of every zero, so the judge
// reports it unspellable instead.
func TestCanonicalValue_DeclinesANumberOutsideTheGrammar(t *testing.T) {
	t.Parallel()
	got, ok := canonicalValue(num("1p4"), 0)

	assert.False(t, ok)
	assert.Empty(t, got)
}

// TestCanonicalValue_SpellsANumberByItsValue holds the spelling to the value:
// 1500 is "1.5e3" however it is written, and every zero is "0".
func TestCanonicalValue_SpellsANumberByItsValue(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ in, want string }{
		{"1500", "1.5e3"},
		{"15e2", "1.5e3"},
		{"-0.0", "0"},
	} {
		got, ok := canonicalValue(num(tc.in), 0)

		assert.True(t, ok, tc.in)
		assert.Equal(t, tc.want, got, tc.in)
	}
}
