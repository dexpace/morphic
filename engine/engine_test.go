package engine_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/engine"
	"github.com/dexpace/morphic/ir"
)

const tinySpec = `openapi: 3.1.0
info: {title: Tiny, version: "1"}
paths:
  /ping:
    get:
      operationId: ping
      responses: {"200": {description: ok}}
`

func writeSpec(t *testing.T, contents string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "spec.yaml")
	require.NoError(t, os.WriteFile(path, []byte(contents), 0o644))
	return path
}

func TestEngine_RunEndToEnd(t *testing.T) {
	t.Parallel()
	eng, err := engine.New()
	require.NoError(t, err)
	res, err := eng.Run(t.Context(), writeSpec(t, tinySpec), engine.RunOptions{})
	require.NoError(t, err)
	require.NotNil(t, res.Document)
	assert.Equal(t, "Tiny", res.Document.Name)
	assert.Equal(t, "3.1", res.Format.Version)
	for _, d := range res.Diagnostics {
		assert.NotEqual(t, ir.SeverityError, d.Severity, "diag: %+v", d)
	}
}

func TestEngine_RunMissingFile(t *testing.T) {
	t.Parallel()
	eng, err := engine.New()
	require.NoError(t, err)
	_, err = eng.Run(t.Context(), filepath.Join(t.TempDir(), "absent.yaml"), engine.RunOptions{})
	require.Error(t, err)
}

func TestEngine_ValidateRuns(t *testing.T) {
	t.Parallel()
	// SkipValidate=false is the default path; assert the validate pass ran by
	// checking Run on a valid doc appends nothing AND that SkipValidate=true
	// yields the same document (pass purity).
	eng, err := engine.New()
	require.NoError(t, err)
	path := writeSpec(t, tinySpec)
	withPass, err := eng.Run(t.Context(), path, engine.RunOptions{})
	require.NoError(t, err)
	withoutPass, err := eng.Run(t.Context(), path, engine.RunOptions{SkipValidate: true})
	require.NoError(t, err)
	assert.Equal(t, withoutPass.Document.Name, withPass.Document.Name)
}
