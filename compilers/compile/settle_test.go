package compile_test

import (
	"context"
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

	diags := types.Settle(t.Context())

	assert.Equal(t, []string{"first", "second"}, runs, "each runs once, in the order deferred")
	require.Len(t, diags, 2)
	assert.Equal(t, "first", diags[0].Code)
	assert.Equal(t, "second", diags[1].Code)
	td, ok := types.Node("t/x")
	require.True(t, ok)
	assert.Equal(t, "firstsecond", td.Common().Docs.Description, "a settlement rewrites the registry the document holds")
	assert.Empty(t, types.Settle(t.Context()), "a settlement taken is not taken again")
	assert.Empty(t, types.Violations())
}

// TestTypes_SettleStopsBetweenSettlementsOnCancellation pins that Settle
// honours ctx as the walks do: once it is done, no further settlement is
// taken, and what the ones before it reported is returned.
func TestTypes_SettleStopsBetweenSettlementsOnCancellation(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	types := compile.NewTypes()
	var runs []string
	types.Defer(settleFunc(func(ir.TypeRegistry) []ir.Diagnostic {
		runs = append(runs, "first")
		cancel()
		return []ir.Diagnostic{{Code: "first"}}
	}))
	types.Defer(settleFunc(func(ir.TypeRegistry) []ir.Diagnostic {
		runs = append(runs, "second")
		return nil
	}))

	diags := types.Settle(ctx)

	assert.Equal(t, []string{"first"}, runs, "the settlement after the cancellation is not taken")
	require.Len(t, diags, 1)
	assert.Equal(t, "first", diags[0].Code)
	assert.Empty(t, types.Violations(), "a stop is the caller's to act on, not a breach")
}

// TestTypes_SettleWithNothingDeferredReturnsNothing pins the common case: a
// compile that deferred nothing settles to no diagnostics and no violation.
func TestTypes_SettleWithNothingDeferredReturnsNothing(t *testing.T) {
	t.Parallel()
	types := compile.NewTypes()

	assert.Empty(t, types.Settle(t.Context()))
	assert.Empty(t, types.Violations())
}

// TestTypes_DeferRefusesANilSettlement pins that Defer records a nil
// settlement, which cannot be taken, as a breach rather than dropping it.
func TestTypes_DeferRefusesANilSettlement(t *testing.T) {
	t.Parallel()
	types := compile.NewTypes()
	types.Defer(nil)

	assert.Empty(t, types.Settle(t.Context()))
	assert.Len(t, types.Violations(), 1, "the refusal is recorded, not dropped")
}

// TestTypes_ASettlementNoSettleTakesIsAViolation pins that a settlement
// deferred after the last Settle is reported, since nothing takes it.
func TestTypes_ASettlementNoSettleTakesIsAViolation(t *testing.T) {
	t.Parallel()
	types := compile.NewTypes()
	runs := 0
	types.Settle(t.Context())
	types.Defer(settleFunc(func(ir.TypeRegistry) []ir.Diagnostic { runs++; return nil }))

	assert.Zero(t, runs)
	require.Len(t, types.Violations(), 1)
	assert.Contains(t, types.Violations()[0], "1 settlements were deferred after the last Settle")
}

// TestTypes_SettlementThatAddsOrRemovesANodeIsRefused pins the writes a
// settlement may not make. An added node is one no lowering produced, and a
// removed one leaves its references dangling. Either is reported as a breach, and left standing.
func TestTypes_SettlementThatAddsOrRemovesANodeIsRefused(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		seeded  bool // t/other is registered before the settlement runs
		write   func(reg ir.TypeRegistry)
		want    string
		present bool // t/other is registered after it ran
	}{
		{"adds", false, func(reg ir.TypeRegistry) { reg["t/other"] = &ir.Model{ID: "t/other"} }, "from 1 to 2 nodes", true},
		{"removes", true, func(reg ir.TypeRegistry) { delete(reg, "t/other") }, "from 2 to 1 nodes", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			types := compile.NewTypes()
			types.Intern("/p", "t/x", func() ir.TypeDef { return &ir.Model{ID: "t/x"} })
			if c.seeded {
				types.Register("t/other", &ir.Model{ID: "t/other"})
			}
			types.Defer(settleFunc(func(reg ir.TypeRegistry) []ir.Diagnostic {
				c.write(reg)
				return nil
			}))

			types.Settle(t.Context())

			require.Len(t, types.Violations(), 1)
			assert.Contains(t, types.Violations()[0], c.want)
			_, present := types.Node("t/other")
			assert.Equal(t, c.present, present, "what the settlement did is left standing")
		})
	}
}

// TestTypes_SettleTakesWhatIsQueuedAndLeavesDeferOpen pins the early settling
// a compiler uses before a later lowering reads the registry: each Settle takes
// what was queued since the last one, once, in order, and Defer stays open.
func TestTypes_SettleTakesWhatIsQueuedAndLeavesDeferOpen(t *testing.T) {
	t.Parallel()
	types := compile.NewTypes()
	var runs []string
	record := func(name string) compile.Settlement {
		return settleFunc(func(ir.TypeRegistry) []ir.Diagnostic {
			runs = append(runs, name)
			return []ir.Diagnostic{{Code: name}}
		})
	}
	types.Defer(record("first"))
	types.Defer(record("second"))

	diags := types.Settle(t.Context())

	assert.Equal(t, []string{"first", "second"}, runs, "each runs once, in the order deferred")
	require.Len(t, diags, 2)
	assert.Equal(t, "first", diags[0].Code)
	assert.Equal(t, "second", diags[1].Code)

	types.Defer(record("late"))
	assert.Len(t, types.Violations(), 1, "an untaken settlement is reported until a Settle takes it")
	assert.Equal(t, []string{"first", "second"}, runs, "what was deferred late waits for the next Settle")

	diags = types.Settle(t.Context())

	require.Len(t, diags, 1)
	assert.Equal(t, "late", diags[0].Code, "Settle takes the late one and not the two already taken")
	assert.Equal(t, []string{"first", "second", "late"}, runs)
	assert.Empty(t, types.Settle(t.Context()), "nothing is left to take twice")
	assert.Equal(t, []string{"first", "second", "late"}, runs)
	assert.Empty(t, types.Violations())
}
