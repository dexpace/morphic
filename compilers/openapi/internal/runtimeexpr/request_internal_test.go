// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package runtimeexpr

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/compilers/openapi/internal/compileddoc"
	"github.com/dexpace/morphic/ir"
)

// TestRequestValue_AParameterOnlyABindingNamesResolvesNothing drives the guard
// no document reaches: a binding naming a parameter its operation does not
// hold gives a member of it no type to walk, so the value resolves to nothing.
func TestRequestValue_AParameterOnlyABindingNamesResolvesNothing(t *testing.T) {
	t.Parallel()
	in, typ, why := paramInput(compileddoc.New(&ir.Document{}), &ir.Operation{ID: "operation/x"}, "parameter/x/gone", []string{"a"})
	assert.Nil(t, in)
	assert.Empty(t, typ)
	assert.Contains(t, why, "which a binding names and the operation does not hold")
}

// TestWalkedType_AStepFindingNoMemberHasNoType drives the guard no walk the
// steps took reaches: a token naming no member types nothing.
func TestWalkedType_AStepFindingNoMemberHasNoType(t *testing.T) {
	t.Parallel()
	assert.Empty(t, walkedType(compileddoc.New(&ir.Document{Types: ir.TypeRegistry{}}), "type/gone", []string{"a"}))
}
