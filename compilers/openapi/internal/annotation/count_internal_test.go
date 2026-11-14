// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package annotation

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestExactCount_TakesTheCountFromItsLiteral holds a count to the number its
// literal states as YAML 1.2's core schema reads it, not to the model's reading
// of it, and to none where the literal reads as no number.
func TestExactCount_TakesTheCountFromItsLiteral(t *testing.T) {
	t.Parallel()
	got := ExactCount(schemaFromYAMLUnvalidated(t, "minLength: 010"), "minLength", new(int64(8)))
	require.NotNil(t, got)
	assert.Equal(t, int64(10), *got, "010 is ten, whatever the model read")

	assert.Nil(t, ExactCount(schemaFromYAMLUnvalidated(t, "minLength: .inf"), "minLength", new(int64(0))),
		"an infinity, which reads as no number, states no count")
}
