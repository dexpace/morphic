// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package ir

import (
	"iter"
	"maps"
	"reflect"
	"slices"
)

// DomainParents exposes domainParents to ir's external tests.
func (k PrimitiveKind) DomainParents() []PrimitiveKind { return k.domainParents() }

// Kept returns the references r keeps for Unresolved to settle, in walk order,
// each with its path spelled, so ir's external tests can hold the walk to
// settling what it can as it goes.
func (r *IDReferences) Kept() []IDReference {
	kept := slices.Clone(r.sites)
	for i, at := range r.paths.All() {
		kept[i].Path = at.String()
	}
	return kept
}

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
	decimalLiteral, ok := scanDecimal(s)
	if !ok {
		return "", false, false
	}
	return jsonSpelling(decimalLiteral), decimalLiteral.spelledForJSON(s), true
}

// IDClasses returns idClasses, for the external tests.
func IDClasses() map[reflect.Type]bool { return idClasses }

// Locator reports whether r is a discriminator's tag property that nothing
// locates, so ir's external tests can tell it apart.
func (r EmptyReference) Locator() bool { return r.locator }

// IDPositions returns the "Type.Field" keys idPositions classifies.
func IDPositions() []string {
	return slices.Sorted(maps.Keys(idPositions))
}

// EmptyFormPositions returns the "Type.Field" keys idPositions gives an empty
// form of their own: optional ones and the discriminator's locator.
func EmptyFormPositions() []string {
	var keys []string
	for key, rule := range idPositions {
		if rule == idOptional || rule == idLocator {
			keys = append(keys, key)
		}
	}
	slices.Sort(keys)
	return keys
}

// RequiredIDFields returns the names of the fields of struct type t that the
// empty-reference reading holds as references, an empty one being reported.
func RequiredIDFields(t reflect.Type) []string {
	var names []string
	for _, refField := range idFieldsOf(t, idClasses) {
		if refField.rule == idRequired {
			names = append(names, refField.name)
		}
	}
	return names
}

// DeclaredIDField returns where struct type t declares its identity, the field's
// index and class, read by the rule an IDReader remembers per type.
func DeclaredIDField(t reflect.Type) (int, reflect.Type) {
	idField := declaredIDField(t)
	return idField.index, idField.class
}

// FlattenSteps returns how many steps one FlattenedProperties walk of root
// takes: each composition entry and model end it reads, each alias scalar it
// reads through, each property and Requires name it reads, and each type it
// reads a redaction off.
func FlattenSteps(types TypeRegistry, root TypeID) int {
	flattenerValue := flattener{types: types}
	flattenerValue.flatten(root)
	return flattenerValue.steps
}

// TreeWork returns how many nodes editor has stepped through and elements of
// the service tree's slices it has built, which is what its writes cost.
func TreeWork(editor *Editor) int { return editor.tree.work }

// IDsScanned returns how many values editor's ID check has entered, which is
// what its scans cost.
func IDsScanned(editor *Editor) int { return editor.declared.entered }

// IDListsSorted returns how many lists editor's ID check has sorted to compare
// them.
func IDListsSorted(editor *Editor) int { return editor.declared.sorted }

// StoredSteps returns how many steps l stores for the paths it holds, so ir's
// external tests can hold it to keeping each step a run of paths shares once.
func (l *PathLog) StoredSteps() int { return len(l.tails) }

// Steps returns how many steps the walks of r's Member calls have taken in all,
// counted as FlattenSteps counts a flattening's, so ir's external tests can
// hold the cost of one call to the composition it reads.
func (r *MemberReader) Steps() int { return r.steps }

// WalkValuesWithin is WalkValues showing each visitor at most limit values.
func WalkValuesWithin(limit int, root any, path string, visitors ...Visitor) bool {
	return walkValues(nil, limit, root, path, visitors).truncated
}

// ForEachGroupWithin is ForEachGroup within limit steps.
func ForEachGroupWithin(limit int, doc *Document, fn func(OperationGroup)) bool {
	return forEachGroup(limit, doc, fn)
}

// ForEachOperationWithin is ForEachOperation within limit steps.
func ForEachOperationWithin(limit int, doc *Document, fn func(Operation)) bool {
	return forEachOperation(limit, doc, fn)
}

// ForEachOperationAtWithin is ForEachOperationAt within limit steps.
func ForEachOperationAtWithin(limit int, doc *Document, fn func(Operation, OperationPlace)) bool {
	return forEachOperationAt(limit, doc, fn)
}

// TypeEdgesWithin is TypeEdges entering at most limit values.
func TypeEdgesWithin(limit int, td TypeDef) iter.Seq[TypeID] {
	return typeEdges(limit, td)
}

// NewEditorWithin is NewEditor with its service tree read within limit steps,
// where an edit's own read takes MaxWalkSteps.
func NewEditorWithin(limit int, doc *Document) *Editor {
	editor := NewEditor(doc)
	editor.tree.index(editor.out.Services, limit)
	return editor
}
