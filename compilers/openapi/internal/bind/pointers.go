// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package bind

import (
	"encoding/json/jsontext"
)

// readPointer reads pointer from the root of document id, as RFC 6901 reads
// it, and through each reference object it passes, into what that object's
// chain ends on (through), settling each such chain first.
func (b *binder) readPointer(id DocID, pointer jsontext.Pointer) (reading, Reason) {
	r, reason, _ := b.readFrom(id, pointer, true)
	return r, reason
}

// readFrom is readPointer. With settle false it settles no chain: where it reaches a
// reference object whose chain is not settled, it stops and returns that
// chain's key as what it needs, so the read can be made again once that one is
// (objects.settle). The pointer read so far is kept as bytes and spelled
// into the reading only where one is passed or returned, so a read costs its
// pointer's length rather than its square.
func (b *binder) readFrom(id DocID, pointer jsontext.Pointer, settle bool) (reading, Reason, objectKey) {
	r := reading{at: Position{Doc: id}, node: b.docs.list[id].root, state: b.docs.rootState(id)}
	var soFar []byte
	tokens := 0
	for token := range pointer.Tokens() {
		if tokens++; tokens > maxPointerTokens || !b.work.spend(1) {
			return r.spelledAs(soFar), NoTarget, objectKey{}
		}
		if _, isRef := refKinds[r.state]; isRef && b.docs.writesRef(r) {
			next, reason, need := b.through(r.spelledAs(soFar), token, settle)
			if reason != NoReason || need.kind != 0 {
				return next, reason, need
			}
			r, soFar = next, append(soFar[:0], next.at.Pointer...)
		}
		child := b.docs.child(r.at.Doc, r.node, token)
		if child == nil {
			return r.spelledAs(soFar), b.docs.absent(r.at.Doc, r.node), objectKey{}
		}
		soFar = append(soFar, jsontext.Pointer("").AppendToken(token)...)
		r = reading{at: Position{Doc: r.at.Doc}, node: child, state: step(r.state, token)}
	}
	return r.spelledAs(soFar), NoReason, objectKey{}
}

// writesRef reports whether r, read at a reference object's state, writes a
// $ref, which makes it a reference.
func (d *docs) writesRef(r reading) bool {
	_, ok := d.refOf(r.at.Doc, r.node)
	return ok
}

// through carries r, a reference object with token left to read, on to where
// its chain ends, read at r's state: a pointer through a response reads on in
// the response it names, in whichever document. A path item's token the
// mapping writes beside its $ref is read where it is written, as a mount keeps
// the nearer layer (docs/compilers/openapi.md#path-item-ref-fields). A chain
// that does not resolve is Unpassable, and one that waits for documents a pass
// has yet to read is Held. Without settle, a chain not settled is returned as
// what the read needs.
func (b *binder) through(r reading, token string, settle bool) (reading, Reason, objectKey) {
	kind := refKinds[r.state]
	if kind == RefPathItem && token != "$ref" && b.docs.child(r.at.Doc, r.node, token) != nil {
		return r, NoReason, objectKey{}
	}
	key := objectKey{at: r.at, kind: kind}
	c, settled := b.objs.chains[key]
	if !settled && settle && !b.wait.onObjects[key] {
		b.objs.settle(key)
		c, settled = b.objs.chains[key]
	}
	switch {
	case !settled && b.wait.onObjects[key]:
		return r, Held, objectKey{}
	case !settled && !settle:
		return r, NoReason, key
	case c.Status != Resolved:
		// A chain the budget left unsettled is no chain that resolved: the hop
		// reading through it is answered OverBudget (binder.hop).
		return r, Unpassable, objectKey{}
	}
	return reading{at: c.End, node: b.docs.node(c.End), state: r.state}, NoReason, objectKey{}
}
