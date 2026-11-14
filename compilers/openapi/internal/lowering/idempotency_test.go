// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package lowering_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
)

// TestIdempotencyInference_TheZeroValueHonoursThePolicy pins the policy's zero
// value as on, so that an operation's Idempotency-Key header names its token
// unless a caller says otherwise, and Disabled as what turns it off.
func TestIdempotencyInference_TheZeroValueHonoursThePolicy(t *testing.T) {
	t.Parallel()
	assert.True(t, lowering.IdempotencyInference{}.Honours())
	assert.False(t, lowering.IdempotencyInference{Disabled: true}.Honours())
}
