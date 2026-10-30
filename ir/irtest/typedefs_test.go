// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package irtest_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/ir/irtest"
)

// TestDocOf_KeysEachDefinitionByItsOwnID pins the registry the builders feed:
// every definition, under the ID it carries.
func TestDocOf_KeysEachDefinitionByItsOwnID(t *testing.T) {
	t.Parallel()
	m, s := irtest.ModelOf("t/x/M", nil), irtest.ScalarOf("t/x/S", "")
	assert.Equal(t, ir.TypeRegistry{"t/x/M": m, "t/x/S": s}, irtest.DocOf(m, s).Types)
	assert.Empty(t, irtest.DocOf().Types)
}

// TestModelOf_ShapesTheModelOnlyThroughMutate pins both arms: no mutate leaves
// the bare model, and a mutate sees the model already carrying its ID.
func TestModelOf_ShapesTheModelOnlyThroughMutate(t *testing.T) {
	t.Parallel()
	assert.Equal(t, &ir.Model{ID: "t/x/M"}, irtest.ModelOf("t/x/M", nil))

	var seen ir.TypeID
	got := irtest.ModelOf("t/x/M", func(m *ir.Model) {
		seen = m.ID
		m.Mixins = irtest.RefsTo("t/x/A")
	})
	assert.Equal(t, ir.TypeID("t/x/M"), seen)
	assert.Equal(t, &ir.Model{ID: "t/x/M", Mixins: []ir.TypeRef{{Target: "t/x/A"}}}, got)
}

// TestScalarOf_IsOpaqueWithoutABase pins that an empty base leaves Base nil
// rather than a reference to the empty ID.
func TestScalarOf_IsOpaqueWithoutABase(t *testing.T) {
	t.Parallel()
	assert.Equal(t, &ir.Scalar{ID: "t/x/S"}, irtest.ScalarOf("t/x/S", ""))
	assert.Equal(t, &ir.Scalar{ID: "t/x/S", Base: &ir.TypeRef{Target: "t/prim/string"}},
		irtest.ScalarOf("t/x/S", "t/prim/string"))
}

// TestRefsTo_RefersToEachIDInOrder pins the order and the empty case.
func TestRefsTo_RefersToEachIDInOrder(t *testing.T) {
	t.Parallel()
	assert.Equal(t, []ir.TypeRef{{Target: "t/x/B"}, {Target: "t/x/A"}}, irtest.RefsTo("t/x/B", "t/x/A"))
	assert.Empty(t, irtest.RefsTo())
}
