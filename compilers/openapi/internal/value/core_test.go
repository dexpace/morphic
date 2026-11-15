// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package value_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/compilers/openapi/internal/value"
	"github.com/dexpace/morphic/ir"
)

// TestNumericLiteral_AnIntegerNoCoreFormSpellsKeepsTheParsersDecode holds an
// !!int the document tags itself, in a spelling the core schema does not
// know, to the parser's decode, past int64 included.
func TestNumericLiteral_AnIntegerNoCoreFormSpellsKeepsTheParsersDecode(t *testing.T) {
	t.Parallel()
	got, err := value.NumericLiteral(openapitest.ScalarNode("!!int", "18_446_744_073_709_551_615"))
	require.NoError(t, err)
	assert.Equal(t, ir.BigVal("18446744073709551615"), got)
}

// digitsOf is n digits, none a leading zero.
func digitsOf(n int) string { return "1" + strings.Repeat("7", n-1) }

// TestNumericLiteral_ABaseTenIntegerCostsNoMoreForMoreDigits counts the
// allocations of reading a base-ten integer at two lengths a hundred times
// apart: they are equal, as copying the digits allocates alike, where
// rendering through math/big allocates more the longer the number.
func TestNumericLiteral_ABaseTenIntegerCostsNoMoreForMoreDigits(t *testing.T) {
	// AllocsPerRun counts the whole process, so the test runs alone.
	allocs := func(n int) float64 {
		node := openapitest.ScalarNode("!!int", "-"+digitsOf(n))
		return testing.AllocsPerRun(3, func() {
			_, err := value.NumericLiteral(node)
			require.NoError(t, err)
		})
	}

	assert.Equal(t, allocs(1_000), allocs(100_000))
}

// TestNumericLiteral_ABasedIntegerIsReadUpToItsBound holds the digits of an
// 0o or 0x integer read to the bound: 256 hexadecimal digits read, and one
// more refuses the literal rather than paying for its base change.
func TestNumericLiteral_ABasedIntegerIsReadUpToItsBound(t *testing.T) {
	t.Parallel()
	_, err := value.NumericLiteral(openapitest.ScalarNode("!!int", "0x"+strings.Repeat("f", 256)))
	require.NoError(t, err)
	_, err = value.NumericLiteral(openapitest.ScalarNode("!!int", "0o"+strings.Repeat("7", 257)))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "a base-8 integer of 257 digits is past the 256 digits read")
}
