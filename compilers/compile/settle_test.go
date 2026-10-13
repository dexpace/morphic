package compile_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/compile"
	"github.com/dexpace/morphic/ir"
)

// settleFunc adapts a function to compile.Settlement.
type settleFunc func(reg ir.TypeRegistry) []ir.Diagnostic

func (f settleFunc) Settle(reg ir.TypeRegistry) []ir.Diagnostic { return f(reg) }

// TestTypes_SettleTakesEachDeferredSettlementOnceInOrder pins what a compiler
// relies on: every settlement it deferred runs, once, against the live
// registry, and the diagnostics come back in the order deferred.
func TestTypes_SettleTakesEachDeferredSettlementOnceInOrder(t *testing.T) {
	t.Parallel()
	types := compile.NewTypes()
	types.Intern("/p", "t/x", func() ir.TypeDef { return &ir.Model{ID: "t/x"} })
	var runs []string
	for _, name := range []string{"first", "second"} {
		types.Defer(settleFunc(func(reg ir.TypeRegistry) []ir.Diagnostic {
			runs = append(runs, name)
			reg["t/x"].Common().Docs.Description += name
			return []ir.Diagnostic{{Code: name}}
		}))
	}

	diags := types.Settle()

	assert.Equal(t, []string{"first", "second"}, runs, "each runs once, in the order deferred")
	require.Len(t, diags, 2)
	assert.Equal(t, "first", diags[0].Code)
	assert.Equal(t, "second", diags[1].Code)
	td, ok := types.Node("t/x")
	require.True(t, ok)
	assert.Equal(t, "firstsecond", td.Common().Docs.Description, "a settlement rewrites the registry the document holds")
	assert.Empty(t, types.Settle(), "a settlement taken is not taken again")
	assert.Empty(t, types.Violations())
}

// TestTypes_SettleWithNothingDeferredReturnsNothing pins the common case: a
// compile that deferred nothing settles to no diagnostics and no violation.
func TestTypes_SettleWithNothingDeferredReturnsNothing(t *testing.T) {
	t.Parallel()
	types := compile.NewTypes()

	assert.Empty(t, types.Settle())
	assert.Empty(t, types.Violations())
}

// TestTypes_DeferRefusesWhatSettleWouldNeverTake pins the two settlements
// Defer refuses rather than drops: one deferred after Settle ran, which no
// later Settle is promised to take, and a nil one, which cannot be taken.
func TestTypes_DeferRefusesWhatSettleWouldNeverTake(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		queue func(types *compile.Types, s compile.Settlement)
	}{
		{"after settling", func(types *compile.Types, s compile.Settlement) {
			types.Settle()
			types.Defer(s)
		}},
		{"nil", func(types *compile.Types, _ compile.Settlement) { types.Defer(nil) }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			types := compile.NewTypes()
			runs := 0
			c.queue(types, settleFunc(func(ir.TypeRegistry) []ir.Diagnostic { runs++; return nil }))

			assert.Empty(t, types.Settle())
			assert.Zero(t, runs)
			assert.Len(t, types.Violations(), 1, "the refusal is recorded, not dropped")
		})
	}
}

// TestTypes_SettlementThatAddsANodeIsRefused pins the one write a settlement
// may not make. Nothing lowers after Settle, so a node one added would be the
// only node no lowering produced; the registry keeps it and reports the breach.
func TestTypes_SettlementThatAddsANodeIsRefused(t *testing.T) {
	t.Parallel()
	types := compile.NewTypes()
	types.Defer(settleFunc(func(reg ir.TypeRegistry) []ir.Diagnostic {
		reg["t/added"] = &ir.Model{ID: "t/added"}
		return nil
	}))

	types.Settle()

	require.Len(t, types.Violations(), 1)
	assert.Contains(t, types.Violations()[0], "added 1 node")
	_, kept := types.Node("t/added")
	assert.True(t, kept, "the node stays, since something may already reference it")
}
