// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package ir_test

import (
	"go/types"
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
)

// maxTypeChain bounds the walk through a field's type (the bounded-recursion
// rule). A type may recur through a pointer, slice or map, so a walk that has
// not ended by then is following a cycle, not a deep IR.
const maxTypeChain = 16

// declaredField is one field of a struct type the ir package declares, with
// the type it is declared on.
type declaredField struct {
	owner string
	field *types.Var
}

// key names the field "Type.Field", the spelling idPositions uses.
func (f declaredField) key() string {
	return f.owner + "." + f.field.Name()
}

// declaredFields returns every field of every struct type the ir package
// declares at package level, unexported and embedded ones included. An alias
// of another declaration adds no fields of its own; an alias of a struct
// literal and a defined type over a struct do.
func declaredFields(t *testing.T) []declaredField {
	t.Helper()
	var out []declaredField
	for decl := range irDecls(t).Objects[*types.TypeName]() {
		if named, isNamed := types.Unalias(decl.Type()).(*types.Named); isNamed && named.Obj() != decl {
			continue
		}
		st, isStruct := decl.Type().Underlying().(*types.Struct)
		if !isStruct {
			continue
		}
		for field := range st.Fields() {
			out = append(out, declaredField{owner: decl.Name(), field: field})
		}
	}
	require.NotEmpty(t, out, "the ir package must declare struct fields")
	return out
}

// idClassTypes resolves the classes of identity the empty-reference reading
// classifies by to the types the ir package declares. A class is compared by
// identity, as the reading compares a reflect.Type: resolving TypeID to string
// would defeat the check this mirrors.
func idClassTypes(t *testing.T) map[*types.TypeName]bool {
	t.Helper()
	p := irDecls(t)
	classes := map[*types.TypeName]bool{}
	for _, class := range ir.IDTypes() {
		named, err := p.Named(class.Name())
		require.NoError(t, err, "idTypes names %s, which the ir package must declare", class.Name())
		classes[named.Obj()] = true
	}
	return classes
}

// isIDClass reports whether typ is one of classes. A defined type over a class
// is a type of its own, as it is to reflection, and is not one.
func isIDClass(classes map[*types.TypeName]bool, typ types.Type) bool {
	named, isNamed := types.Unalias(typ).(*types.Named)
	return isNamed && classes[named.Obj()]
}

// mentionsIDClass reports whether a field's type is, or is a pointer, slice or
// map (key or value) of, one of classes: the shapes the reading recognizes. It
// looks one level deep, as the reading does;
// TestIDPositions_EveryShapeIsOneTheReadingReads fails if a field nests deeper.
func mentionsIDClass(classes map[*types.TypeName]bool, typ types.Type) bool {
	if isIDClass(classes, typ) {
		return true
	}
	switch u := typ.Underlying().(type) {
	case *types.Pointer:
		return isIDClass(classes, u.Elem())
	case *types.Slice:
		return isIDClass(classes, u.Elem())
	case *types.Map:
		return isIDClass(classes, u.Key()) || isIDClass(classes, u.Elem())
	default:
		return false // a struct, interface, func: no identity of its own
	}
}

// TestIDPositions_AreAllClassified fails when the ir package declares an
// identity-bearing field idPositions holds no rule for, or when idPositions
// still classifies one ir no longer declares. Its rule is what EmptyReferences
// enforces, so a field missing from it is a position nobody has decided whether
// an empty reference is legitimate at. Each name is reported on its own line.
func TestIDPositions_AreAllClassified(t *testing.T) {
	t.Parallel()
	classes := idClassTypes(t)
	found := map[string]bool{}
	for _, f := range declaredFields(t) {
		if mentionsIDClass(classes, f.field.Type()) {
			found[f.key()] = true
		}
	}
	require.NotEmpty(t, found, "the ir package must declare identity-bearing fields")
	listed := map[string]bool{}
	for _, name := range ir.IDPositions() {
		listed[name] = true
		assert.True(t, found[name], "idPositions classifies ir.%s, which the ir package no longer declares", name)
	}
	for name := range found {
		assert.True(t, listed[name], "ir.%s is an identity-bearing field idPositions does not classify: "+
			"say whether this position may hold an empty reference", name)
	}
}

