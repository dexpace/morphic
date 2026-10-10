package engine_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/engine"
	"github.com/dexpace/morphic/internal/testspec"
	"github.com/dexpace/morphic/pass"
)

// TestPass_DefaultPassesOrder pins the explicit order and names of the passes a
// default engine runs. Adding or reordering a pass reddens it, which is the
// point: the order is a decision, not an accident of a loop.
func TestPass_DefaultPassesOrder(t *testing.T) {
	t.Parallel()

	passes := engine.DefaultPasses()
	names := make([]string, 0, len(passes))
	for _, p := range passes {
		names = append(names, p.Name())
	}

	assert.Empty(t, cmp.Diff([]string{"validate"}, names))
	assert.Equal(t, pass.ValidateName, engine.ValidatePass)
}

// TestEngine_RunDisablePassesIsPerName pins that disabling validate silences
// its diagnostics and nothing else, and that naming it twice is harmless.
func TestEngine_RunDisablePassesIsPerName(t *testing.T) {
	t.Parallel()
	eng, err := engine.NewWith(danglingCompiler{})
	require.NoError(t, err)
	path := writeSpec(t, testspec.Tiny)

	res, err := eng.Run(t.Context(), path,
		engine.RunOptions{DisablePasses: []string{"validate", "validate"}})

	require.NoError(t, err)
	require.NotNil(t, res.Document)
	assert.Empty(t, res.Diagnostics)
}

// TestEngine_RunUnknownPassIsRefused pins that an invented pass name is an
// error rather than silently ignored, and that it is refused before the spec
// is read.
func TestEngine_RunUnknownPassIsRefused(t *testing.T) {
	t.Parallel()
	eng, err := engine.NewWith(danglingCompiler{})
	require.NoError(t, err)

	res, err := eng.Run(t.Context(), "/no/such/spec.yaml",
		engine.RunOptions{DisablePasses: []string{"validate", "no-such-pass"}})

	require.ErrorIs(t, err, engine.ErrUnknownPass)
	assert.Contains(t, err.Error(), "no-such-pass")
	assert.Nil(t, res)
}
