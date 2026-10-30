// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package irverify

import (
	"go/types"
	"reflect"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/ir/irtest"
)

// idClassNames names idTypes' identity classes by their Go type name, so the
// type-checked ir declarations can be compared against the same set
// checkEmptyRefs classifies by reflect.Type.
func idClassNames() map[string]bool {
	names := make(map[string]bool, len(idTypes))
	for c := range idTypes {
		names[c.Name()] = true
	}
	return names
}

// idClassTypes resolves idClassNames to the types the ir package declares. A
// class is compared by identity, as idTypes compares a reflect.Type: resolving
// TypeID to string would defeat the check this mirrors.
func idClassTypes(t *testing.T) map[*types.TypeName]bool {
	t.Helper()
	p := irDecls(t)
	classes := map[*types.TypeName]bool{}
	for name := range idClassNames() {
		named, err := p.Named(name)
		require.NoError(t, err, "idTypes names %s, which the ir package must declare", name)
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

// mentionsIDClass reports whether a field's type is, or is a pointer/slice/map
// (key or value) of, one of classes: the shapes idShapeOf recognizes. Only one
// level of pointer/slice/map is examined, because idShapeOf looks no deeper
// either: its idTypes[t.Elem()] lookup is a direct map lookup, not a recursive
// shape check, so a slice of a pointer to an ID class (which nothing in the ir
// package declares) would not be classified by the production code and must not
// be demanded here. An array is no shape idShapeOf reads, so it is not one here.
// TestIDPositions_EveryShapeIsOneIdShapeOfReads fails instead if one is ever
// declared.
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

// declaredIDFields returns "Type.Field" for every struct field in the ir package
// whose type is, or is a pointer/slice/map (key or value) of, one of idTypes'
// identity classes. Modeled on declaredIntegerFields in indices_internal_test.go.
func declaredIDFields(t *testing.T) []string {
	t.Helper()
	classes := idClassTypes(t)
	var names []string
	for _, f := range declaredFields(t) {
		if mentionsIDClass(classes, f.field.Type()) {
			names = append(names, f.key())
		}
	}
	return names
}

// TestIDPositions_AreAllClassified fails when the ir package declares an
// identity-bearing field idPositions holds no rule for, or when idPositions
// still classifies one ir no longer declares. idPositions' rule is what
// appendEmptyRefs enforces, so a field missing from it is a position nobody
// has decided whether an empty reference is legitimate at.
func TestIDPositions_AreAllClassified(t *testing.T) {
	t.Parallel()
	found := declaredIDFields(t)
	require.NotEmpty(t, found, "the ir package must declare identity-bearing fields")
	for _, name := range found {
		assert.Contains(t, idPositions, name,
			"ir.%s is an identity-bearing field idPositions does not classify: "+
				"say whether this position may hold an empty reference", name)
	}
	assert.Len(t, idPositions, len(found),
		"idPositions classifies a field the ir package no longer declares")
}

// TestIDPositions_EveryShapeIsOneIdShapeOfReads fails when an identity-bearing
// field holds its IDs in a shape idShapeOf does not read: nested more than one
// pointer, slice or map deep, or in a map with an identity on both sides, whose
// values idShapeOf never reaches. mentionsIDClass looks exactly as deep as
// idShapeOf does, so without this a nested field would pass
// TestIDPositions_AreAllClassified unlisted and be checked by nothing, and a
// map with an identity on both sides would pass it once listed, its values
// never checked.
func TestIDPositions_EveryShapeIsOneIdShapeOfReads(t *testing.T) {
	t.Parallel()
	classes := idClassTypes(t)
	for _, f := range declaredFields(t) {
		typ := f.field.Type()
		if !mentionsIDClassAnywhere(t, classes, typ, 0) {
			continue
		}
		assert.True(t, mentionsIDClass(classes, typ),
			"ir.%s holds an identity nested deeper than idShapeOf reads", f.key())
		assert.False(t, identityOnBothSides(classes, typ),
			"ir.%s maps an identity to an identity, and idShapeOf reads only the key", f.key())
	}
}

// mentionsIDClassAnywhere reports whether a field's type holds one of classes
// at any depth: through pointers, slices and arrays, both halves of a map, and
// every named type, as mentionsInteger does. A struct is not followed, because
// the ID fields it declares are positions of their own.
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

// identityOnBothSides reports whether typ is a map whose key and value are
// both one of classes.
func identityOnBothSides(classes map[*types.TypeName]bool, typ types.Type) bool {
	m, isMap := typ.Underlying().(*types.Map)
	return isMap && isIDClass(classes, m.Key()) && isIDClass(classes, m.Elem())
}

// TestIDTypes_MatchTheIdentityClasses holds idTypes to naming exactly the
// types identityClasses (duplicates_internal_test.go) already calls an identity, so
// this package's second table of reference classes cannot silently drift from
// the first: idTypes is what checkEmptyRefs classifies by, identityClasses is
// what the rest of the walk-based checks were audited against.
func TestIDTypes_MatchTheIdentityClasses(t *testing.T) {
	t.Parallel()
	var want []string
	for name, desc := range identityClasses {
		if strings.HasPrefix(desc, "identity") {
			want = append(want, name)
		}
	}
	require.NotEmpty(t, want, "identityClasses must classify at least one type as an identity")

	got := idClassNames()
	for _, name := range want {
		assert.Contains(t, got, name, "idTypes does not list %s, which identityClasses calls an identity", name)
	}
	assert.Len(t, got, len(want), "idTypes lists a type identityClasses does not call an identity")
}

// localIDCarrier lives only in this test, so no idPositions entry can name a
// field of it. That is the point: TestIDFieldsOf_UnlistedPositionDefaultsToRequired
// proves what idFieldsOf does for a position nothing has recorded a decision
// for, not merely that idPositions covers everything ir declares today.
type localIDCarrier struct {
	Stray ir.TypeID
}

// TestIDFieldsOf_UnlistedPositionDefaultsToRequired pins the loud default:
// idPositions is a map, so a key it does not hold reads back as idRule's zero
// value, and idRequired is defined first so that value is idRequired rather
// than a rule that lets an empty reference through unreported.
func TestIDFieldsOf_UnlistedPositionDefaultsToRequired(t *testing.T) {
	t.Parallel()
	fields := idFieldsOf(reflect.TypeFor[localIDCarrier]())
	require.Len(t, fields, 1, "an ID-typed field must be classified even with no idPositions entry")
	assert.Equal(t, idRequired, fields[0].rule,
		"a position idPositions does not list must default to idRequired, the map's zero value")
}

// TestCheckEmptyRefs_VisitingCostsNoAllocation holds the check's cost for a
// map of references none of which is empty at nothing once it has met the
// map's type: it reads values into what it keeps, where reflect.MapIter.Value
// allocates a copy of each. It is not run in parallel: AllocsPerRun measures
// the process.
func TestCheckEmptyRefs_VisitingCostsNoAllocation(t *testing.T) {
	doc := &ir.Document{Types: ir.TypeRegistry{
		"t/pet": &ir.Model{ID: "t/pet", Discriminator: &ir.Discriminator{
			PropertyName: "kind",
			Mapping:      map[string]ir.TypeID{"cat": "t/cat", "dog": "t/dog", "fox": "t/fox"},
		}},
		"t/zoo": &ir.Model{ID: "t/zoo", Discriminator: &ir.Discriminator{
			PropertyName: "kind",
			Mapping:      map[string]ir.TypeID{"pet": "t/pet"},
		}},
	}}
	check := checkEmptyRefs(doc)
	assert.Empty(t, irtest.AllocatingVisits(doc, check))
	assert.Empty(t, check.report(declarations{}), "no reference is empty")
}
