// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package replacement_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/compilers/openapi/internal/replacement"
)

// TestInference_TheZeroValueHonoursThePolicy pins the policy's zero value as
// on, so that a deprecation's message fills its ReplacedBy unless a caller
// says otherwise, and Disabled as what turns it off.
func TestInference_TheZeroValueHonoursThePolicy(t *testing.T) {
	t.Parallel()
	assert.True(t, replacement.Inference{}.Honours())
	assert.False(t, replacement.Inference{Disabled: true}.Honours())
}
