// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package lowering_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
)

// TestSecretInference_TheZeroValueHonoursThePolicy pins the policy's zero value
// as on, so that a credential's name asks for its redaction unless a caller
// says otherwise, and Disabled as what turns it off.
func TestSecretInference_TheZeroValueHonoursThePolicy(t *testing.T) {
	t.Parallel()
	assert.True(t, lowering.SecretInference{}.Honours())
	assert.False(t, lowering.SecretInference{Disabled: true}.Honours())
}
