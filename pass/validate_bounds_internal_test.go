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

// TestPrimShapes_EveryPrimKindHasARow holds primShape to the kinds ir declares:
// a kind added there with no case here would be judged by no check, and
// nothing else would say so.
func TestPrimShapes_EveryPrimKindHasARow(t *testing.T) {
	t.Parallel()
	p, err := decls.Load("github.com/dexpace/morphic/ir")
	require.NoError(t, err)
	kinds, err := p.StringConsts("PrimKind")
	require.NoError(t, err)
	require.NotEmpty(t, kinds)

	for _, k := range kinds {
		assert.NotEqual(t, shapeNotChecked, primShape(ir.PrimKind(k.Value)), "%s has no shape", k.Value)
	}
	assert.Equal(t, shapeNotChecked, primShape("no-such-kind"))
}

// TestKindWords_NamesEveryKind holds the words a value is described in to
// the kinds ir declares, those the checks judge before they are asked
// included.
func TestKindWords_NamesEveryKind(t *testing.T) {
	t.Parallel()
	p, err := decls.Load("github.com/dexpace/morphic/ir")
	require.NoError(t, err)
	kinds, err := p.StringConsts("ValueKind")
	require.NoError(t, err)
	require.NotEmpty(t, kinds)

	for _, k := range kinds {
		assert.NotEmpty(t, kindWords(ir.ValueKind(k.Value)), k.Value)
	}
	assert.Equal(t, "a null", kindWords(ir.ValueNull))
	assert.Equal(t, "an infinity", kindWords(ir.ValueNegInf))
}
