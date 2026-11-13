// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package lowering_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
)

// TestHTTPDateInference_TheZeroValueHonoursThePolicy pins the policy's zero
// value as on, so that an HTTP-date field's definition gives a header its text
// unless a caller says otherwise, and Disabled as what turns it off.
func TestHTTPDateInference_TheZeroValueHonoursThePolicy(t *testing.T) {
	t.Parallel()
	assert.True(t, lowering.HTTPDateInference{}.Honours())
	assert.False(t, lowering.HTTPDateInference{Disabled: true}.Honours())
}
