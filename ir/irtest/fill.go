// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package irtest

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/dexpace/morphic/ir"
)

// fillDepth is how many times Fill enters one struct type on a single path,
// which is how deep it nests ir.Value.
const fillDepth = 3

// Fill populates the value v points to, for a test that must reach every field
// the IR declares: each ir.TypeID gets an ID no other has, every pointer is
// allocated, and every slice and string-keyed map gets one element. Numbers,
// bools, byte slices, other maps and other strings are left zero. It fails the
// test unless v is a non-nil pointer.
func Fill(t *testing.T, v any) {
	t.Helper()
	fill(t, v)
}

// fill holds Fill's logic against the minimal testingT surface, so its refusal
// can be driven by a stub.
func fill(t testingT, v any) {
	t.Helper()
	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Pointer || rv.IsNil() {
		t.Fatalf("irtest: fill: want a non-nil pointer, got %T", v)
	}
	f := &filler{stack: map[reflect.Type]int{}}
	f.fill(rv.Elem())
}

// typeIDType is the reflect.Type of ir.TypeID.
var typeIDType = reflect.TypeFor[ir.TypeID]()

// filler is one Fill: the type IDs and map keys handed out so far, and how many
// times each struct type is entered on the current path.
type filler struct {
	next, keys int
	stack      map[reflect.Type]int
}

// id returns the next distinct type ID.
func (f *filler) id() string {
	f.next++
	return fmt.Sprintf("t/filled-%d", f.next)
}

// fill populates v by its kind.
func (f *filler) fill(v reflect.Value) {
	switch v.Kind() {
	case reflect.String:
		if v.Type() == typeIDType {
			v.SetString(f.id())
		}
	case reflect.Pointer:
		v.Set(reflect.New(v.Type().Elem()))
		f.fill(v.Elem())
	case reflect.Struct:
		f.fillStruct(v)
	case reflect.Slice:
		if v.Type().Elem().Kind() == reflect.Uint8 {
			return
		}
		v.Set(reflect.MakeSlice(v.Type(), 1, 1))
		f.fill(v.Index(0))
	case reflect.Map:
		f.fillMap(v)
	default:
		// Numbers, bools and the like carry no TypeID.
	}
}

// fillStruct populates v's exported fields, unless its type is already entered
// fillDepth times on this path.
func (f *filler) fillStruct(v reflect.Value) {
	t := v.Type()
	if f.stack[t] >= fillDepth {
		return
	}
	f.stack[t]++
	defer func() { f.stack[t]-- }()
	for i := range v.NumField() {
		if t.Field(i).IsExported() {
			f.fill(v.Field(i))
		}
	}
}

// fillMap gives v one entry when its keys are strings, under a key no other map
// of the fill uses: a type ID when the key is one, and otherwise "keyN".
func (f *filler) fillMap(v reflect.Value) {
	if v.Type().Key().Kind() != reflect.String {
		return
	}
	key := reflect.New(v.Type().Key()).Elem()
	if key.Type() == typeIDType {
		key.SetString(f.id())
	} else {
		f.keys++
		key.SetString(fmt.Sprintf("key%d", f.keys))
	}
	elem := reflect.New(v.Type().Elem()).Elem()
	f.fill(elem)
	v.Set(reflect.MakeMap(v.Type()))
	v.SetMapIndex(key, elem)
}
