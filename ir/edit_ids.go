// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package ir

import (
	"cmp"
	"fmt"
	"reflect"
	"slices"
)

// idCounts lists the IDs the node an update or a replacement starts from and
// the one it ends with declare, so the edit can refuse one adding an ID: a
// nested one collides as silently as a node would.
//
// A scan lists them in the order it meets them, so equal lists, the usual case,
// need no sorting. It enters only what can hold a declaration, skipping
// examples and defaults, and lists a value two paths reach on each, as its
// encoding writes it twice, where a [WalkValues] visitor sees a shared pointer
// once. The zero value is ready.
type idCounts struct {
	ids IDReader
	// node and change list the IDs the node an update starts from and the one
	// it ends with declare.
	node, change []declaredID
	// fields holds, for each struct type met, the indices of its fields that
	// can declare an ID or hold one that does, and declares, for each type met,
	// whether a value of it can.
	fields   map[reflect.Type][]int
	declares map[reflect.Type]bool
	// pending holds the values the running scan has yet to enter. entered counts
	// the values scans have entered, and sorted the lists sorted to compare them,
	// which is what checks cost.
	pending []reflect.Value
	entered int
	sorted  int
}

// declaredID is one ID a node declares, with its class.
type declaredID struct {
	class reflect.Type
	id    string
}

// idCount is how many times the node an update starts from declares an ID, and
// how many times the node it ends with does.
type idCount struct {
	node, change int
}

// scan appends to list each ID root, a pointer to a node or nil, declares.
//
// It ends: no type that can declare an ID holds itself but an operation group
// through its Groups, which every update holds apart, as
// TestEditor_IDCheckReadsABoundedPartOfANode pins.
func (c *idCounts) scan(list []declaredID, root any) []declaredID {
	if c.fields == nil {
		c.fields, c.declares = map[reflect.Type][]int{}, map[reflect.Type]bool{}
	}
	c.pending = append(c.pending[:0], reflect.ValueOf(root))
	for len(c.pending) > 0 {
		v := c.pending[len(c.pending)-1]
		c.pending = c.pending[:len(c.pending)-1]
		list = c.enter(list, v)
	}
	return list
}

// enter appends to list the ID v declares, if any, and queues what v holds that
// can declare one. No IR type holds an array, or an interface below a node an
// edit copies, which TestEditor_CopiesEveryIRTypeWhole holds the IR to.
func (c *idCounts) enter(list []declaredID, v reflect.Value) []declaredID {
	c.entered++
	switch v.Kind() {
	case reflect.Pointer:
		if !v.IsNil() {
			c.pending = append(c.pending, v.Elem())
		}
	case reflect.Struct:
		if class, id, declares := c.ids.DeclaredIDOf(v); declares && id != "" {
			list = append(list, declaredID{class: class, id: id})
		}
		for _, i := range c.fieldsOf(v.Type()) {
			c.pending = append(c.pending, v.Field(i))
		}
	case reflect.Slice:
		if c.canDeclare(v.Type().Elem()) {
			for i := range v.Len() {
				c.pending = append(c.pending, v.Index(i))
			}
		}
	case reflect.Map:
		if c.canDeclare(v.Type().Elem()) {
			for entry := v.MapRange(); entry.Next(); {
				c.pending = append(c.pending, entry.Value())
			}
		}
	default: // a scalar declares nothing
	}
	return list
}

// fieldsOf returns the indices of the fields of struct type t that can declare
// an ID or hold one that does, read the first time t is met.
func (c *idCounts) fieldsOf(t reflect.Type) []int {
	if fields, read := c.fields[t]; read {
		return fields
	}
	var fields []int
	for i := range t.NumField() {
		if c.canDeclare(t.Field(i).Type) {
			fields = append(fields, i)
		}
	}
	c.fields[t] = fields
	return fields
}

// canDeclare reports whether a value of type t can declare an ID or hold one
// that does, read the first time t is met.
func (c *idCounts) canDeclare(t reflect.Type) bool {
	known, read := c.declares[t]
	if !read {
		known = len(idClassesOf([]reflect.Type{t})) > 0
		c.declares[t] = known
	}
	return known
}

// excess returns the ID the node an update ends with declares more often than
// the one it starts from, the least by class and then ID when several do, with
// its counts, and whether there is one. Lists in one order need no sorting;
// others are sorted and read side by side.
func (c *idCounts) excess() (declaredID, idCount, bool) {
	if slices.Equal(c.node, c.change) {
		return declaredID{}, idCount{}, false
	}
	slices.SortFunc(c.node, compareDeclared)
	slices.SortFunc(c.change, compareDeclared)
	c.sorted += 2
	i := 0
	for j := 0; j < len(c.change); {
		d, n := c.change[j], idCount{}
		for ; j < len(c.change) && c.change[j] == d; j++ {
			n.change++
		}
		for ; i < len(c.node) && compareDeclared(c.node[i], d) < 0; i++ {
		}
		for ; i < len(c.node) && c.node[i] == d; i++ {
			n.node++
		}
		if n.change > n.node {
			return d, n, true
		}
	}
	return declaredID{}, idCount{}, false
}

// compareDeclared orders IDs by class name, then by ID.
func compareDeclared(a, b declaredID) int {
	if a.class != b.class {
		return cmp.Compare(a.class.String(), b.class.String())
	}
	return cmp.Compare(a.id, b.id)
}

// clause says how often the node an update ends with declares d, against how
// often from, the node it is held to, does.
func (n idCount) clause(d declaredID, from string) string {
	if n.node == 0 {
		return fmt.Sprintf("the %s %q, which %s does not", d.class, d.id, from)
	}
	return fmt.Sprintf("the %s %q %d times, where %s does %d times", d.class, d.id, n.change, from, n.node)
}
