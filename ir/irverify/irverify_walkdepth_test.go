// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package irverify_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/ir/irtest"
)

// TestVerify_DeepInBoundsDefaultIsNotTruncated builds a property default nested
// far deeper than the former 256 reflection cap allowed but within the range a
// compiler accepts, and asserts the verifier traverses it fully instead of
// reporting ir/walk-truncated. Regression for the value-depth vs walk-depth
// bound mismatch.
func TestVerify_DeepInBoundsDefaultIsNotTruncated(t *testing.T) {
	def := irtest.NestedListValue(200)
	m := &ir.Model{
		ID:   "type/x/M",
		Name: ir.Naming{Source: "M", Canonical: "m"},
		Properties: []ir.Property{{
			ID:       "property/x/M/f",
			Name:     ir.Naming{Source: "f", Canonical: "f"},
			WireName: "f",
			Type:     ir.TypeRef{Target: "type/primitive/integer"},
			Default:  &def,
		}},
	}
	doc := &ir.Document{Types: ir.TypeRegistry{
		m.ID:                     m,
		"type/primitive/integer": &ir.Primitive{ID: "type/primitive/integer", PrimitiveKind: ir.PrimitiveInteger},
	}}

	for _, v := range verifyDeclared(doc) {
		assert.NotEqual(t, "ir/walk-truncated", v.Code,
			"deep but in-bounds default must not truncate the verifier walk")
	}
}
