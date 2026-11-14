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

// idClassTypes resolves the classes of ID the IR's nodes declare, which the
// empty-reference reading classifies by, to the types the ir package declares.
// A class is compared by identity, as the reading compares a reflect.Type:
// resolving TypeID to string would defeat the check this mirrors.
func idClassTypes(t *testing.T) map[*types.TypeName]bool {
	t.Helper()
	p := irDecls(t)
	classes := map[*types.TypeName]bool{}
	for class := range ir.IDClasses() {
		named, err := p.Named(class.Name())
		require.NoError(t, err, "a node declares an ID of %s, which the ir package must declare", class.Name())
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
	found := identityFieldKeys(t)
	require.NotEmpty(t, found, "the ir package must declare identity-bearing fields")
	for _, problem := range classificationProblems(ir.IDPositions(), found,
		"ir.%s is an identity-bearing field idPositions does not classify: "+
			"say whether this position may hold an empty reference") {
		t.Error(problem)
	}
}

// identityFieldKeys returns the "Type.Field" key of every field the ir package
// declares that holds an identity.
func identityFieldKeys(t *testing.T) map[string]bool {
	t.Helper()
	classes := idClassTypes(t)
	found := map[string]bool{}
	for _, f := range declaredFields(t) {
		if mentionsIDClass(classes, f.field.Type()) {
			found[f.key()] = true
		}
	}
	return found
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

// TestIDPositions_AnEmptyFormIsASingleIDs fails when idPositions gives an empty
// form to a field holding its IDs through a pointer, a slice or a map. The rule
// is read only for a field holding one ID: a pointer's none is nil, and an
// element or key of a collection is a reference whatever the field's rule, so
// such an entry would say a field may be empty and change nothing.
func TestIDPositions_AnEmptyFormIsASingleIDs(t *testing.T) {
	t.Parallel()
	classes := idClassTypes(t)
	fields := map[string]types.Type{}
	for _, f := range declaredFields(t) {
		fields[f.key()] = f.field.Type()
	}
	positions := ir.EmptyFormPositions()
	require.NotEmpty(t, positions)
	for _, key := range positions {
		typ, declared := fields[key]
		require.True(t, declared, "idPositions gives ir.%s an empty form, which ir does not declare", key)
		assert.True(t, isIDClass(classes, typ), "idPositions gives ir.%s an empty form, but it holds %s", key, typ)
	}
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

// TestEmptyReference_CodeAndMessageNameTheClass pins the one wording both
// checkers report an empty reference in: the code names the class as RefNoun
// does, and the tag property of a discriminator locating nothing says what it
// lacks is a way to find the tag.
func TestEmptyReference_CodeAndMessageNameTheClass(t *testing.T) {
	t.Parallel()
	server := ir.EmptyReference{Class: reflect.TypeFor[ir.ServerID](), Path: "doc.Services[0].Servers[0]"}
	assert.Equal(t, "ir/empty-server-ref", server.Code())
	assert.Equal(t, "server reference is empty, and this position has no empty form", server.Message())

	untagged := emptyReferencesOf(&ir.Document{Types: ir.TypeRegistry{
		"t/x/Base": &ir.Model{ID: "t/x/Base", Discriminator: &ir.Discriminator{}},
	}})
	require.Len(t, untagged, 1)
	tag := untagged[0]
	assert.Equal(t, "ir/empty-prop-ref", tag.Code())
	assert.Equal(t, "discriminator names no tag property, and neither PropertyName nor Index locates the tag instead",
		tag.Message())
}
