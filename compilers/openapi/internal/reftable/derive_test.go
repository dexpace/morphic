// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package reftable_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/budget"
	"github.com/dexpace/morphic/compilers/openapi/internal/reftable"
)

// TestBind_SpendsAStepOnEachTagAnOperationLists pins that an operation's tags,
// a list of strings, are read as an object a walk descends, each tag a step a
// limit counts, rather than as a value it stops at: a table over three tags
// costs two steps more than one over one, and a limit one short of it cuts it.
func TestBind_SpendsAStepOnEachTagAnOperationLists(t *testing.T) {
	t.Parallel()
	steps := func(tags string, limit budget.Limit) *reftable.ReferenceTargetTable {
		cfg := self
		cfg.Limit = limit
		return bound(t, "openapi: 3.1.0\ninfo: {title: t, version: '1'}\npaths: {/a: {get: {tags: "+tags+
			", responses: {}}}}\n", cfg)
	}
	one, three := steps("[a]", budget.Unbounded()), steps("[a, b, c]", budget.Unbounded())
	require.False(t, three.Exhausted())
	assert.Equal(t, one.Steps()+2, three.Steps())
	assert.False(t, steps("[a, b, c]", budget.AtMost(three.Steps())).Exhausted())
	assert.True(t, steps("[a, b, c]", budget.AtMost(three.Steps()-1)).Exhausted())
}
