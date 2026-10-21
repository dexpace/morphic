package compile

import (
	"context"

	"github.com/dexpace/morphic/ir"
)

// A Settlement is a decision about a node that reads nodes other lowerings
// intern. A walk lowers one declaration at a time, so mid-walk such a decision
// would read whatever had been lowered so far, and its answer would follow
// declaration order (invariant 3). Deferred to the next Settle, it reads
// every node the walk that queued it interned.
//
// Settle may rewrite nodes already in reg but must not add or remove one: an
// added node is one no lowering produced, and a removed one leaves every
// reference to it dangling.
type Settlement interface {
	Settle(reg ir.TypeRegistry) []ir.Diagnostic
}

// Defer queues s for the next Settle. A nil one cannot be taken, so it is
// refused. One deferred after the last Settle is never taken; Violations
// reports it.
func (t *Types) Defer(s Settlement) {
	if s == nil {
		t.refuse("defer rejected: nil settlement")
		return
	}
	t.pending = append(t.pending, s)
}

// Settle takes every Settlement deferred since the last call, once, in the
// order deferred, and returns their diagnostics. A compiler calls it after its
// last lowering, and earlier when a later lowering reads what an earlier one
// deferred. It checks ctx before each settlement and stops once it is done,
// leaving a registry the caller must not assemble. A settlement that changes
// the node count is refused and left standing, as a compiler bug. A swap keeps
// the count, so the references it leaves dangling are left to pass.Validate
// and irverify.
func (t *Types) Settle(ctx context.Context) []ir.Diagnostic {
	pending := t.pending
	t.pending = nil
	diags := make([]ir.Diagnostic, 0, len(pending))
	for _, s := range pending {
		if ctx.Err() != nil {
			return diags
		}
		before := len(t.reg)
		diags = append(diags, s.Settle(t.reg)...)
		if after := len(t.reg); after != before {
			t.refuse("settlement %T changed the registry from %d to %d nodes; a settlement may only rewrite nodes the walk interned",
				s, before, after)
		}
	}
	return diags
}
