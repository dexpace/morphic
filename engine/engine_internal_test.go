package engine

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/ir"
)

// collidingCompiler claims a single fixed format. Two of them registered
// together make the second Register call fail, driving newEngine's error path.
type collidingCompiler struct{}

func (collidingCompiler) Formats() []compilers.SourceFormat {
	return []compilers.SourceFormat{{Name: "openapi", Version: "3.1"}}
}

func (collidingCompiler) Compile(context.Context, []compilers.Source, compilers.Options) (*ir.Document, []ir.Diagnostic, error) {
	return nil, nil, nil
}

func TestNewEngine_RegisterError(t *testing.T) {
	t.Parallel()
	eng, err := newEngine(collidingCompiler{}, collidingCompiler{})
	require.Error(t, err)
	assert.Nil(t, eng)
	assert.Contains(t, err.Error(), "engine: register compiler")
}
