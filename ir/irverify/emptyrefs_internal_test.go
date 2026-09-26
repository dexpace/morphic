package irverify

import (
	"go/ast"
	"go/parser"
	"go/token"
	"reflect"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
)

// idClassNames names idTypes' identity classes by their Go type name, so an
// AST walk over field declarations can compare against the same set
// checkEmptyRefs classifies by reflect.Type.
func idClassNames() map[string]bool {
	names := make(map[string]bool, len(idTypes))
	for c := range idTypes {
		names[c.Name()] = true
	}
	return names
}

// followDecls follows expr through decls until it reaches a shape other than
// an identifier: a pointer, slice, map, struct, or a name decls does not
// declare (a builtin, or a type from outside the ir package). An identifier
// naming one of classes is also a stop, not a name to keep resolving: idTypes
// compares a reflect.Type by identity, never by what the named type is
// declared as underneath (every one of the seven is a defined string type),
// so continuing past "TypeID" to "string" would make it indistinguishable
// from any other named string and defeat the very check this mirrors.
//
// depth bounds the walk (the bounded-recursion rule), mirroring
// mentionsInteger in indices_test.go: Go forbids a cycle among type
// declarations except through a pointer, slice or map, so exceeding it means
// the parse went wrong rather than that the IR grew deep.
func followDecls(t *testing.T, decls map[string]ast.Expr, classes map[string]bool, expr ast.Expr, depth int) ast.Expr {
	t.Helper()
	require.Less(t, depth, maxTypeChain, "resolving a field type exceeded %d steps", maxTypeChain)
	ident, isIdent := expr.(*ast.Ident)
	if !isIdent || classes[ident.Name] {
		return expr
	}
	declared, isDeclared := decls[ident.Name]
	if !isDeclared {
		return expr
	}
	return followDecls(t, decls, classes, declared, depth+1)
}

// isIDClassIdent reports whether expr, once resolved, names one of classes
// directly — the check applied to what a pointer, slice element, map key or
// map value resolves to, after unwrapping exactly one level (see
// mentionsIDClass).
func isIDClassIdent(classes map[string]bool, expr ast.Expr) bool {
	ident, isIdent := expr.(*ast.Ident)
	return isIdent && classes[ident.Name]
}

// mentionsIDClass reports whether a field's type expression is, or is a
// pointer/slice/map (key or value) of, one of classes — the shapes idShapeOf
// recognizes once reflection has already collapsed a named type to its
// identity. Only one level of pointer/slice/map is examined, because
// idShapeOf looks no deeper either: its idTypes[t.Elem()] lookup is a direct
// map lookup, not a recursive shape check, so a slice of a pointer to an ID
// class (which nothing in the ir package declares) would not be classified by
// the production code and must not be demanded here.
func mentionsIDClass(t *testing.T, decls map[string]ast.Expr, classes map[string]bool, expr ast.Expr, depth int) bool {
	t.Helper()
	switch e := followDecls(t, decls, classes, expr, depth).(type) {
	case *ast.Ident:
		return classes[e.Name]
	case *ast.StarExpr:
		return isIDClassIdent(classes, followDecls(t, decls, classes, e.X, depth+1))
	case *ast.ArrayType:
		return isIDClassIdent(classes, followDecls(t, decls, classes, e.Elt, depth+1))
	case *ast.MapType:
		return isIDClassIdent(classes, followDecls(t, decls, classes, e.Key, depth+1)) ||
			isIDClassIdent(classes, followDecls(t, decls, classes, e.Value, depth+1))
	default:
		return false // a struct, selector, interface, func: no identity of its own
	}
}

// declaredIDFields returns "Type.Field" for every struct field in the ir
// package's production sources whose type is, or is a pointer/slice/map (key
// or value) of, one of idTypes' identity classes. Modeled on
// declaredIntegerFields in indices_test.go.
func declaredIDFields(t *testing.T) []string {
	t.Helper()
	decls := typeDecls(t)
	classes := idClassNames()
	var names []string
	for _, path := range irSourceFiles(t) {
		f, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.SkipObjectResolution)
		require.NoError(t, err)
		ast.Inspect(f, func(node ast.Node) bool {
			ts, ok := node.(*ast.TypeSpec)
			if !ok {
				return true
			}
			names = append(names, idFieldsDeclaredOn(t, decls, classes, ts)...)
			return true
		})
	}
	return names
}

// idFieldsDeclaredOn returns the identity-bearing fields of one struct
// declaration.
func idFieldsDeclaredOn(t *testing.T, decls map[string]ast.Expr, classes map[string]bool, ts *ast.TypeSpec) []string {
	t.Helper()
	st, ok := ts.Type.(*ast.StructType)
	if !ok {
		return nil
	}
	var names []string
	for _, field := range st.Fields.List {
		if !mentionsIDClass(t, decls, classes, field.Type, 0) {
			continue
		}
		for _, name := range field.Names {
			names = append(names, ts.Name.Name+"."+name.Name)
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

// TestIDTypes_MatchTheIdentityClasses holds idTypes to naming exactly the
// types identityClasses (duplicates_test.go) already calls an identity, so
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
