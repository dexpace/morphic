package oas

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestValidFormat_TakesAnyStringWhereNoSyntaxIsNamed pins the format that
// asks for nothing.
func TestValidFormat_TakesAnyStringWhereNoSyntaxIsNamed(t *testing.T) {
	assert.True(t, validFormat(FormatNone, "anything at all"))
	assert.False(t, validFormat(ComponentName, "a b"))
	assert.Equal(t, "1 entry", plural(1))
	assert.Equal(t, "2 entries", plural(2))
}
