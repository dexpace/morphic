// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package pass

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/internal/decls"
	"github.com/dexpace/morphic/ir"
)

// TestPrimitiveShapes_EveryPrimitiveKindHasARow holds primitiveShape to the
// kinds ir declares: a kind added there with no case here would be judged by no
// check, and nothing else would say so.
func TestPrimitiveShapes_EveryPrimitiveKindHasARow(t *testing.T) {
	t.Parallel()
	loadedPackage, err := decls.Load("github.com/dexpace/morphic/ir")
	require.NoError(t, err)
	kinds, err := loadedPackage.StringConsts("PrimitiveKind")
	require.NoError(t, err)
	require.NotEmpty(t, kinds)

	for _, declaredConst := range kinds {
		assert.NotEqual(t, shapeNotChecked, primitiveShape(ir.PrimitiveKind(declaredConst.Value)), "%s has no shape", declaredConst.Value)
	}
	assert.Equal(t, shapeNotChecked, primitiveShape("no-such-kind"))
}

// TestKindWords_NamesEveryKind holds the words a value is described in to
// the kinds ir declares, those the checks judge before they are asked
// included.
func TestKindWords_NamesEveryKind(t *testing.T) {
	t.Parallel()
	loadedPackage, err := decls.Load("github.com/dexpace/morphic/ir")
	require.NoError(t, err)
	kinds, err := loadedPackage.StringConsts("ValueKind")
	require.NoError(t, err)
	require.NotEmpty(t, kinds)

	for _, declaredConst := range kinds {
		assert.NotEmpty(t, kindWords(ir.ValueKind(declaredConst.Value)), declaredConst.Value)
	}
	assert.Equal(t, "a null", kindWords(ir.ValueNull))
	assert.Equal(t, "an infinity", kindWords(ir.ValueNegativeInfinity))
}
