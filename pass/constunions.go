// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package pass

import (
	"context"
	"maps"
	"slices"

	"github.com/dexpace/morphic/ir"
)

var _ Pass = ConstUnions{}

// ConstUnionsName is the stable name of the [ConstUnions] pass.
const ConstUnionsName = "const-unions"

// ConstUnions rewrites a closed union of strings as the closed [ir.Enum] of
// them, at the union's own ID, so a consumer meets one closed set of strings in
// one shape however the document spelled it
// (docs/compilers/openapi.md#string-const-union). A variant names one value, or
// a branch of several that nothing else references and that says nothing of
// them all, which moves into the Enum. Where the Enum could not say all the
// union does, the union stays. It holds no state, so it is safe for concurrent
// use.
type ConstUnions struct{}

// Name returns [ConstUnionsName].
func (ConstUnions) Name() string { return ConstUnionsName }

// Run returns doc with each union constEnums reads as an Enum replaced by it,
// each branch it took removed, and each type only those unions named, which
// nothing references once they are gone, removed. It returns doc unchanged
// where no union is one, and with nothing once ctx is done.
func (ConstUnions) Run(ctx context.Context, doc *ir.Document) (*ir.Document, []ir.Diagnostic) {
	return runConstUnions(ctx, doc, ir.WalkValuesContext)
}

// runConstUnions is Run, walking doc through walk.
func runConstUnions(ctx context.Context, doc *ir.Document, walk walkFunc) (*ir.Document, []ir.Diagnostic) {
	if doc == nil {
		return doc, nil
	}
	sole, err := soleBranches(ctx, doc, walk)
	if err != nil {
		return doc, nil
	}
	rw := constEnums(doc, sole)
	if len(rw.enums) == 0 || !rw.sound || ctx.Err() != nil {
		return doc, nil
	}
	e := ir.NewEditor(doc)
	for _, enum := range rw.enums {
		e.ReplaceType(enum)
	}
	for _, id := range rw.taken {
		e.RemoveType(id)
	}
	out, diags := e.Finish()

	dead, err := unreferenced(ctx, out, rw.branches, walk)
	if err != nil {
		return doc, nil
	}
	if len(dead) == 0 {
		return out, diags
	}
	trim := ir.NewEditor(out)
	for _, id := range dead {
		trim.RemoveType(id)
	}
	trimmed, more := trim.Finish()
	return trimmed, append(diags, more...)
}

// soleBranches returns the types that only one reference names, among those a
// union could take as a branch of several values, which a count of the
// references to them tells. A document with none of them is not walked, and one
// the depth cap cut short has none, as no count of it is one.
func soleBranches(ctx context.Context, doc *ir.Document, walk walkFunc) (map[ir.TypeID]bool, error) {
	ids := branchCandidates(doc)
	if len(ids) == 0 {
		return nil, nil
	}
	refs, complete, err := countRefs(ctx, doc, ids, walk)
	if err != nil || !complete {
		return nil, err
	}
	sole := make(map[ir.TypeID]bool, len(ids))
	for _, id := range ids {
		sole[id] = refs[id] == 1
	}
	return sole, nil
}

// branchCandidates returns, in order, the anonymous Enums of several members and
// anonymous unions that a union keyed at its own ID names as a variant. A
// superset of what a union can take, which costs a count and decides nothing.
func branchCandidates(doc *ir.Document) []ir.TypeID {
	found := map[ir.TypeID]bool{}
	for id, td := range doc.Types {
		u, isUnion := td.(*ir.Union)
		if !isUnion || u == nil || u.ID != id {
			continue
		}
		for _, v := range u.Variants {
			if candidate(doc.Types[v.Type.Target], v.Type.Target) {
				found[v.Type.Target] = true
			}
		}
	}
	return slices.Sorted(maps.Keys(found))
}

// candidate reports whether td, the type keyed at id, is an anonymous Enum of
// several members or an anonymous union keyed at its own ID.
func candidate(td ir.TypeDef, id ir.TypeID) bool {
	switch t := td.(type) {
	case *ir.Enum:
		return t != nil && t.Anonymous && len(t.Members) > 1
	case *ir.Union:
		return t != nil && t.Anonymous && t.ID == id
	default:
		return false
	}
}

