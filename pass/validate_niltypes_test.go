package pass_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/pass"
)

// TestValidate_NilTypeDefIsReportedNotPanicked covers every TypeDef kind, not
// just the two whose dereference was observed to crash: a nil guard added only
// where a panic was noticed is scoped to the site rather than the mechanism, and
// any future check that dereferences a matched TypeDef reintroduces the fault.
func TestValidate_NilTypeDefIsReportedNotPanicked(t *testing.T) {
	t.Parallel()
	kinds := map[string]ir.TypeDef{
		"primitive": (*ir.Primitive)(nil),
		"scalar":    (*ir.Scalar)(nil),
		"model":     (*ir.Model)(nil),
		"union":     (*ir.Union)(nil),
		"enum":      (*ir.Enum)(nil),
		"list":      (*ir.List)(nil),
		"map":       (*ir.MapT)(nil),
		"tuple":     (*ir.Tuple)(nil),
		"literal":   (*ir.Literal)(nil),
		"external":  (*ir.External)(nil),
		"any":       (*ir.Any)(nil),
		"untyped":   nil,
	}
	for name, td := range kinds {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			doc := validDoc()
			doc.Types["t/nil"] = td

			var diags []ir.Diagnostic
			require.NotPanics(t, func() { diags = pass.Validate(doc) })

			codes := make([]string, 0, len(diags))
			for _, d := range diags {
				codes = append(codes, d.Code)
			}
			assert.Contains(t, codes, "ir/nil-type",
				"a nil type definition must be reported, not skipped: %v", codes)
		})
	}
}

func TestValidate_TypedNilMappingTargetDoesNotPanic(t *testing.T) {
	t.Parallel()
	var nilModel *ir.Model
	doc := &ir.Document{Types: ir.TypeRegistry{
		"m": &ir.Model{
			ID:            "m",
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
			ID:            "m",
			Discriminator: &ir.Discriminator{Mapping: map[string]ir.TypeID{"x": "s"}},
		},
		"s": &ir.Model{ID: "s", Implements: []ir.TypeRef{{Target: "a"}}},
		"a": &ir.Scalar{ID: "a", Base: &ir.TypeRef{Target: "z"}},
		"z": nilScalar,
	}}

	var diags []ir.Diagnostic
	require.NotPanics(t, func() { diags = pass.Validate(doc) })

	got := codes(diags)
	assert.Contains(t, got, "ir/nil-type")
	assert.Contains(t, got, "pass/discriminator-missing-variant")
}
