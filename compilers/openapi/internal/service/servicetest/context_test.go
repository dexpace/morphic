// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package servicetest_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/service"
	"github.com/dexpace/morphic/compilers/openapi/internal/service/servicetest"
)

// TestContext_LowersWhatTheLoadPhaseRead pins the context the helper hands a
// test: over the document the load phase read and bound, named as the source
// spec.yaml, under the policy it was given.
func TestContext_LowersWhatTheLoadPhaseRead(t *testing.T) {
	t.Parallel()
	const spec = "openapi: 3.1.0\ninfo: {title: T, version: \"1\"}\npaths: {}\n" +
		"components: {schemas: {A: {type: string}}}\n"

	c := servicetest.Context(t, spec, service.Policy{Grouping: service.GroupByPathPrefix})

	require.NotNil(t, c.Doc)
	assert.Equal(t, "3.1.0", c.Doc.GetOpenAPI())
	assert.Equal(t, "spec.yaml", c.Sources()[0].Path)
	assert.Equal(t, 0, c.SrcIndex)
	assert.True(t, c.DeclaresSchema("A"))
	assert.NotNil(t, c.Bindings(), "the load phase bound the source")
	assert.Equal(t, service.GroupByPathPrefix, c.Grouping)
}
