// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package bind

import (
	"slices"
	"strings"
	"unicode/utf8"

	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/fragment"
	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
)

// objectKey is a position read as a Reference Object of a kind.
type objectKey struct {
	at   Position
	kind RefKind
}

// objects is what a pass of the binder reads of reference objects: the hop
// each writes, read once whatever kind it is read as, and the chain each
// settles to, read as a kind. Its reads go through the binder, whose pointer
// reads come back to it for each reference object they pass.
type objects struct {
	b      *binder
	hops   map[Position]objectHop
	chains map[objectKey]Chain
}

// objectHop is the hop of the $ref a reference object writes, whichever kind
// it is read as, with what a kind is checked against (ofKind): the state the
// grammar reads its target at, whether that holds a mapping, and whether that
// writes a $ref, which a chain follows on.
type objectHop struct {
	hop     Hop
	target  state
	mapping bool
	refs    bool
}

// hopAt is the hop of the reference object at p, read once and
// remembered, or the chain its pointer passes that must be settled first,
// having remembered nothing (settle).
func (o *objects) hopAt(p Position) (objectHop, objectKey) {
	if h, ok := o.hops[p]; ok {
		return h, objectKey{}
	}
	if o.b.work.over() {
		o.hops[p] = objectHop{hop: Hop{Status: OverBudget}}
		return o.hops[p], objectKey{}
	}
	ref, _ := o.b.docs.refOf(p.Doc, o.b.docs.node(p))
	h, need := o.readRef(p.Doc, ref)
	if need.kind != 0 {
		return objectHop{}, need
	}
	if o.b.work.over() {
		h = objectHop{hop: Hop{Ref: ref, Status: OverBudget}}
	}
	o.hops[p] = h
	if h.hop.Reason == Held {
		o.b.wait.heldObjects = append(o.b.wait.heldObjects, p)
	}
	return h, objectKey{}
}

// readRef reads ref, a reference object's $ref written in document id: its
// document part as a schema's is (named), against id, which no $id rebases,
// and its fragment, empty, naming that document's root, or a JSON pointer read
// as RFC 6901 and the decoder read one, not as the parser's resolver does
// (docs/parser-library.md#trailing-empty-token,
// docs/parser-library.md#merge-list-pointer,
// docs/parser-library.md#pointer-token-range). A plain name, a second '#', an
// invalid escape or bytes that are not UTF-8 name no object. Where the pointer
// passes a reference whose chain is not settled, nothing is read and that
// chain is returned as what it needs.
func (o *objects) readRef(id DocID, ref string) (objectHop, objectKey) {
	h := Hop{Ref: ref, Status: Unresolved}
	uri, frag, _ := strings.Cut(ref, "#")
	pointer := fragment.Pointer(ref)
	if strings.Contains(frag, "#") || pointer != "" && !strings.HasPrefix(string(pointer), "/") ||
		!pointer.IsValid() || !utf8.ValidString(string(pointer)) {
		h.Reason = BadPointer
		return objectHop{hop: h}, objectKey{}
	}
	to, reason, detail := o.b.named(o.b.resources.document(id), uri)
	if reason != NoReason {
		h.Reason, h.Detail = reason, detail
		return objectHop{hop: h}, objectKey{}
	}
	r, reason, need := o.b.readFrom(to.root.Doc, to.root.Pointer+pointer, false)
	switch {
	case need.kind != 0:
		return objectHop{}, need
	case reason == NoTarget && to.id:
		h.Reason, h.Detail = NoResourceTarget, to.uri
	case reason != NoReason:
		h.Reason = reason
	default:
		h.Status, h.Target, h.Role = Resolved, r.at, roleOf(r.state)
	}
	n := nodeview.Deref(r.node)
	_, refs := o.b.docs.refOf(r.at.Doc, n)
	return objectHop{hop: h, target: r.state, mapping: n != nil && n.Kind == yaml.MappingNode, refs: refs}, objectKey{}
}

// ofKind is h read as a hop of a reference object of kind: h itself where it
// lands on a mapping the grammar reads as an object of kind, or raw YAML it
// reads as one, and NotObject where it lands on anything else, such as an
// object of another kind, a schema, a value, or no mapping.
func ofKind(h objectHop, kind RefKind) Hop {
	if h.hop.Status != Resolved || h.mapping && (h.target == kind.state() || h.target == sRaw) {
		return h.hop
	}
	return Hop{Ref: h.hop.Ref, Status: Unresolved, Reason: NotObject, Detail: kind.String()}
}

