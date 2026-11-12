// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package pass

import (
	"context"
	"reflect"
	"slices"

	"github.com/dexpace/morphic/ir"
)

var _ ir.Visitor = (*typeRefs)(nil)

// typeDefType is the reflect.Type of ir.TypeDef, which a typeRefs walk tells
// a type's own ID by.
var typeDefType = reflect.TypeFor[ir.TypeDef]()

// memberIDType and memberType are the reflect.Types of ir.EnumMemberID, a
// reference to an Enum's member, and of ir.EnumMember, whose own ID declares it.
var (
	memberIDType = reflect.TypeFor[ir.EnumMemberID]()
	memberType   = reflect.TypeFor[ir.EnumMember]()
)

// unreferenced returns those of ids, in order, that nothing in doc references,
// and ctx's error once ctx is done. A walk the depth cap cut short read too
// little to show any unreferenced, so it returns none.
func unreferenced(ctx context.Context, doc *ir.Document, ids []ir.TypeID, walk walkFunc) ([]ir.TypeID, error) {
	refs, complete, err := countRefs(ctx, doc, ids, walk)
	if err != nil || !complete {
		return nil, err
	}
	return slices.DeleteFunc(slices.Clone(ids), func(id ir.TypeID) bool { return refs[id] > 0 }), nil
}

// countRefs returns how many references doc makes to each of ids, a reference
// to one of an Enum's members counting as one to the Enum, in one walk
// through walk, and false where the depth cap cut the walk short, which read
// too little for a count to be one. It returns ctx's error once ctx is done.
func countRefs(ctx context.Context, doc *ir.Document, ids []ir.TypeID, walk walkFunc) (map[ir.TypeID]int, bool, error) {
	r := refsTo(doc.Types, ids)
	truncated, err := walk(ctx, doc, ir.DocumentPath, r)
	if err != nil {
		return nil, false, err
	}
	return r.refs, !truncated, nil
}

// refsTo returns the typeRefs of ids, which no walk has shown a value, holding
// the members of those of ids that types keys as Enums.
func refsTo(types ir.TypeRegistry, ids []ir.TypeID) *typeRefs {
	r := &typeRefs{
		watched: make(map[ir.TypeID]bool, len(ids)),
		members: map[ir.EnumMemberID]ir.TypeID{},
		refs:    map[ir.TypeID]int{},
		keys:    map[ir.TypeID]int{},
		carries: map[reflect.Type]bool{},
	}
	for _, id := range ids {
		r.watched[id] = true
		if enum, isEnum := types[id].(*ir.Enum); isEnum && enum != nil {
			for _, m := range enum.Members {
				r.members[m.ID] = id
			}
		}
	}
	return r
}

// typeRefs counts the references a walk reaches to each watched type: its
// TypeID anywhere but the registry's key and the type's own ID, which declare
// it, and the ID of one of its members anywhere but the member's own ID. It is
// shown only what can hold a TypeID or a member's ID.
type typeRefs struct {
	watched map[ir.TypeID]bool
	// members maps the ID of each member of a watched Enum to the Enum.
	members map[ir.EnumMemberID]ir.TypeID
	// refs counts the references to each watched type.
	refs map[ir.TypeID]int
	// keys counts the map keys spelling each watched type. The registry keys
	// each type once, so a second key is another map's: a reference.
	keys map[ir.TypeID]int
	// carries holds, for each Go type met, whether a value of it can hold a
	// TypeID, and typesRead counts the types canCarry read to learn it, each
	// once however many values have it, which is what the pruning costs.
	carries   map[reflect.Type]bool
	typesRead int
}

// Visit counts a reference v makes to a watched type, and asks for what v
// holds where it can hold a TypeID or a member's ID.
func (r *typeRefs) Visit(v reflect.Value, at ir.WalkPath) bool {
	switch v.Type() {
	case typeIDType:
		r.countType(ir.TypeID(v.String()), at)
	case memberIDType:
		r.countMember(ir.EnumMemberID(v.String()), at)
	default:
		return r.canCarry(v.Type())
	}
	return false
}

// countMember counts id, a member's ID met at at, as a reference to the Enum
// holding it, where that is watched and at is not the member's own ID.
func (r *typeRefs) countMember(id ir.EnumMemberID, at ir.WalkPath) {
	if owner, name, ok := at.Field(); ok && name == "ID" && owner == memberType {
		return
	}
	if enum, watched := r.members[id]; watched {
		r.refs[enum]++
	}
}

// countType counts id, a TypeID met at at, as a reference to the type where it
// is watched.
func (r *typeRefs) countType(id ir.TypeID, at ir.WalkPath) {
	switch {
	case !r.watched[id]:
	case at.IsMapKey():
		r.keys[id]++
		if r.keys[id] > 1 {
			r.refs[id]++
		}
	case ownID(at):
	default:
		r.refs[id]++
	}
}

// ownID reports whether at is a type's own ID, which declares it.
func ownID(at ir.WalkPath) bool {
	owner, name, ok := at.Field()
	return ok && name == "ID" && reflect.PointerTo(owner).Implements(typeDefType)
}

// canCarry reports whether a value of type t can hold a TypeID or a member's
// ID, at any depth, read the first time t is met. An interface can hold
// anything.
func (r *typeRefs) canCarry(t reflect.Type) bool {
	if known, read := r.carries[t]; read {
		return known
	}
	r.typesRead++
	seen := map[reflect.Type]bool{t: true}
	queue := []reflect.Type{t}
	var carries bool
	for len(queue) > 0 && !carries {
		next := queue[len(queue)-1]
		queue = queue[:len(queue)-1]
		var held []reflect.Type
		carries, held = holds(next)
		for _, h := range held {
			if !seen[h] {
				seen[h] = true
				queue = append(queue, h)
			}
		}
	}
	r.carries[t] = carries
	return carries
}

// holds reports whether t is a TypeID, a member's ID or an interface, which can
// be one, and otherwise returns the types a value of t directly holds.
func holds(t reflect.Type) (bool, []reflect.Type) {
	switch t.Kind() {
	case reflect.String:
		return t == typeIDType || t == memberIDType, nil
	case reflect.Interface:
		return true, nil
	case reflect.Pointer, reflect.Slice, reflect.Array:
		return false, []reflect.Type{t.Elem()}
	case reflect.Map:
		return false, []reflect.Type{t.Key(), t.Elem()}
	case reflect.Struct:
		fields := make([]reflect.Type, t.NumField())
		for i := range fields {
			fields[i] = t.Field(i).Type
		}
		return false, fields
	default:
		return false, nil
	}
}
