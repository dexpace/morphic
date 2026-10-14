package decls

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestLoadAlone_Refusals covers what listing a pattern first keeps LoadEach
// from reaching: the go command refusing to run for the load itself, and a
// path naming other than one package. It sets the environment, so it cannot
// run in parallel.
func TestLoadAlone_Refusals(t *testing.T) {
	p, err := loadAlone("github.com/dexpace/morphic/internal/decls/testdata/nowhere/...")
	require.Error(t, err)
	assert.Nil(t, p)
	assert.Contains(t, err.Error(), "loaded as 0 packages, want 1")

	t.Setenv("GOFLAGS", "-mod=bogus")
	p, err = loadAlone("github.com/dexpace/morphic/internal/decls/testdata/good/fixture")
	require.Error(t, err)
	assert.Nil(t, p)
	assert.Contains(t, err.Error(), "bogus", "the go command's own complaint is kept")
}
