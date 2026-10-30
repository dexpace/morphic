// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package oas_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/compilers/openapi/internal/oas"
)

// TestValidFormat_TakesAnyStringWhereNoSyntaxIsNamed pins the format that
// asks for nothing.
func TestValidFormat_TakesAnyStringWhereNoSyntaxIsNamed(t *testing.T) {
	assert.True(t, oas.ValidFormat(oas.FormatNone, "anything at all"))
	assert.False(t, oas.ValidFormat(oas.ComponentName, "a b"))
	assert.Equal(t, "1 entry", oas.Plural(1))
	assert.Equal(t, "2 entries", oas.Plural(2))
}