// settle settles the chain of the reference object key, and before it
// each chain the pointer of a hop on it passes, and each one theirs pass, held
// on an explicit stack rather than recursed into: a chain whose hop needs
// another is tried again once that one is settled. A chain needed while it is
// already on the stack closes a cycle of reads each needing the next, none of
// which can resolve before another, so each hop reading one of them is
// settled Unpassable, whichever the cycle was entered from.
func (o *objects) settle(key objectKey) {
	type frame struct {
		key objectKey
		at  Position
	}
	stack := []frame{{key: key}}
	on := map[objectKey]int{key: 0}
	for len(stack) > 0 && !o.b.work.over() && !o.b.work.cancelled() {
		top := len(stack) - 1
		if _, done := o.chains[stack[top].key]; done || o.b.wait.onObjects[stack[top].key] {
			delete(on, stack[top].key)
			stack = stack[:top]
			continue
		}
		at, need := o.chain(stack[top].key)
		if need.kind == 0 {
			continue
		}
		stack[top].at = at
		if i, cyclic := on[need]; cyclic {
			for _, f := range stack[i:] {
				o.hops[f.at] = objectHop{hop: Hop{Ref: o.refAt(f.at), Status: Unresolved, Reason: Unpassable}}
			}
			for _, f := range stack[i+1:] {
				delete(on, f.key)
			}
			stack = stack[:i+1]
			continue
		}
		on[need] = len(stack)
		stack = append(stack, frame{key: need})
	}
}

// refAt is the $ref the reference object at p writes.
func (o *objects) refAt(p Position) string {
	ref, _ := o.b.docs.refOf(p.Doc, o.b.docs.node(p))
	return ref
}

// chain settles where the chain of the reference object key ends,
// following each hop while its target is an object of the kind and writes a
// $ref, and every position it passes with it, as binder.chain does a schema
// $ref's. A chain meeting a hop held for documents the pass has yet to read
// waits, and is settled later (waits). Where a hop's pointer needs another
// chain settled first, it returns that hop's position and that chain, having
// settled nothing.
func (o *objects) chain(key objectKey) (Position, objectKey) {
	var path []Position
	on := map[Position]int{}
	var end Chain
	for p := key.at; ; {
		k := objectKey{at: p, kind: key.kind}
		if c, done := o.chains[k]; done {
			end = c
			break
		}
		if o.b.wait.onObjects[k] {
			o.waitOn(path, key.kind)
			return Position{}, objectKey{}
		}
		if i, looped := on[p]; looped {
			o.closeLoop(path[i:], key.kind)
			path, end = path[:i], Chain{Status: Cyclic}
			break
		}
		oh, need := o.hopAt(p)
		if need.kind != 0 {
			return p, need
		}
		if oh.hop.Status == OverBudget {
			end = Chain{Status: OverBudget}
			break
		}
		on[p] = len(path)
		path = append(path, p)
		h := ofKind(oh, key.kind)
		if h.Status != Resolved && h.Reason == Held {
			o.waitOn(path, key.kind)
			return Position{}, objectKey{}
		}
		if h.Status != Resolved {
			end = Chain{Status: Unresolved}
			break
		}
		o.b.find.landedObject(h.Target, key.kind, oh.target != key.kind.state())
		if !oh.refs {
			end = Chain{Status: Resolved, End: h.Target}
			break
		}
		p = h.Target
	}
	o.settleChain(path, key.kind, end)
	return Position{}, objectKey{}
}

// settleChain settles each position on path, read as kind, with end, the
// answer of the position after the last, counting the hop each position adds
// to a chain that resolved. A loop's members were settled already, and taken
// off path, so every position left leads into what end is.
func (o *objects) settleChain(path []Position, kind RefKind, end Chain) {
	end.Member = false
	for _, p := range slices.Backward(path) {
		if end.Status == Resolved {
			end.Hops++
		}
		o.chains[objectKey{at: p, kind: kind}] = end
	}
}

