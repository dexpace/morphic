// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package pass_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
)

// TestValidate_AParameterNoBindingPlacesIsUnbound holds the code of a parameter
// an HTTP binding places nowhere to pass/param-unbound, a warning, at the
// binding, and not to pass/param-binding-mismatch, which is an error only: the
// document lowers and the source likely says other than its author meant, where
// a binding naming a parameter twice or one the operation lacks is broken. The
// twin places the parameter at the query and draws neither.
func TestValidate_AParameterNoBindingPlacesIsUnbound(t *testing.T) {
	t.Parallel()
	const unbound, mismatch = "pass/param-unbound", "pass/param-binding-mismatch"
	param := ir.Parameter{ID: "parameter/x/op/q", Name: ir.Naming{Source: "q"}}

	op := paramBindingOp("op", param)
	op.Bindings.HTTP[0].ParamBindings = nil
	diags := validated(t, docWithOperation(op))

	require.Len(t, withCode(diags, unbound), 1)
	got := withCode(diags, unbound)[0]
	assert.Equal(t, ir.SeverityWarning, got.Severity)
	assert.Equal(t, "op/bindings/http/0", got.Provenance.Node)
	assert.Equal(t, `parameter "q" on op/bindings/http/0 is not bound to any HTTP location`, got.Message)
	assert.Empty(t, withCode(diags, mismatch), "an unplaced parameter draws no mismatch")

	placed := validated(t, docWithOperation(paramBindingOp("op", param)))
	assert.Empty(t, withCode(placed, unbound))
	assert.Empty(t, withCode(placed, mismatch))
}

// TestValidate_AParameterPlacedTwiceIsAMismatchAndNotUnbound is the other
// side: a binding placing one parameter twice at the query draws the error it
// always has, and no unbound warning, since the parameter is placed.
func TestValidate_AParameterPlacedTwiceIsAMismatchAndNotUnbound(t *testing.T) {
	t.Parallel()
	op := paramBindingOp("op", ir.Parameter{ID: "parameter/x/op/q", Name: ir.Naming{Source: "q"}})
	op.Bindings.HTTP[0].ParamBindings = append(op.Bindings.HTTP[0].ParamBindings, op.Bindings.HTTP[0].ParamBindings[0])

	diags := validated(t, docWithOperation(op))

	require.Len(t, withCode(diags, "pass/param-binding-mismatch"), 1)
	got := withCode(diags, "pass/param-binding-mismatch")[0]
	assert.Equal(t, ir.SeverityError, got.Severity)
	assert.Equal(t, "op/bindings/http/0", got.Provenance.Node)
	assert.Empty(t, withCode(diags, "pass/param-unbound"))
}
