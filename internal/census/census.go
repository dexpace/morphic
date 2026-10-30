// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package census

import (
	"errors"
	"fmt"
	"go/constant"
	"maps"
	"reflect"

	"github.com/dexpace/morphic/ir"
)

// ErrNilDocument is returned when a census is asked to count no document.
var ErrNilDocument = errors.New("census: nil document")

// errNoDeclaration reports a value whose type the declarations do not describe
// as the build does.
var errNoDeclaration = errors.New("census: this build's IR differs from the declarations it read")

// tally is how many documents set a fact and how many times all of them did.
type tally struct {
	docs      int
	instances int
}

// valueKey names one value of one enumeration, by its spelling.
type valueKey struct {
	enum     string
	spelling string
}

// Census counts the facts of a Universe that the documents added to it set.
// The zero value is not usable; build one with New. It is not safe for
// concurrent use.
type Census struct {
	universe   Universe
	documents  int
	truncated  int
	noDocument int
	members    map[string]tally
	empties    map[string]tally
	values     map[valueKey]tally
	// undeclared holds each value a document held that its enumeration
	// declares no constant for.
	undeclared map[valueKey]constant.Value
	// checked holds the members of each struct type met so far, once its
	// fields are found to be the ones its declaration names.
	checked map[reflect.Type][]member
}

// New returns an empty census of the universe u.
func New(u Universe) *Census {
	return &Census{
		universe:   u,
		members:    map[string]tally{},
		empties:    map[string]tally{},
		values:     map[valueKey]tally{},
		undeclared: map[valueKey]constant.Value{},
		checked:    map[reflect.Type][]member{},
	}
}

// NoDocument counts a run that produced no document, which Add never sees.
func (c *Census) NoDocument() {
	c.noDocument++
}

// Add counts the facts doc sets, whatever diagnostics it carries. A value two
// places share is counted once, where the walk of [ir.WalkValues] first meets
// it, and a walk cut at its depth bound still counts what it reached and is
// counted as truncated. A document holding a value whose type differs from
// its declaration is refused and counts nothing.
func (c *Census) Add(doc *ir.Document) error {
	if doc == nil {
		return ErrNilDocument
	}
	return c.add(doc)
}

// add counts the facts the value root holds, whatever its type.
func (c *Census) add(root any) error {
	d := &docCount{
		census:     c,
		members:    map[string]int{},
		empties:    map[string]int{},
		values:     map[valueKey]int{},
		undeclared: map[valueKey]constant.Value{},
	}
	truncated := ir.WalkValues(root, ir.DocumentPath, ir.VisitFunc(d.visit))
	if d.err != nil {
		return d.err
	}

	c.documents++
	if truncated {
		c.truncated++
	}
	merge(c.members, d.members)
	merge(c.empties, d.empties)
	merge(c.values, d.values)
	maps.Copy(c.undeclared, d.undeclared)
	return nil
}

// merge folds the instance counts of one document into the running tallies:
// each fact the document set gains one document and its instances.
func merge[K comparable](into map[K]tally, doc map[K]int) {
	for k, n := range doc {
		t := into[k]
		t.docs++
		t.instances += n
		into[k] = t
	}
}

// docCount is what one document's walk has counted so far, and the first
// value it could not count.
type docCount struct {
	census     *Census
	members    map[string]int
	empties    map[string]int
	values     map[valueKey]int
	undeclared map[valueKey]constant.Value
	err        error
}

// visit counts v and descends into it, and counts nothing once a value could
// not be counted.
func (d *docCount) visit(v reflect.Value, _ ir.WalkPath) bool {
	if d.err != nil {
		return false
	}
	switch v.Kind() {
	case reflect.Struct:
		d.err = d.countMembers(v)
	case reflect.Interface:
		d.err = d.countVariant(v)
	case reflect.String:
		d.countValue(v.Type(), constant.MakeString(v.String()))
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		d.countValue(v.Type(), constant.MakeInt64(v.Int()))
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		d.countValue(v.Type(), constant.MakeUint64(v.Uint()))
	default:
	}
	return true
}

// countMembers counts each member of the struct v that is set, and each
// emptiable one that is set and empty.
func (d *docCount) countMembers(v reflect.Value) error {
	if !d.census.universe.declares(v.Type()) {
		return nil
	}
	members, err := d.census.membersOf(v.Type())
	if err != nil {
		return err
	}
	for _, m := range members {
		f := v.Field(m.index)
		if !isSet(f, m.emptiable) {
			continue
		}
		d.members[m.name]++
		if m.emptiable && f.Len() == 0 {
			d.empties[m.name]++
		}
	}
	return nil
}

// membersOf returns the members of the struct type t, once its fields are
// found to be the ones its declaration names: a build of one revision reading
// the declarations of another would otherwise count the wrong fields.
func (c *Census) membersOf(t reflect.Type) ([]member, error) {
	if members, met := c.checked[t]; met {
		return members, nil
	}
	sf, declared := c.universe.structs[t.Name()]
	if !declared || len(sf.fields) != t.NumField() {
		return nil, fmt.Errorf("%w: struct %s", errNoDeclaration, t)
	}
	for i, name := range sf.fields {
		if t.Field(i).Name != name {
			return nil, fmt.Errorf("%w: field %d of %s is %s, declared as %s", errNoDeclaration, i, t, t.Field(i).Name, name)
		}
	}
	c.checked[t] = sf.members
	return sf.members, nil
}

// countVariant counts the tag of the variant a sealed sum v holds, which the
// sum reports through Kind and no field holds. A nil sum, or one holding a nil
// pointer, holds no variant.
func (d *docCount) countVariant(v reflect.Value) error {
	if !d.census.universe.declares(v.Type()) || v.IsNil() {
		return nil
	}
	held := v.Elem()
	byPointer := held.Kind() == reflect.Pointer
	if byPointer && held.IsNil() {
		return nil
	}
	t := held.Type()
	if byPointer {
		t = t.Elem()
	}
	key, isVariant := d.census.universe.sums[v.Type().Name()][variantKey{name: t.Name(), byPointer: byPointer}]
	if !isVariant {
		return fmt.Errorf("%w: %s holds %s", errNoDeclaration, v.Type(), held.Type())
	}
	d.values[key]++
	return nil
}

// countValue counts the value val of the type t when t is an enumeration.
func (d *docCount) countValue(t reflect.Type, val constant.Value) {
	if !d.census.universe.declares(t) {
		return
	}
	e, isEnum := d.census.universe.enums[t.Name()]
	if !isEnum {
		return
	}
	key := valueKey{enum: e.name, spelling: val.ExactString()}
	d.values[key]++
	if _, isDeclared := e.declared[key.spelling]; !isDeclared {
		d.undeclared[key] = val
	}
}

// isSet reports whether a member's value is set: not the zero value of its
// type, where an empty slice or map that is not nil is set only when its
// emptiness is a fact, and is otherwise the same as its absence.
func isSet(v reflect.Value, emptiable bool) bool {
	if (v.Kind() == reflect.Slice || v.Kind() == reflect.Map) && !emptiable {
		return v.Len() > 0
	}
	return !v.IsZero()
}
