// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package compile

import (
	"context"
	"slices"

	"github.com/dexpace/morphic/ir"
)

// A Settlement is a decision that reads nodes other lowerings intern. A walk
// lowers one declaration at a time, so mid-walk such a decision would follow
// declaration order (invariant 3). Deferred to Finish, it reads the registry
// whole.
//
// Settle may rewrite nodes already in reg. It must not remove one, which would
// leave every reference to it dangling, and it adds one only through Register,
// which holds a node it mints to a namespace of its own. One settling many
// nodes asks ctx between them and returns once it is done, as Finish does
// between settlements.
type Settlement interface {
	Settle(ctx context.Context, reg ir.TypeRegistry) []ir.Diagnostic
}

// Defer queues settlement for Finish to settle. A nil settlement cannot be
// taken and is refused, as is one deferred after Finish, which would never be.
func (t *Types) Defer(settlement Settlement) { t.queue(&t.pending, "defer", settlement) }

// QueueSettlementBeforeDeferredOnes queues settlement for Finish to settle
// before every settlement Defer queued, whenever either was queued, and refuses
// what Defer refuses. It is for a settlement that finishes a node its lowering
// left open, because finishing it reads nodes not built yet, such as a field
// several declarations write: every settlement Defer queued then reads that
// node finished, whether it only reads it or rewrites it too.
func (t *Types) QueueSettlementBeforeDeferredOnes(settlement Settlement) {
	t.queue(&t.completing, "complete", settlement)
}

// queue appends settlement to settlements, which verb names, or refuses it as
// Defer documents.
func (t *Types) queue(settlements *[]Settlement, verb string, settlement Settlement) {
	if t.settled {
		t.refuse("%s rejected: the registry has already settled", verb)
		return
	}
	if settlement == nil {
		t.refuse("%s rejected: nil settlement", verb)
		return
	}
	*settlements = append(*settlements, settlement)
}

// settle takes every Settlement once, those QueueSettlementBeforeDeferredOnes
// queued before those Defer queued, each kind in the order queued, and returns
// their diagnostics; Finish calls it once every promised node is built. It
// checks ctx before each settlement and stops once it is done, leaving a
// registry the caller must not assemble. A settlement leaving the registry any
// size but the one its Register calls account for is refused and left standing,
// a compiler bug, not a recovery. A swap of one node for another keeps the
// count, so the references it leaves dangling are left to pass.Validate and
// irverify.
func (t *Types) settle(ctx context.Context) []ir.Diagnostic {
	t.settled = true
	pending := slices.Concat(t.completing, t.pending)
	t.completing, t.pending = nil, nil
	diags := make([]ir.Diagnostic, 0, len(pending))
	for _, settlement := range pending {
		if ctx.Err() != nil {
			return diags
		}
		before, minted := len(t.reg), t.minted
		diags = append(diags, settlement.Settle(ctx, t.reg)...)
		if after := len(t.reg); after != before+t.minted-minted {
			t.refuse("settlement %T changed the registry from %d to %d nodes, having registered %d; "+
				"a settlement rewrites nodes the registry holds, and adds one only through Register",
				settlement, before, after, t.minted-minted)
		}
	}
	return diags
}