// waitOn notes each position on path, read as kind, as waiting with the chain
// it is on for documents the pass has yet to read.
func (o *objects) waitOn(path []Position, kind RefKind) {
	for _, p := range path {
		o.b.wait.onObjects[objectKey{at: p, kind: kind}] = true
	}
}

// closeLoop records members, reference objects read as kind, as a loop,
// each settled as its member, to be reported once, as the parser's resolver
// does not (docs/parser-library.md#reference-object-cycle).
func (o *objects) closeLoop(members []Position, kind RefKind) {
	sorted := slices.SortedFunc(slices.Values(members), o.b.docs.compare)
	o.b.loops = append(o.b.loops, Loop{Members: sorted, Kind: kind})
	for _, m := range members {
		o.chains[objectKey{at: m, kind: kind}] = Chain{Status: Cyclic, Member: true}
	}
}

// reference is the entry of the reference object k, its chain settled first,
// and reports that it waits for documents the pass has yet to read. One the
// budget, or a cancel, left unsettled is OverBudget.
func (o *objects) reference(k objectKey) (Reference, bool) {
	if _, done := o.chains[k]; !done && !o.b.wait.onObjects[k] {
		o.settle(k)
	}
	if o.b.wait.onObjects[k] {
		return Reference{}, true
	}
	r := Reference{Site: k.at, Kind: k.kind, Hop: Hop{Status: OverBudget}, Chain: Chain{Status: OverBudget}}
	if c, done := o.chains[k]; done {
		r.Chain = c
	}
	if h, read := o.hops[k.at]; read {
		r.Hop = ofKind(h, k.kind)
	}
	return r, false
}

// compareObjects orders reference objects by position in d, then by kind.
func compareObjects(d *docs) func(x, y objectKey) int {
	return func(x, y objectKey) int {
		if c := d.compare(x.at, y.at); c != 0 {
			return c
		}
		return int(x.kind) - int(y.kind)
	}
}

// settleAll answers each of keys, the reference objects binder.settle took,
// in order of position and then kind, as settle answers sites.
func (o *objects) settleAll(t *Table, keys []objectKey) {
	slices.SortFunc(keys, compareObjects(o.b.docs))
	for _, k := range keys {
		if o.b.work.cancelled() {
			return
		}
		if r, waits := o.reference(k); waits {
			o.b.wait.objects[k] = true
		} else {
			t.references[k] = r
		}
	}
}

// landings returns the table's object landings, each placed at the least
// reference object whose chain reaches it (attribute), ordered by position and
// then kind.
func (o *objects) landings(t *Table) []ObjectLanding {
	reach := o.attribute(t)
	out := make([]ObjectLanding, 0, len(o.b.find.objLandings))
	for k := range o.b.find.objLandings {
		site, reached := reach[k]
		out = append(out, ObjectLanding{At: k.at, Kind: k.kind, Name: t.docs[k.at.Doc].Name, Site: site, Reached: reached})
	}
	slices.SortFunc(out, func(x, y ObjectLanding) int {
		if c := o.b.docs.compare(x.At, y.At); c != 0 {
			return c
		}
		return int(x.Kind) - int(y.Kind)
	})
	return out
}

// attribute returns, for each position read as a kind of object that a
// reference object's chain reaches, the least reference object of that kind
// whose chain does, in whichever document, as binder.attribute does for
// schemas.
func (o *objects) attribute(t *Table) map[objectKey]Position {
	starts := make([]Reference, 0, len(t.references))
	for _, r := range t.references {
		if r.Hop.Status == Resolved {
			starts = append(starts, r)
		}
	}
	slices.SortFunc(starts, func(x, y Reference) int {
		return compareObjects(o.b.docs)(objectKey{at: x.Site, kind: x.Kind}, objectKey{at: y.Site, kind: y.Kind})
	})
	reach := map[objectKey]Position{}
	for _, s := range starts {
		for p := s.Hop.Target; ; {
			k := objectKey{at: p, kind: s.Kind}
			if _, seen := reach[k]; seen {
				break
			}
			reach[k] = s.Site
			h, ok := o.hops[p]
			if !ok || ofKind(h, s.Kind).Status != Resolved {
				break
			}
			p = h.hop.Target
		}
	}
	return reach
}
