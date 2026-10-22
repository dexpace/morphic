package compile

import (
	"context"
	"slices"

	"github.com/dexpace/morphic/ir"
)

// A Settlement is a decision that reads nodes other lowerings intern. A walk
// lowers one declaration at a time, so mid-walk such a decision would read
// whatever had been lowered so far, and its answer would follow declaration
// order (invariant 3). Deferred to Finish, it reads the registry whole.
//
// Settle may rewrite nodes already in reg but must not add or remove one:
// nothing lowers after it, so an added node is one no lowering produced, and a
// removed one leaves every reference to it dangling.
type Settlement interface {
	Settle(reg ir.TypeRegistry) []ir.Diagnostic
}

// Defer queues s for Finish to settle. A nil settlement cannot be taken and is
// refused, as is one deferred after Finish, which would never be.
func (t *Types) Defer(s Settlement) { t.queue(&t.pending, "defer", s) }

// Complete queues s for Finish to settle before every settlement Defer queued,
// whenever either was queued, and refuses what Defer refuses. It is for a
// settlement that finishes a node its lowering left open, because finishing it
// reads nodes not built yet, such as a field several declarations write: every
// settlement Defer queued then reads that node finished, whether it only reads
// it or rewrites it too.
func (t *Types) Complete(s Settlement) { t.queue(&t.completing, "complete", s) }

// queue appends s to q, which verb names, or refuses it as Defer documents.
func (t *Types) queue(q *[]Settlement, verb string, s Settlement) {
	if t.settled {
		t.refuse("%s rejected: the registry has already settled", verb)
		return
	}
	if s == nil {
		t.refuse("%s rejected: nil settlement", verb)
		return
	}
	*q = append(*q, s)
}

// settle takes every Settlement once, those Complete queued before those Defer
// queued, each kind in the order queued, and returns their diagnostics; Finish
// calls it once every promised node is built. It checks ctx before each
// settlement and stops once it is done, leaving a registry the caller must not
// assemble. A settlement that changes how many nodes the registry holds is
// refused and left standing, a compiler bug rather than a recovery. A swap of
// one node for another keeps the count, so the references it leaves dangling
// are left to pass.Validate and irverify.
func (t *Types) settle(ctx context.Context) []ir.Diagnostic {
	t.settled = true
	pending := slices.Concat(t.completing, t.pending)
	t.completing, t.pending = nil, nil
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
