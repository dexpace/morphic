// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package convention_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/compilers/openapi/internal/convention"
)

// TestIdempotencyInference_TheZeroValueHonoursThePolicy pins the policy's zero
// value as on, so that an operation's Idempotency-Key header names its token
// unless a caller says otherwise, and Disabled as what turns it off.
func TestIdempotencyInference_TheZeroValueHonoursThePolicy(t *testing.T) {
	t.Parallel()
	assert.True(t, convention.IdempotencyInference{}.Honours())
	assert.False(t, convention.IdempotencyInference{Disabled: true}.Honours())
}

// TestSecretInference_TheZeroValueHonoursThePolicy pins the policy's zero value
// as on, so that a credential's name asks for its redaction unless a caller
// says otherwise, and Disabled as what turns it off.
func TestSecretInference_TheZeroValueHonoursThePolicy(t *testing.T) {
	t.Parallel()
	assert.True(t, convention.SecretInference{}.Honours())
	assert.False(t, convention.SecretInference{Disabled: true}.Honours())
}

// TestHTTPDateInference_TheZeroValueHonoursThePolicy pins the policy's zero
// value as on, so that an HTTP-date field's definition gives a header its text
// unless a caller says otherwise, and Disabled as what turns it off.
func TestHTTPDateInference_TheZeroValueHonoursThePolicy(t *testing.T) {
	t.Parallel()
	assert.True(t, convention.HTTPDateInference{}.Honours())
	assert.False(t, convention.HTTPDateInference{Disabled: true}.Honours())
}
