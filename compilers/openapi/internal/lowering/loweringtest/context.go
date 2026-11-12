// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package loweringtest

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi/internal/load"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
)

// Context returns the context over spec, loaded as the source spec.yaml at
// index 0, under p. It fails t where the load returns an error or refuses the
// source; what the load reports is its own tests' to read.
func Context(t testing.TB, spec string, p lowering.Policy) lowering.Ctx {
	t.Helper()
	doc, _, err := load.Load(t.Context(), 0, compilers.Source{Path: "spec.yaml", Data: []byte(spec)}, load.Options{})
	require.NoError(t, err)
	require.NotNil(t, doc, "the load refused the source")
	return lowering.New(0, doc, p)
}
