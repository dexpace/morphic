package compile

import "github.com/dexpace/morphic/ir"

// A Settlement is a decision about a node that reads nodes other lowerings
// intern. A walk lowers one declaration at a time, so mid-walk such a decision
// would read whatever had been lowered so far, and its answer would follow
// declaration order (invariant 3). Deferred to Settle, it reads the registry
// whole.
//
// Settle may rewrite nodes already in reg but must not add one: nothing lowers
// after it, so a node it added would be the only one no lowering produced.
type Settlement interface {
	Settle(reg ir.TypeRegistry) []ir.Diagnostic
}

// Defer queues s for Settle. One deferred once Settle has run would never be
// taken, and a nil one cannot be, so both are refused.
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

// Settle takes every deferred Settlement once, in the order deferred, and
// returns their diagnostics. A compiler calls it after its last lowering
// returns. A settlement that adds a node is refused; the node stays, as it may
// already be referenced.
func (t *Types) Settle() []ir.Diagnostic {
	t.settled = true
	pending := t.pending
	t.pending = nil
	diags := make([]ir.Diagnostic, 0, len(pending))
	for _, s := range pending {
		before := len(t.reg)
		diags = append(diags, s.Settle(t.reg)...)
		if added := len(t.reg) - before; added != 0 {
			t.refuse("settlement %T added %d node(s); a settlement may only rewrite nodes the walk interned", s, added)
		}
	}
	return diags
}
