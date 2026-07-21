package irverify_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/ir/irverify"
)

// nestedListValue returns a Value that is depth levels of single-element lists
// wrapping a number — the shape a deeply-nested array default lowers to.
func nestedListValue(depth int) ir.Value {
	v := ir.Value{Kind: ir.ValueNumber, Num: ir.BigVal("0")}
	for range depth {
		v = ir.Value{Kind: ir.ValueList, List: []ir.Value{v}}
	}
	return v
}

// TestVerify_DeepInBoundsDefaultIsNotTruncated builds a property default nested
// far deeper than the former 256 reflection cap allowed but within the range a
// compiler accepts, and asserts the verifier traverses it fully instead of
// reporting ir/walk-truncated. Regression for the value-depth vs walk-depth
// bound mismatch.
func TestVerify_DeepInBoundsDefaultIsNotTruncated(t *testing.T) {
	def := nestedListValue(200)
	m := &ir.Model{
		TypeCommon: ir.TypeCommon{ID: "t/x/M", Name: ir.Naming{Source: "M", Canonical: "m"}},
		Properties: []ir.Property{{
			ID:       "p/x/M/f",
			Name:     ir.Naming{Source: "f", Canonical: "f"},
			WireName: "f",
			Type:     ir.TypeRef{Target: "t/prim/integer"},
			Default:  &def,
		}},
	}
	doc := &ir.Document{Types: ir.TypeRegistry{
		m.ID:             m,
		"t/prim/integer": &ir.Primitive{TypeCommon: ir.TypeCommon{ID: "t/prim/integer"}, Prim: ir.PrimInteger},
	}}

	for _, v := range irverify.Verify(doc) {
		assert.NotEqual(t, "ir/walk-truncated", v.Code,
			"deep but in-bounds default must not truncate the verifier walk")
	}
}
