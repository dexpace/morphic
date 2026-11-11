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

// unreferenced returns those of ids, in order, that nothing in doc references,
// and ctx's error once ctx is done. A walk the depth cap cut short read too
// little to show any unreferenced, so it returns none.
func unreferenced(ctx context.Context, doc *ir.Document, ids []ir.TypeID) ([]ir.TypeID, error) {
	r := watching(ids)
	truncated, err := ir.WalkValuesContext(ctx, doc, ir.DocumentPath, r)
	if err != nil || truncated {
		return nil, err
	}
	return slices.DeleteFunc(slices.Clone(ids), func(id ir.TypeID) bool { return r.reached[id] }), nil
}

// watching returns a typeRefs watching ids, which no walk has shown a value.
func watching(ids []ir.TypeID) *typeRefs {
	r := &typeRefs{
		watched: make(map[ir.TypeID]bool, len(ids)),
		reached: map[ir.TypeID]bool{},
		keys:    map[ir.TypeID]int{},
		carries: map[reflect.Type]bool{},
	}
	for _, id := range ids {
		r.watched[id] = true
	}
	return r
}

// typeRefs finds which of the watched types a walk reaches a reference to: its
// TypeID anywhere but the registry's key and the type's own ID, which declare
// it. It is shown only what can hold a TypeID.
type typeRefs struct {
	watched, reached map[ir.TypeID]bool
	// keys counts the map keys spelling each watched type. The registry keys
	// each type once, so a second key is another map's: a reference.
	keys map[ir.TypeID]int
	// carries holds, for each Go type met, whether a value of it can hold a
	// TypeID.
	carries map[reflect.Type]bool
}

// Visit records a reference v makes to a watched type, and asks for what v
// holds where it can hold a TypeID.
func (r *typeRefs) Visit(v reflect.Value, at ir.WalkPath) bool {
	if v.Type() != typeIDType {
		return r.canCarry(v.Type())
	}
	id := ir.TypeID(v.String())
	switch {
	case !r.watched[id]:
	case at.IsMapKey():
		r.keys[id]++
		r.reached[id] = r.reached[id] || r.keys[id] > 1
	case ownID(at):
	default:
		r.reached[id] = true
	}
	return false
}

// ownID reports whether at is a type's own ID, which declares it.
func ownID(at ir.WalkPath) bool {
	owner, name, ok := at.Field()
	return ok && name == "ID" && reflect.PointerTo(owner).Implements(typeDefType)
}

// canCarry reports whether a value of type t can hold a TypeID, at any depth,
// read the first time t is met. An interface can hold anything.
func (r *typeRefs) canCarry(t reflect.Type) bool {
	if known, read := r.carries[t]; read {
		return known
	}
	seen := map[reflect.Type]bool{t: true}
	queue := []reflect.Type{t}
	carries := false
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

// holds reports whether t is a TypeID or an interface, which can be one, and
// otherwise returns the types a value of t directly holds.
func holds(t reflect.Type) (bool, []reflect.Type) {
	switch t.Kind() {
	case reflect.String:
		return t == typeIDType, nil
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
