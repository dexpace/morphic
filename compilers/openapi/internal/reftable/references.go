// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package reftable

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
	at   DocumentPointer
	kind RefKind
}

// objects is what a pass of the binder reads of reference objects: the hop
// each writes, read once whatever kind it is read as, and the chain each
// settles to, read as a kind. Its reads go through the binder, whose pointer
// reads come back to it for each reference object they pass.
type objects struct {
	b      *binder
	hops   map[DocumentPointer]objectHop
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

// hopAt is the hop of the reference object at documentPointer, read once and
// remembered, or the chain its pointer passes that must be settled first,
// having remembered nothing (settle).
func (o *objects) hopAt(documentPointer DocumentPointer) (objectHop, objectKey) {
	if objectHop, ok := o.hops[documentPointer]; ok {
		return objectHop, objectKey{}
	}
	if o.b.work.over() {
		o.hops[documentPointer] = objectHop{hop: Hop{Status: OverBudget}}
		return o.hops[documentPointer], objectKey{}
	}
	ref := o.refAt(documentPointer)
	objectHopValue, need := o.readRef(documentPointer.Doc, ref)
	if need.kind != 0 {
		return objectHop{}, need
	}
	if o.b.work.over() {
		objectHopValue = objectHop{hop: Hop{Ref: ref, Status: OverBudget}}
	}
	o.hops[documentPointer] = objectHopValue
	if objectHopValue.hop.Reason == PendingExternalDocumentRead {
		o.b.wait.heldObjects = append(o.b.wait.heldObjects, documentPointer)
	}
	return objectHopValue, objectKey{}
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
	hop := Hop{Ref: ref, Status: Unresolved}
	uri, frag, _ := strings.Cut(ref, "#")
	pointer := fragment.Pointer(ref)
	if strings.Contains(frag, "#") || pointer != "" && !strings.HasPrefix(string(pointer), "/") ||
		!pointer.IsValid() || !utf8.ValidString(string(pointer)) {
		hop.Reason = BadPointer
		return objectHop{hop: hop}, objectKey{}
	}
	to, reason, detail := o.b.named(o.b.resources.document(id), uri)
	if reason != NoReason {
		hop.Reason, hop.Detail = reason, detail
		return objectHop{hop: hop}, objectKey{}
	}
	reading, reason, need := o.b.readFrom(to.root.Doc, to.root.Pointer+pointer, false)
	switch {
	case need.kind != 0:
		return objectHop{}, need
	case reason == NoTarget && to.namedByDollarID:
		hop.Reason, hop.Detail = NoResourceTarget, to.uri
	case reason != NoReason:
		hop.Reason = reason
	default:
		hop.Status, hop.Target, hop.Role = Resolved, reading.at, reading.role()
	}
	node := nodeview.Deref(reading.node)
	_, refs := o.b.docs.refOf(reading.at.Doc, node)
	return objectHop{hop: hop, target: reading.state, mapping: node != nil && node.Kind == yaml.MappingNode, refs: refs}, objectKey{}
}

// ofKind is objectHopValue read as a hop of a reference object of kind: objectHopValue itself where it
// lands on a mapping the grammar reads as an object of kind, or raw YAML it
// reads as one, and NotObject where it lands on anything else, such as an
// object of another kind, a schema, a value, or no mapping.
func ofKind(objectHopValue objectHop, kind RefKind) Hop {
	if objectHopValue.hop.Status != Resolved || objectHopValue.mapping && (objectHopValue.target == kind.state() || objectHopValue.target == sRaw) {
		return objectHopValue.hop
	}
	return Hop{Ref: objectHopValue.hop.Ref, Status: Unresolved, Reason: NotObject, Detail: kind.String()}
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
		at  DocumentPointer
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
			for _, frame := range stack[i:] {
				o.hops[frame.at] = objectHop{hop: Hop{Ref: o.refAt(frame.at), Status: Unresolved, Reason: Unpassable}}
			}
			for _, frame := range stack[i+1:] {
				delete(on, frame.key)
			}
			stack = stack[:i+1]
			continue
		}
		on[need] = len(stack)
		stack = append(stack, frame{key: need})
	}
}

