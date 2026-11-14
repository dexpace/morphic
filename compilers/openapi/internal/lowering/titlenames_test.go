// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package lowering_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
)

// TestTitleNames_TheZeroValueNamesByTitle pins the policy's zero value, which
// an Options left unset hands the lowering: on, so a title names its node
// unless a caller turns the reading off.
func TestTitleNames_TheZeroValueNamesByTitle(t *testing.T) {
	t.Parallel()
	assert.True(t, lowering.TitleNames{}.Honours())
	assert.False(t, lowering.TitleNames{Disabled: true}.Honours())
}
