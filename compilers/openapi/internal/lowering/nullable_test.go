// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package lowering_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
)

// TestNullableWidening_TheZeroValueHonoursTheKeyword pins the policy's zero
// value, which an Options left unset hands the lowering: on, so a caller who
// says nothing gets the reading documents written to 3.0.0's wording rely on.
func TestNullableWidening_TheZeroValueHonoursTheKeyword(t *testing.T) {
	t.Parallel()
	assert.True(t, lowering.NullableWidening{}.Honours())
	assert.False(t, lowering.NullableWidening{Disabled: true}.Honours())
}
