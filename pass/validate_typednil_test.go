package pass_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/pass"
)

func TestValidate_TypedNilMappingTargetDoesNotPanic(t *testing.T) {
	t.Parallel()
	var nilModel *ir.Model
	doc := &ir.Document{Types: ir.TypeRegistry{
		"m": &ir.Model{
			TypeCommon:    ir.TypeCommon{ID: "m"},
			Discriminator: &ir.Discriminator{Mapping: map[string]ir.TypeID{"x": "n"}},
		},
		"n": nilModel,
	}}

	var diags []ir.Diagnostic
	require.NotPanics(t, func() { diags = pass.Validate(doc) })

	got := codes(diags)
	assert.Contains(t, got, "ir/nil-type")
	assert.Contains(t, got, "pass/discriminator-missing-variant")
}

func TestValidate_TypedNilAliasInSupertypeChainDoesNotPanic(t *testing.T) {
	t.Parallel()
	var nilScalar *ir.Scalar
	doc := &ir.Document{Types: ir.TypeRegistry{
		"m": &ir.Model{
			TypeCommon:    ir.TypeCommon{ID: "m"},
			Discriminator: &ir.Discriminator{Mapping: map[string]ir.TypeID{"x": "s"}},
		},
		"s": &ir.Model{TypeCommon: ir.TypeCommon{ID: "s"}, Implements: []ir.TypeRef{{Target: "a"}}},
		"a": &ir.Scalar{TypeCommon: ir.TypeCommon{ID: "a"}, Base: &ir.TypeRef{Target: "z"}},
		"z": nilScalar,
	}}

	var diags []ir.Diagnostic
	require.NotPanics(t, func() { diags = pass.Validate(doc) })

	got := codes(diags)
	assert.Contains(t, got, "ir/nil-type")
	assert.Contains(t, got, "pass/discriminator-missing-variant")
}
