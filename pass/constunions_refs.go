// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package pass

import (
	"reflect"

	"github.com/dexpace/morphic/ir"
)

var _ ir.Visitor = (*typeRefs)(nil)

var (
	// typeDefType is the reflect.Type of ir.TypeDef, which a typeRefs walk
	// tells a type's own ID by.
	typeDefType = reflect.TypeFor[ir.TypeDef]()
	// memberIDType and enumMemberType are the reflect.Types of ir.EnumMemberID
	// and ir.EnumMember, which a typeRefs walk tells a member's own ID by.
	memberIDType   = reflect.TypeFor[ir.EnumMemberID]()
	enumMemberType = reflect.TypeFor[ir.EnumMember]()
)

// refsTo returns the typeRefs of the branches of types' unions, which no walk
// has shown a value: each type a variant of a union that may be read as an
// Enum names, and each member of one that is an Enum, beside how many variants
// of the registry's unions name each such type.
func refsTo(types ir.TypeRegistry) *typeRefs {
	r := &typeRefs{
		watched: map[ir.TypeID]bool{},
		members: map[ir.EnumMemberID]ir.TypeID{},
		held:    map[ir.TypeID]int{},
		sites:   map[ir.TypeID]int{},
		keys:    map[ir.TypeID]int{},
		carries: map[reflect.Type]bool{},
	}
	for id, td := range types {
		u, isUnion := td.(*ir.Union)
		if !isUnion || u == nil {
			continue
		}
		for _, v := range u.Variants {
			r.held[v.Type.Target]++
		}
		if u.ID == id && mayBeEnum(types, u) {
			for _, v := range u.Variants {
				r.watch(types, v.Type.Target)
			}
		}
	}
	return r
}

// mayBeEnum reports whether u may be read as an Enum: a closed union saying
// nothing beside its variants, each naming an anonymous Literal, Enum or union.
// Reading it decides; this spares a document holding no such union the walk.
func mayBeEnum(types ir.TypeRegistry, u *ir.Union) bool {
	if !u.Closed || !plainUnion(*u) {
		return false
	}
	for _, v := range u.Variants {
		td := types[v.Type.Target]
		switch td.(type) {
		case *ir.Literal, *ir.Enum, *ir.Union:
			if ir.IsNilTypeDef(td) || !td.Common().Anonymous {
				return false
			}
		default:
			return false
		}
	}
	return true
}

// watch has the walk count the references to the type at id and, where it is
// an Enum, to its members.
func (r *typeRefs) watch(types ir.TypeRegistry, id ir.TypeID) {
	r.watched[id] = true
	if enum, isEnum := types[id].(*ir.Enum); isEnum {
		for _, m := range enum.Members {
			r.members[m.ID] = id
		}
	}
}

// typeRefs counts the references a walk meets to the watched types: each
// TypeID naming one anywhere but the registry's key and the type's own ID,
// which declare it, and each EnumMemberID naming a member of one anywhere but
// the member's own ID. It is shown only what can hold either.
//
// A walk shows a node held through one pointer at two places once, so the
// variants naming a type are counted off the registry too (held), where a
// union two keys hold counts twice.
type typeRefs struct {
	watched map[ir.TypeID]bool
	members map[ir.EnumMemberID]ir.TypeID
	// held counts the variants of the registry's unions naming each type, and
	// sites the references the walk met to each watched one, variants
	// included.
	held, sites map[ir.TypeID]int
	// keys counts the map keys spelling each watched type. The registry keys
	// each type once, so a second key is another map's: a reference.
	keys map[ir.TypeID]int
	// carries holds, for each Go type met, whether a value of it can hold a
	// TypeID or an EnumMemberID, and typesRead counts the types canCarry read
	// to learn it, each once however many values have it, which is what the
	// pruning costs.
	carries   map[reflect.Type]bool
	typesRead int
}

// sole reports whether the one reference to id is the variant of the one
// union naming it. A nil r, whose walk was cut short, shows none is.
func (r *typeRefs) sole(id ir.TypeID) bool {
	return r != nil && r.held[id] == 1 && r.sites[id] == 1
}

// onlyVariants reports whether the references to id are the variants of the
// registry's unions naming it, and those are the given number. A nil r, whose
// walk was cut short, shows nothing is.
func (r *typeRefs) onlyVariants(id ir.TypeID, variants int) bool {
	return r != nil && r.held[id] == variants && r.sites[id] == variants
}

// Visit records a reference v makes to a watched type, and asks for what v
// holds where it can hold one.
func (r *typeRefs) Visit(v reflect.Value, at ir.WalkPath) bool {
	switch v.Type() {
	case typeIDType:
		r.typeID(ir.TypeID(v.String()), at)
	case memberIDType:
		if owner, watched := r.members[ir.EnumMemberID(v.String())]; watched && !ownMemberID(at) {
			r.sites[owner]++
		}
	default:
		return r.canCarry(v.Type())
	}
	return false
}

// typeID records the reference id at at makes, where id is watched and at
// does not declare it.
func (r *typeRefs) typeID(id ir.TypeID, at ir.WalkPath) {
	switch {
	case !r.watched[id]:
	case at.IsMapKey():
		r.keys[id]++
		if r.keys[id] > 1 {
			r.sites[id]++
		}
	case ownID(at):
	default:
		r.sites[id]++
	}
}

// ownID reports whether at is a type's own ID, which declares it.
func ownID(at ir.WalkPath) bool {
	owner, name, ok := at.Field()
	return ok && name == "ID" && reflect.PointerTo(owner).Implements(typeDefType)
}

// ownMemberID reports whether at is an enum member's own ID, which declares it.
func ownMemberID(at ir.WalkPath) bool {
	owner, name, ok := at.Field()
	return ok && name == "ID" && owner == enumMemberType
}

// canCarry reports whether a value of type t can hold a TypeID or an
// EnumMemberID, at any depth, read the first time t is met. An interface can
// hold anything.
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

// holds reports whether t is a TypeID, an EnumMemberID or an interface, which
// can be one, and otherwise returns the types a value of t directly holds.
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
