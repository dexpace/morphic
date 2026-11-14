// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package openapi

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/compile"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering/loweringtest"
	"github.com/dexpace/morphic/compilers/openapi/internal/whole"
	"github.com/dexpace/morphic/ir"
)

// TestRun_ReadsTheFinishedDocumentOnce holds every step reading the finished
// document to one whole.Document a run: a document whose link and
// paginations those steps read is read through one, built once.
func TestRun_ReadsTheFinishedDocumentOnce(t *testing.T) {
	t.Parallel()
	src, err := os.ReadFile("../../testdata/conformance/openapi/body-alias.yaml")
	require.NoError(t, err)
	built := 0
	count := func(doc *ir.Document) *whole.Document {
		built++
		return whole.New(doc)
	}

	doc, diags, err := run(t.Context(), loweringtest.Context(t, string(src), lowering.Policy{}), compile.NewTypes(), count)
	require.NoError(t, err)
	require.NotNil(t, doc)
	assert.Empty(t, diags)
	assert.Equal(t, 1, built, "one reading of the finished document a run")
}
