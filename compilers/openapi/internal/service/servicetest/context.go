// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package servicetest

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi/internal/load"
	"github.com/dexpace/morphic/compilers/openapi/internal/service"
)

// Context returns the context over spec, loaded as the source spec.yaml at
// index 0, under policy. It fails t where the load returns an error or refuses
// the source; what the load reports is its own tests' to read. A test of the
// schema lowering takes its embedded lowering.Ctx.
func Context(t testing.TB, spec string, policy service.Policy) service.Ctx {
	t.Helper()
	doc, _, err := load.Load(t.Context(), 0, compilers.Source{Path: "spec.yaml", Data: []byte(spec)}, load.Options{})
	require.NoError(t, err)
	require.NotNil(t, doc, "the load refused the source")
	return service.New(0, doc, policy)
}
