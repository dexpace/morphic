// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package value_test

import (
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