// TestIDPositions_EveryShapeIsOneTheReadingReads fails when an
// identity-bearing field holds its IDs in a shape the reading does not read:
// nested more than one pointer, slice or map deep, or in a map with an
// identity on both sides, whose values it never reaches. Without it a nested
// field would pass TestIDPositions_AreAllClassified unlisted and be checked by
// nothing.
func TestIDPositions_EveryShapeIsOneTheReadingReads(t *testing.T) {
	t.Parallel()
	classes := idClassTypes(t)
	for _, f := range declaredFields(t) {
		typ := f.field.Type()
		if !mentionsIDClassAnywhere(t, classes, typ, 0) {
			continue
		}
		assert.True(t, mentionsIDClass(classes, typ),
			"ir.%s holds an identity nested deeper than the reading reads", f.key())
		m, isMap := typ.Underlying().(*types.Map)
		assert.False(t, isMap && isIDClass(classes, m.Key()) && isIDClass(classes, m.Elem()),
			"ir.%s maps an identity to an identity, and the reading reads only the key", f.key())
	}
}

// mentionsIDClassAnywhere reports whether a field's type holds one of classes
// at any depth: through pointers, slices and arrays, and both halves of a map.
// A struct is not followed, because the ID fields it declares are positions of
// their own.
func mentionsIDClassAnywhere(t *testing.T, classes map[*types.TypeName]bool, typ types.Type, depth int) bool {
	t.Helper()
	require.Less(t, depth, maxTypeChain, "resolving a field type exceeded %d steps", maxTypeChain)
	if isIDClass(classes, typ) {
		return true
	}
	switch u := typ.Underlying().(type) {
	case *types.Pointer:
		return mentionsIDClassAnywhere(t, classes, u.Elem(), depth+1)
	case *types.Slice:
		return mentionsIDClassAnywhere(t, classes, u.Elem(), depth+1)
	case *types.Array:
		return mentionsIDClassAnywhere(t, classes, u.Elem(), depth+1)
	case *types.Map:
		return mentionsIDClassAnywhere(t, classes, u.Key(), depth+1) ||
			mentionsIDClassAnywhere(t, classes, u.Elem(), depth+1)
	default:
		return false
	}
}

// TestIDTypes_AreTheDeclaredClasses holds idTypes, what the reading classifies
// by, to the classes of ID the IR's nodes declare, which ir derives from their
// types.
func TestIDTypes_AreTheDeclaredClasses(t *testing.T) {
	t.Parallel()
	declared := ir.IDClasses()
	require.NotEmpty(t, declared)
	for class := range declared {
		assert.True(t, ir.IsIDType(class), "idTypes does not list %s, which a node declares", class)
	}
	assert.Len(t, ir.IDTypes(), len(declared), "idTypes lists a class no node declares")
}

// localIDCarrier lives only in this test, so no idPositions entry can name a
// field of it: TestIDPositions_AnUnlistedPositionIsARequiredReference shows what
// the reading does for a position nothing has recorded a decision for.
type localIDCarrier struct {
	Stray ir.TypeID
}

// TestIDPositions_AnUnlistedPositionIsARequiredReference pins the loud default:
// idPositions is a map, so a key it does not hold reads back as the rule's zero
// value, which is the required one rather than one letting an empty reference
// through.
func TestIDPositions_AnUnlistedPositionIsARequiredReference(t *testing.T) {
	t.Parallel()
	assert.Equal(t, []string{"Stray"}, ir.RequiredIDFields(reflect.TypeFor[localIDCarrier]()))

	var found ir.EmptyReferences
	ir.WalkValues(&localIDCarrier{}, "doc", &found)
	got := found.List()
	require.Len(t, got, 1)
	assert.Equal(t, "doc.Stray", got[0].Path)
}
