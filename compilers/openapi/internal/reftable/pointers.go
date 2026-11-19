// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package reftable

import (
	"encoding/json/jsontext"
)

// readPointer reads pointer from the root of document id, as RFC 6901 reads
// it, and through each reference object it passes, into what that object's
// chain ends on (through), settling each such chain first.
func (b *binder) readPointer(id DocID, pointer jsontext.Pointer) (reading, Reason) {
	readingValue, reason, _ := b.readFrom(id, pointer, true)
	return readingValue, reason
}

// readFrom is readPointer. With settle false it settles no chain: where it reaches a
// reference object whose chain is not settled, it stops and returns that
// chain's key as what it needs, so the read can be made again once that one is
// (objects.settle). The pointer read so far is kept as bytes and spelled
// into the reading only where one is passed or returned, so a read costs its
// pointer's length rather than its square.
func (b *binder) readFrom(id DocID, pointer jsontext.Pointer, settle bool) (reading, Reason, objectKey) {
	readingValue := reading{at: DocumentPointer{Doc: id}, node: b.docs.list[id].root, state: b.docs.rootState(id)}
	// grammar is the state the grammar places readingValue's position at, which
	// says where a reference object stands: readingValue's own in the source,
	// and in another OpenAPI document what it places there although
	// readingValue reads raw YAML, which no model holds (docs.identityState).
	grammar := b.docs.identityState(id)
	var soFar []byte
	tokens := 0
	for token := range pointer.Tokens() {
		if tokens++; tokens > maxPointerTokens || !b.work.spend(1) {
			return readingValue.spelledAs(soFar), NoTarget, objectKey{}
		}
		if kind, isRef := refKinds[grammar]; isRef && b.docs.writesRef(readingValue) {
			next, reason, need := b.through(readingValue.spelledAs(soFar), kind, token, settle)
			if reason != NoReason || need.kind != 0 {
				return next, reason, need
			}
			readingValue, soFar = next, append(soFar[:0], next.at.Pointer...)
		}
		child := b.docs.child(readingValue.at.Doc, readingValue.node, token)
		if child == nil {
			return readingValue.spelledAs(soFar), b.docs.absent(readingValue.at.Doc, readingValue.node), objectKey{}
		}
		soFar = append(soFar, jsontext.Pointer("").AppendToken(token)...)
		readingValue = reading{at: DocumentPointer{Doc: readingValue.at.Doc}, node: child, place: readingValue.below(token)}
		grammar = step(grammar, token)
	}
	return readingValue.spelledAs(soFar), NoReason, objectKey{}
}

// writesRef reports whether readingValue, read at a reference object's state,
// writes a $ref, which makes it a reference.
func (d *docs) writesRef(readingValue reading) bool {
	_, ok := d.refOf(readingValue.at.Doc, readingValue.node)
	return ok
}

// through carries readingValue, a reference object of kind with token left to
// read, on to where its chain ends, read at readingValue's state afresh
// (place): a pointer through a response reads on in the response it names, in
// whichever document. A path item's token the mapping writes beside its $ref is
// read where it is written, as a mount keeps the nearer layer
// (docs/compilers/openapi.md#path-item-ref-fields). A chain that does not
// resolve is Unpassable, and one that waits for documents a pass has yet to
// read is Held. Without settle, a chain not settled is returned as what the
// read needs.
func (b *binder) through(readingValue reading, kind RefKind, token string, settle bool) (reading, Reason, objectKey) {
	if kind == RefPathItem && token != "$ref" && b.docs.child(readingValue.at.Doc, readingValue.node, token) != nil {
		return readingValue, NoReason, objectKey{}
	}
	key := objectKey{at: readingValue.at, kind: kind}
	chain, settled := b.objs.chains[key]
	if !settled && settle && !b.wait.onObjects[key] {
		b.objs.settle(key)
		chain, settled = b.objs.chains[key]
	}
	switch {
	case !settled && b.wait.onObjects[key]:
		return readingValue, PendingExternalDocumentRead, objectKey{}
	case !settled && !settle:
		return readingValue, NoReason, key
	case chain.Status != Resolved:
		// A chain the budget left unsettled is no chain that resolved: the hop
		// reading through it is answered OverBudget (binder.hop).
		return readingValue, Unpassable, objectKey{}
	}
	return reading{at: chain.End, node: b.docs.node(chain.End), state: readingValue.state}, NoReason, objectKey{}
}
