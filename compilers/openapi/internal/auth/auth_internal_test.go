package auth

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
)

func TestLowerSecurityRequirement_Nil(t *testing.T) {
	t.Parallel()
	got, diags := lowerSecurityRequirement(lowering.Ctx{}, nil)

	assert.Empty(t, got.Schemes)
	assert.Empty(t, diags)
}
