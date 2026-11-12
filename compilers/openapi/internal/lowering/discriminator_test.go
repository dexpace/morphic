// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package lowering_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
)

// TestDiscriminatorInference_TheZeroValueHonoursThePolicy pins the policy's
// zero value as on, which is what makes the Options field's default the
// documented one, and Disabled as what turns it off.
func TestDiscriminatorInference_TheZeroValueHonoursThePolicy(t *testing.T) {
	t.Parallel()
	assert.True(t, lowering.DiscriminatorInference{}.Honours())
	assert.False(t, lowering.DiscriminatorInference{Disabled: true}.Honours())
}
