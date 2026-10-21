package compile

import (
	"context"

	"github.com/dexpace/morphic/ir"
)

// A Settlement is a decision about a node that reads nodes other lowerings
// intern. A walk lowers one declaration at a time, so mid-walk such a decision
// would read whatever had been lowered so far, and its answer would follow
// declaration order (invariant 3). Deferred to Finish, it reads the registry
// whole.
//
// Settle may rewrite nodes already in reg but must not add or remove one:
// nothing lowers after it, so an added node is one no lowering produced, and a
// removed one leaves every reference to it dangling.
type Settlement interface {
	Settle(reg ir.TypeRegistry) []ir.Diagnostic
}

// Defer queues s for Finish to settle. A nil settlement cannot be taken and is
// refused, as is one deferred after Finish, which would never be.
func (t *Types) Defer(s Settlement) {
	if t.settled {
		t.refuse("defer rejected: the registry has already settled")
		return
	}
	if s == nil {
		t.refuse("defer rejected: nil settlement")
		return
	}
	t.pending = append(t.pending, s)
}

// settle takes every deferred Settlement once, in the order deferred, and
// returns their diagnostics; Finish is how a compiler calls it, after its last
// lowering returns. Like the walks, it checks ctx before each settlement and
// stops once it is done, leaving a registry the caller must not assemble. A
// settlement that changes how many nodes the registry holds is refused, and
// what it did is left standing: the refusal names a compiler bug, not a
// recovery. A swap of one node for another keeps the count, so the references
// it leaves dangling are left to pass.Validate and irverify.
func (t *Types) settle(ctx context.Context) []ir.Diagnostic {
	t.settled = true
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
