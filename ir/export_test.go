package ir

import "reflect"

// DomainParents exposes domainParents to ir's external tests.
func (k PrimKind) DomainParents() []PrimKind { return k.domainParents() }

// Kept returns the references r keeps for Unresolved to settle, in walk order,
// so ir's external tests can hold the walk to settling what it can as it goes.
func (r *IDReferences) Kept() []IDReference { return r.sites }

// CountingIDReader returns an IDReader that counts in reads each time it reads
// where a struct type declares its identity.
func CountingIDReader(reads *int) *IDReader {
	return &IDReader{fieldOf: func(t reflect.Type) idField {
		*reads++
		return declaredIDField(t)
	}}
}

// IDClasses is idClasses, for the external tests.
var IDClasses = idClasses

// DeclaredIDField returns where struct type t declares its identity, the field's
// index and class, read by the rule an IDReader remembers per type.
func DeclaredIDField(t reflect.Type) (int, reflect.Type) {
	f := declaredIDField(t)
	return f.index, f.class
}

// FlattenSteps returns how many steps one FlattenedProperties walk of root
// takes: each composition entry and model end it reads, each alias scalar it
// reads through, and each property and Requires name it reads.
func FlattenSteps(types TypeRegistry, root TypeID) int {
	f := flattener{types: types}
	f.flatten(root)
	return f.steps
}