// refAt is the $ref the reference object at documentPointer writes, or the key a
// requirement key at documentPointer is read by (finder.requirementKeys).
func (o *objects) refAt(documentPointer DocumentPointer) string {
	if key, ok := o.b.docs.keyRefs[documentPointer]; ok {
		return key
	}
	ref, _ := o.b.docs.refOf(documentPointer.Doc, o.b.docs.node(documentPointer))
	return ref
}

// chain settles where the chain of the reference object key ends,
// following each hop while its target is an object of the kind and writes a
// $ref, and every position it passes with it, as binder.chain does a schema
// $ref's. A chain meeting a hop held for documents the pass has yet to read
// waits, and is settled later (waits). Where a hop's pointer needs another
// chain settled first, it returns that hop's position and that chain, having
// settled nothing.
func (o *objects) chain(key objectKey) (DocumentPointer, objectKey) {
	var path []DocumentPointer
	on := map[DocumentPointer]int{}
	var end Chain
	for documentPointer := key.at; ; {
		objectKeyValue := objectKey{at: documentPointer, kind: key.kind}
		if chain, done := o.chains[objectKeyValue]; done {
			end = chain
			break
		}
		if o.b.wait.onObjects[objectKeyValue] {
			o.waitOn(path, key.kind)
			return DocumentPointer{}, objectKey{}
		}
		if i, looped := on[documentPointer]; looped {
			o.closeLoop(path[i:], key.kind)
			path, end = path[:i], Chain{Status: Cyclic}
			break
		}
		oh, need := o.hopAt(documentPointer)
		if need.kind != 0 {
			return documentPointer, need
		}
		if oh.hop.Status == OverBudget {
			end = Chain{Status: OverBudget}
			break
		}
		on[documentPointer] = len(path)
		path = append(path, documentPointer)
		hop := ofKind(oh, key.kind)
		if hop.Status != Resolved && hop.Reason == PendingExternalDocumentRead {
			o.waitOn(path, key.kind)
			return DocumentPointer{}, objectKey{}
		}
		if hop.Status != Resolved {
			end = Chain{Status: Unresolved}
			break
		}
		o.b.find.landedObject(hop.Target, key.kind, oh.target != key.kind.state())
		if !oh.refs {
			end = Chain{Status: Resolved, End: hop.Target}
			break
		}
		documentPointer = hop.Target
	}
	o.settleChain(path, key.kind, end)
	return DocumentPointer{}, objectKey{}
}

// settleChain settles each position on path, read as kind, with end, the
// answer of the position after the last, counting the hop each position adds
// to a chain that resolved. A loop's members were settled already, and taken
// off path, so every position left leads into what end is.
func (o *objects) settleChain(path []DocumentPointer, kind RefKind, end Chain) {
	end.PositionLiesOnReferenceCycle = false
	for _, documentPointer := range slices.Backward(path) {
		if end.Status == Resolved {
			end.Hops++
		}
		o.chains[objectKey{at: documentPointer, kind: kind}] = end
	}
}

// waitOn notes each position on path, read as kind, as waiting with the chain
// it is on for documents the pass has yet to read.
func (o *objects) waitOn(path []DocumentPointer, kind RefKind) {
	for _, documentPointer := range path {
		o.b.wait.onObjects[objectKey{at: documentPointer, kind: kind}] = true
	}
}

// closeLoop records members, reference objects read as kind, as a loop,
// each settled as its member, to be reported once, as the parser's resolver
// does not (docs/parser-library.md#reference-object-cycle).
func (o *objects) closeLoop(members []DocumentPointer, kind RefKind) {
	sorted := slices.SortedFunc(slices.Values(members), o.b.docs.compare)
	o.b.loops = append(o.b.loops, Loop{Members: sorted, Kind: kind})
	for _, documentPointer := range members {
		o.chains[objectKey{at: documentPointer, kind: kind}] = Chain{Status: Cyclic, PositionLiesOnReferenceCycle: true}
	}
}

