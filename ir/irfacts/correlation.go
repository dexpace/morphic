// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package irfacts

import (
	"container/heap"
	"strings"

	"github.com/dexpace/morphic/ir"
)

// CorrelationVersion is the version of the derivation Correlation's keys come
// from (docs/ir/derived-facts.md#correlation-keys). It moves with every change
// to how a key is derived, so a consumer storing keys records it beside them: a
// key another version derived agrees with none of this one's.
// TestCorrelationVersion_PinsTheDerivation holds it to the keys derived.
const CorrelationVersion = 2

// Correlation returns the correlation key of the entity id names, of any class
// of ID (docs/ir/derived-facts.md#correlation-keys): a key built from what the
// document names the entity by, so it survives a reordered array or a moved
// schema that changes the ID. It is false for an id the document does not
// declare, for an entity no named position reaches, and for one whose key
// another entity of the document shares.
func (x *Index) Correlation(id string) (string, bool) {
	key, ok := x.correlationIndex().keys[id]
	return key, ok
}

// Correlated returns the ID of the entity whose correlation key is key, and
// false when no entity of the document has it. Matching an entity across two
// revisions is Correlation in one and Correlated in the other.
func (x *Index) Correlated(key string) (string, bool) {
	id, ok := x.correlationIndex().ids[key]
	return id, ok
}

// correlationIndex returns the correlation keys of the document, derived once.
func (x *Index) correlationIndex() *correlations {
	x.correlationOnce.Do(func() { x.correlation = newCorrelations(x.doc) })
	return x.correlation
}

// correlations holds each keyed entity's key, and the reverse.
type correlations struct {
	keys map[string]string // entity ID → key
	ids  map[string]string // key → entity ID
}

// candidate offers key to the entity id, and expand, when the entity takes it,
// offers the keys of what the entity names.
type candidate struct {
	key    string
	id     string
	expand func(c *correlator, key string)
}

// correlator settles every entity's key in key order. An entity takes the
// least key any position offers it, unless that key is also the least another
// entity is offered, and then neither takes one. A key only grows as a child
// extends it, so the least key is settled before any key built on it, as a
// shortest-path search settles the nearest vertex first.
type correlator struct {
	doc         *ir.Document
	pending     candidateHeap
	settled     map[string]bool // every entity decided, keyed or not
	out         *correlations
	inline      map[ir.TypeID]bool        // anonymous models another model composes
	ops         map[ir.OpID]*ir.Operation // every operation the walk reaches
	partHeaders map[ir.PropID][]ir.Header // each multipart part's headers, by the property it encodes
}

// newCorrelations derives the correlation keys of doc. A nil doc has none.
func newCorrelations(doc *ir.Document) *correlations {
	out := &correlations{keys: map[string]string{}, ids: map[string]string{}}
	if doc == nil {
		return out
	}
	c := &correlator{
		doc: doc, settled: map[string]bool{}, out: out,
		inline: inlineConjuncts(doc.Types), ops: map[ir.OpID]*ir.Operation{},
	}
	c.seedLayer()
	c.seedTypes()
	for c.pending.Len() > 0 {
		c.settle(c.popLeast())
	}
	return out
}

// offer proposes key for the entity id. An entity already decided ignores it.
func (c *correlator) offer(key, id string, expand func(*correlator, string)) {
	if key == "" || id == "" || c.settled[id] {
		return
	}
	heap.Push(&c.pending, candidate{key: key, id: id, expand: expand})
}

// popLeast removes every pending candidate holding the least key.
func (c *correlator) popLeast() []candidate {
	least, _ := heap.Pop(&c.pending).(candidate)
	batch := []candidate{least}
	for c.pending.Len() > 0 && c.pending[0].key == least.key {
		next, _ := heap.Pop(&c.pending).(candidate)
		batch = append(batch, next)
	}
	return batch
}

// settle decides the entities offered one key. One undecided entity takes it
// and offers what it names; several undecided entities sharing it take none,
// and name nothing through it.
func (c *correlator) settle(batch []candidate) {
	var fresh []candidate
	for _, cand := range batch {
		if !c.settled[cand.id] && !containsID(fresh, cand.id) {
			fresh = append(fresh, cand)
		}
	}
	for _, cand := range fresh {
		c.settled[cand.id] = true
	}
	if len(fresh) != 1 {
		return
	}
	only := fresh[0]
	c.out.keys[only.id] = only.key
	c.out.ids[only.key] = only.id
	if only.expand != nil {
		only.expand(c, only.key)
	}
}

// containsID reports whether some candidate of cs is offered to id.
func containsID(cs []candidate, id string) bool {
	for i := range cs {
		if cs[i].id == id {
			return true
		}
	}
	return false
}

// candidateHeap orders candidates by key, then by ID, so the candidates of one
// key leave it together and in one order whatever the order they arrived in.
type candidateHeap []candidate

func (h candidateHeap) Len() int { return len(h) }

func (h candidateHeap) Less(i, j int) bool {
	if h[i].key != h[j].key {
		return h[i].key < h[j].key
	}
	return h[i].id < h[j].id
}

func (h candidateHeap) Swap(i, j int) { h[i], h[j] = h[j], h[i] }

func (h *candidateHeap) Push(v any) {
	cand, _ := v.(candidate)
	*h = append(*h, cand)
}

func (h *candidateHeap) Pop() any {
	old := *h
	last := old[len(old)-1]
	*h = old[:len(old)-1]
	return last
}

// segment is one segment of a key: a kind, and the value it names escaped as
// RFC 6901 escapes a pointer token, so no value carries the separator between
// segments.
func segment(kind, value string) string {
	return kind + ":" + strings.ReplaceAll(strings.ReplaceAll(value, "~", "~0"), "/", "~1")
}

// child extends key by one segment.
func child(key, kind, value string) string {
	return key + "/" + segment(kind, value)
}

// part extends key by a segment naming a fixed position, which holds no colon
// and so meets no valued segment.
func part(key, position string) string {
	return key + "/" + position
}
