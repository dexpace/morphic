package engine

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/frontend"
	"github.com/dexpace/morphic/ir"
)

// collidingFrontend claims a single fixed format. Two of them registered
// together make the second Register call fail, driving newEngine's error path.
type collidingFrontend struct{}

func (collidingFrontend) Formats() []frontend.SourceFormat {
	return []frontend.SourceFormat{{Name: "openapi", Version: "3.1"}}
}

func (collidingFrontend) Parse(context.Context, []frontend.Source, frontend.Options) (*ir.Document, []ir.Diagnostic, error) {
	return nil, nil, nil
}

func TestNewEngine_RegisterError(t *testing.T) {
	t.Parallel()
	eng, err := newEngine(collidingFrontend{}, collidingFrontend{})
	require.Error(t, err)
	assert.Nil(t, eng)
	assert.Contains(t, err.Error(), "engine: register frontend")
}
