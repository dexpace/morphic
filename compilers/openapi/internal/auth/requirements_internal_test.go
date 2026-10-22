package auth

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
)

// TestLowerSecurityRequirement_Nil covers the half of the object guard a parsed
// document cannot reach: a hand-built slice holding a nil entry. It is refused
// like any other entry that is no object, rather than surviving as the empty
// option that reads as "no auth is one acceptable choice" — and silently, since
// there is no document position to place a report at.
func TestLowerSecurityRequirement_Nil(t *testing.T) {
	t.Parallel()
	got, ok, diags := lowerSecurityRequirement(lowering.Ctx{}, nil, "/security/0")

	assert.Empty(t, got.Schemes)
	assert.False(t, ok, "a nil entry is no requirement, so the caller drops it")
	assert.Empty(t, diags)
}