// reference is the entry of the reference object objectKeyValue, its chain settled first,
// and reports that it waits for documents the pass has yet to read. One the
// budget, or a cancel, left unsettled is OverBudget.
func (o *objects) reference(objectKeyValue objectKey) (Reference, bool) {
	if _, done := o.chains[objectKeyValue]; !done && !o.b.wait.onObjects[objectKeyValue] {
		o.settle(objectKeyValue)
	}
	if o.b.wait.onObjects[objectKeyValue] {
		return Reference{}, true
	}
	_, key := o.b.docs.keyRefs[objectKeyValue.at]
	reference := Reference{Site: objectKeyValue.at, Kind: objectKeyValue.kind, Hop: Hop{Status: OverBudget}, Chain: Chain{Status: OverBudget}, IsSecurityRequirementKey: key}
	if chain, done := o.chains[objectKeyValue]; done {
		reference.Chain = chain
	}
	if objectHop, read := o.hops[objectKeyValue.at]; read {
		reference.Hop = ofKind(objectHop, objectKeyValue.kind)
	}
	return reference, false
}

// compareObjects orders reference objects by position in docsValue, then by kind.
func compareObjects(docsValue *docs) func(objectKeyValue, otherObjectKey objectKey) int {
	return func(objectKeyValue, otherObjectKey objectKey) int {
		if c := docsValue.compare(objectKeyValue.at, otherObjectKey.at); c != 0 {
			return c
		}
		return int(objectKeyValue.kind) - int(otherObjectKey.kind)
	}
}

// settleAll answers each of keys, the reference objects binder.settle took,
// in order of position and then kind, as settle answers sites.
func (o *objects) settleAll(referenceTargetTable *ReferenceTargetTable, keys []objectKey) {
	slices.SortFunc(keys, compareObjects(o.b.docs))
	for _, objectKey := range keys {
		if o.b.work.cancelled() {
			return
		}
		if reference, waits := o.reference(objectKey); waits {
			o.b.wait.objects[objectKey] = true
		} else {
			referenceTargetTable.references[objectKey] = reference
		}
	}
}

// landings returns the table's object landings, each placed at the least
// reference object whose chain reaches it (attribute), ordered by position and
// then kind.
func (o *objects) landings(referenceTargetTable *ReferenceTargetTable) []ObjectLanding {
	reach := o.attribute(referenceTargetTable)
	out := make([]ObjectLanding, 0, len(o.b.find.objLandings))
	for objectKey := range o.b.find.objLandings {
		site, reached := reach[objectKey]
		out = append(out, ObjectLanding{At: objectKey.at, Kind: objectKey.kind, Name: referenceTargetTable.docs[objectKey.at.Doc].Name, Site: site, Reached: reached})
	}
	slices.SortFunc(out, func(objectLanding, objectLandingValue ObjectLanding) int {
		if c := o.b.docs.compare(objectLanding.At, objectLandingValue.At); c != 0 {
			return c
		}
		return int(objectLanding.Kind) - int(objectLandingValue.Kind)
	})
	return out
}

// attribute returns, for each position read as a kind of object that a
// reference object's chain reaches, the least reference object of that kind
// whose chain does, in whichever document, as binder.attribute does for
// schemas.
func (o *objects) attribute(referenceTargetTable *ReferenceTargetTable) map[objectKey]DocumentPointer {
	starts := make([]Reference, 0, len(referenceTargetTable.references))
	for _, reference := range referenceTargetTable.references {
		if reference.Hop.Status == Resolved {
			starts = append(starts, reference)
		}
	}
	slices.SortFunc(starts, func(reference, referenceValue Reference) int {
		return compareObjects(o.b.docs)(objectKey{at: reference.Site, kind: reference.Kind}, objectKey{at: referenceValue.Site, kind: referenceValue.Kind})
	})
	reach := map[objectKey]DocumentPointer{}
	for _, reference := range starts {
		for documentPointer := reference.Hop.Target; ; {
			objectKeyValue := objectKey{at: documentPointer, kind: reference.Kind}
			if _, seen := reach[objectKeyValue]; seen {
				break
			}
			reach[objectKeyValue] = reference.Site
			objectHop, ok := o.hops[documentPointer]
			if !ok || ofKind(objectHop, reference.Kind).Status != Resolved {
				break
			}
			documentPointer = objectHop.hop.Target
		}
	}
	return reach
}
