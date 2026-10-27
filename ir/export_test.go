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

// JSONSpelling returns what jsonSpelling makes of s and whether spelledForJSON
// reads s as that already, or false for ok when s is no decimal literal.
func JSONSpelling(s string) (spelling string, spelled, ok bool) {
	l, ok := scanDecimal(s)
	if !ok {
		return "", false, false
	}
	return jsonSpelling(l), l.spelledForJSON(s), true
}

// IDClasses is idClasses, for the external tests.
var IDClasses = idClasses

// DeclaredIDField returns where struct type t declares its identity, the field's
// index and class, read by the rule an IDReader remembers per type.
func DeclaredIDField(t reflect.Type) (int, reflect.Type) {
	f := declaredIDField(t)
	return f.index, f.class
}
