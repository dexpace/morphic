// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package pass_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
)

// choiceBetween is a choice of two variants, each naming one parameter.
func choiceBetween(first, second ir.ParamID) ir.ParamChoice {
	return ir.ParamChoice{
		Name: ir.Naming{Source: "which", Canonical: "which"},
		Variants: []ir.ParamVariant{
			{Name: ir.Naming{Source: "first", Canonical: "first"}, Params: []ir.ParamID{first}},
			{Name: ir.Naming{Source: "second", Canonical: "second"}, Params: []ir.ParamID{second}},
		},
	}
}

// TestValidate_ParamChoiceNamesItsOwnOperation pins that a choice among an
// operation's parameters names parameters of that operation: another
// operation's parameter is a well-formed reference, so only this check can say
// it is the wrong one, at the variant that holds it. The choice naming its
// own operation's parameters draws nothing.
func TestValidate_ParamChoiceNamesItsOwnOperation(t *testing.T) {
	t.Parallel()
	const (
		a, b  = ir.ParamID("param/x/get/a/query"), ir.ParamID("param/x/get/b/query")
		other = ir.ParamID("param/x/post/id/query")
	)
	for _, tc := range []struct {
		name   string
		choice ir.ParamChoice
		want   []string
	}{
		{"its own parameters", choiceBetween(a, b), nil},
		{"the other operation's parameter in the second variant", choiceBetween(a, other),
			[]string{"op/x/get/paramChoices/0/variants/1"}},
		{"the other operation's parameter in the first variant", choiceBetween(other, b),
			[]string{"op/x/get/paramChoices/0/variants/0"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			get := paramBindingOp("op/x/get",
				ir.Parameter{ID: a, Name: ir.Naming{Source: "a"}}, ir.Parameter{ID: b, Name: ir.Naming{Source: "b"}})
			get.ParamChoices = []ir.ParamChoice{tc.choice}
			post := paramBindingOp("op/x/post", ir.Parameter{ID: other, Name: ir.Naming{Source: "id"}})

			diags := validated(t, docWithOperations(get, post))
			assert.Equal(t, tc.want, mismatchesAt(diags))
			for _, d := range withCode(diags, "pass/param-binding-mismatch") {
				assert.Equal(t, ir.SeverityError, d.Severity, d.Message)
			}
		})
	}
}

// TestValidate_ParamChoiceNamingNoParameterIsDangling pins that a choice
// naming an ID no operation declares draws the dangling walk's report and not
// this check's, as a binding does.
func TestValidate_ParamChoiceNamingNoParameterIsDangling(t *testing.T) {
	t.Parallel()
	const a = ir.ParamID("param/x/get/a/query")
	get := paramBindingOp("op/x/get", ir.Parameter{ID: a, Name: ir.Naming{Source: "a"}})
	get.ParamChoices = []ir.ParamChoice{choiceBetween(a, "param/x/get/ghost/query")}

	diags := validated(t, docWithOperation(get))
	require.Equal(t, 1, countCode(t, diags, "ir/dangling-param-ref"))
	assert.Empty(t, mismatchesAt(diags), "the dangling walk owns an ID nothing declares")
}