// rewrite is what constEnums decides to do to a document.
type rewrite struct {
	// enums holds, in ID order, the Enum of each union that becomes one and that
	// no other union took, and taken the branches those Enums took the values
	// of. branches holds the other types the unions named, which may be
	// referenced still.
	enums    []*ir.Enum
	taken    []ir.TypeID
	branches []ir.TypeID
	// built counts the members of those Enums, which is what building them costs,
	// and sound is false where one has no ID to take.
	built int
	sound bool
}

// constEnums returns what to rewrite in doc: the Enum of each union that
// constValues reads as one, which holds the values of the branches in sole that
// its variants are the only references to. A union off its own key is left to
// validate.
func constEnums(doc *ir.Document, sole map[ir.TypeID]bool) rewrite {
	r := readingOf(doc, sole)
	rw := rewrite{sound: true, taken: slices.Sorted(maps.Keys(r.taken))}
	for _, id := range slices.Sorted(maps.Keys(r.values)) {
		if r.taken[id] {
			continue
		}
		v := r.values[id]
		members, sound := v.members(id)
		rw.built += len(members)
		rw.sound = rw.sound && sound
		rw.enums = append(rw.enums, &ir.Enum{TypeCommon: v.common, ValueType: v.prim, Members: members, Closed: true})
	}
	rw.branches = slices.DeleteFunc(slices.Sorted(maps.Keys(r.named)), func(id ir.TypeID) bool { return r.taken[id] })
	return rw
}

// readingOf returns the reading of each union of doc, which takes the branches
// in sole.
func readingOf(doc *ir.Document, sole map[ir.TypeID]bool) *unionReading {
	r := &unionReading{types: doc.Types, spaces: doc.IDSpaces[ir.IDKindEnumMember], sole: sole,
		values: map[ir.TypeID]*values{}, named: map[ir.TypeID]bool{}, taken: map[ir.TypeID]bool{},
		opened: map[ir.TypeID]bool{}, read: map[ir.TypeID]bool{}}
	for id := range doc.Types {
		r.settle(id)
	}
	return r
}

// unionReading reads each union of a registry as constValues does, after the
// unions its variants name, each as the values it holds: a union naming one is
// rewritten in the run that rewrites it, where constValues reads it as values,
// so a second run finds none left (docs/architecture.md §2.2).
type unionReading struct {
	types  ir.TypeRegistry
	spaces []string
	// sole holds the types only one reference names, among those that can be taken.
	sole map[ir.TypeID]bool
	// values holds what each union read as an Enum holds, named the types those
	// unions' variants name, and taken those whose values a union took.
	values       map[ir.TypeID]*values
	named, taken map[ir.TypeID]bool
	// opened holds each union whose variants were queued to be read first, and
	// read each union read.
	opened, read map[ir.TypeID]bool
	// stack holds the IDs settle has yet to read.
	stack []ir.TypeID
	// reads counts the unions read, each once however many name it, and moved
	// the keys added to a set of them, which is what a reading costs.
	reads, moved int
}

// settle reads the union at root, if it is one keyed at its own ID and not
// read yet, after each union its variants name. A union met again through a
// cycle is read before what it names is: no union whose variants reach it
// again is an Enum, read in either order.
func (r *unionReading) settle(root ir.TypeID) {
	r.stack = append(r.stack[:0], root)
	for len(r.stack) > 0 {
		id := r.stack[len(r.stack)-1]
		u := r.union(id)
		switch {
		case u == nil || r.read[id]:
			r.stack = r.stack[:len(r.stack)-1]
		case !r.opened[id]:
			r.opened[id] = true
			for _, v := range u.Variants {
				r.stack = append(r.stack, v.Type.Target)
			}
		default:
			r.stack = r.stack[:len(r.stack)-1]
			r.read[id] = true
			r.readUnion(u)
		}
	}
}

// readUnion records the values u holds, where constValues reads it as an Enum,
// and the branches it took, once it has.
func (r *unionReading) readUnion(u *ir.Union) {
	r.reads++
	v, ok := r.constValues(u)
	if !ok {
		return
	}
	r.values[u.ID] = v
	for _, variant := range u.Variants {
		r.named[variant.Type.Target] = true
	}
	for _, p := range v.parts {
		if p.taken != nil {
			r.taken[p.id] = true
		}
	}
}

// union returns the union the registry keys at id under its own ID, or nil.
func (r *unionReading) union(id ir.TypeID) *ir.Union {
	u, isUnion := r.types[id].(*ir.Union)
	if !isUnion || u == nil || u.ID != id {
		return nil
	}
	return u
}
