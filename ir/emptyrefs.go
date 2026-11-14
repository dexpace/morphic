// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package ir

import (
	"reflect"
	"slices"
	"strconv"
)

var _ Visitor = (*EmptyReferences)(nil)

// EmptyReference is one position holding no ID where its position requires one.
type EmptyReference struct {
	// Class is the Go type of the ID the position holds.
	Class reflect.Type
	// Path locates the position, as [WalkValues] spells it.
	Path string
	// Locator is set for a Discriminator.Property that is empty while nothing
	// else locates the tag: what it lacks is a way to find the tag rather than
	// a property in particular.
	Locator bool
}

// Code is the diagnostic code of r: ir/empty-<noun>-ref, the noun naming its
// class as [RefNoun] does.
func (r EmptyReference) Code() string {
	return "ir/empty-" + RefNoun(r.Class) + "-ref"
}

// Message says what is wrong at r, in the one wording every checker reports.
func (r EmptyReference) Message() string {
	if r.Locator {
		return "discriminator names no tag property, and neither PropertyName nor Index locates the tag instead"
	}
	return RefNoun(r.Class) + " reference is empty, and this position has no empty form"
}

// EmptyReferences collects, as a [Visitor], every [EmptyReference] its walk
// reaches, so every checker reads one table of which positions admit an empty
// ID. A position the table does not list is held as a reference. A path is
// spelled only for an empty reference, so a sound value costs nothing once its
// type has been met. The zero EmptyReferences is ready for one walk on one
// goroutine.
//
// An entity's own ID, a registry's keys and TypeRef.Target are not read here:
// other checks hold them.
type EmptyReferences struct {
	classes map[reflect.Type]bool // the classes of ID the IR's nodes declare
	fields  map[reflect.Type][]refField
	ranging entryStack
	found   []EmptyReference
}

// Visit records each empty reference a field of v holds, where v is a struct,
// and asks for v's children either way.
func (e *EmptyReferences) Visit(v reflect.Value, at WalkPath) bool {
	if v.Kind() != reflect.Struct {
		return true
	}
	byType, seen := e.fields[v.Type()]
	if !seen {
		if e.classes == nil {
			e.classes = idClasses()
			e.fields = map[reflect.Type][]refField{}
		}
		byType = idFieldsOf(v.Type(), e.classes)
		e.fields[v.Type()] = byType
	}
	for _, f := range byType {
		e.appendEmpty(v, f, at)
	}
	return true
}

// List returns the empty references the walk reached, in the order it reached
// them, the entries of one map in key order.
func (e *EmptyReferences) List() []EmptyReference {
	return slices.Clone(e.found)
}

// appendEmpty records each empty ID the field f of owner holds; at is where
// owner sits.
func (e *EmptyReferences) appendEmpty(owner reflect.Value, f refField, at WalkPath) {
	v := owner.Field(f.index)
	switch f.shape {
	case idScalar:
		if v.String() == "" && !emptyAllowed(owner, f.rule) {
			e.record(f, fieldPath(at, f), f.rule == idLocator)
		}
	case idPointer:
		if !v.IsNil() && v.Elem().String() == "" {
			e.record(f, fieldPath(at, f), false)
		}
	case idSlice:
		for i := range v.Len() {
			if v.Index(i).String() == "" {
				e.record(f, fieldPath(at, f)+"["+strconv.Itoa(i)+"]", false)
			}
		}
	case idMapKey:
		if v.MapIndex(reflect.Zero(v.Type().Key())).IsValid() {
			e.record(f, fieldPath(at, f)+"[]"+MapKeySuffix, false)
		}
	case idMapValue:
		e.appendEmptyValues(v, f, at)
	}
}

// appendEmptyValues records each empty ID the map m, the field f of the struct
// at at, holds as a value, in key order, as the walk ranges the map.
func (e *EmptyReferences) appendEmptyValues(m reflect.Value, f refField, at WalkPath) {
	if m.Len() == 0 {
		return
	}
	frame := e.ranging.push(m)
	for _, entry := range e.ranging.entries[frame.base:frame.end] {
		if entry.value.String() == "" {
			e.record(f, fieldPath(at, f)+"["+entry.label+"]", false)
		}
	}
	e.ranging.pop(frame)
}

// record keeps one empty reference of f's class at path.
func (e *EmptyReferences) record(f refField, path string, locator bool) {
	e.found = append(e.found, EmptyReference{Class: f.class, Path: path, Locator: locator})
}

// fieldPath spells the field f of the struct at at, as the walk spells it.
func fieldPath(at WalkPath, f refField) string {
	return at.String() + "." + f.name
}

// emptyAllowed reports whether an empty scalar at a position under rule is the
// position's documented "none".
func emptyAllowed(owner reflect.Value, rule idRule) bool {
	switch rule {
	case idOptional:
		return true
	case idLocator:
		return owner.FieldByName("PropertyName").String() != "" || !owner.FieldByName("Index").IsNil()
	case idRequired, idElsewhere:
		// A reference has no empty form, and idFieldsOf reads no position
		// another check holds.
	}
	return false
}
