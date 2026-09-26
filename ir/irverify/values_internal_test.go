package irverify

import (
	"go/ast"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestValuePayloads_CoverTheDeclaredKinds ties valuePayloads to the ir
// sources' own ValueKind const block, the way TestPrimKind_TiesToConstBlock and
// TestAuthKind_TiesToConstBlock (ir/types_test.go, ir/auth_test.go) tie Valid to
// PrimKind's and AuthKind's declarations. valuePayloads has no Valid method
// beside it for those tests' pattern to reuse, so nothing else catches a kind
// added to the enum without saying what it selects, or a stale entry left
// behind for a kind that no longer exists — both directions are asserted in one
// ElementsMatch, since a map has no declaration order for a missing/extra diff
// to preserve.
func TestValuePayloads_CoverTheDeclaredKinds(t *testing.T) {
	t.Parallel()
	declared := declaredConstsOfType(t, "ValueKind")
	require.NotEmpty(t, declared, "the ir sources must declare ValueKind constants")

	declaredValues := make([]string, 0, len(declared))
	for _, c := range declared {
		declaredValues = append(declaredValues, c.value)
	}

	tableValues := make([]string, 0, len(valuePayloads))
	for kind := range valuePayloads {
		tableValues = append(tableValues, string(kind))
	}

	assert.ElementsMatch(t, declaredValues, tableValues,
		"valuePayloads must name exactly the kinds the ir sources declare")
}

// TestValuePayloads_NameValueFields holds the other half of the table to
// ir.Value's own shape: every field name a row names is a real field of
// ir.Value, and every payload field ir.Value declares is named by at least one
// row. Either gap turns a legitimate payload into a violation. A misspelt row
// leaves its kind no real field, so the kind's own payload is reported stray
// and an absent one is never reported missing; a field no row names is
// selected by no kind, so every value that sets it is reported stray.
func TestValuePayloads_NameValueFields(t *testing.T) {
	t.Parallel()
	selected := make(map[string]bool, valueType.NumField())
	for i := range valueType.NumField() {
		name := valueType.Field(i).Name
		if name != "Kind" {
			selected[name] = false
		}
	}

	for kind, rule := range valuePayloads {
		if rule.field == "" {
			continue // ValueNull selects no payload at all.
		}
		_, isField := selected[rule.field]
		require.True(t, isField, "kind %q names %q, which is not a field of ir.Value", kind, rule.field)
		selected[rule.field] = true
	}

	for name, isSelected := range selected {
		assert.True(t, isSelected, "ir.Value field %q is not selected by any declared kind", name)
	}
}

// valueCarriers derives, from the ir sources, every struct field that holds an
// ir.Value — a Value, a pointer to one, or a slice, array or map of either —
// named "Type.Field", sorted. Value, Field and CtorValue are left out: they are
// a value's own payload, reached only below a value the walk has already
// found, which TestVerify_NestedStrayPayloadIsReportedAtItsPath covers.
func valueCarriers(t *testing.T) []string {
	t.Helper()
	payloadTypes := map[string]bool{"Value": true, "Field": true, "CtorValue": true}
	decls := typeDecls(t)
	var out []string
	for name, expr := range decls {
		st, isStruct := expr.(*ast.StructType)
		if !isStruct || payloadTypes[name] {
			continue
		}
		for _, f := range st.Fields.List {
			if !holdsValue(t, decls, f.Type, 0) {
				continue
			}
			require.NotEmpty(t, f.Names, "%s embeds a Value; the carrier has no field name to key", name)
			for _, n := range f.Names {
				out = append(out, name+"."+n.Name)
			}
		}
	}
	slices.Sort(out)
	return out
}

// holdsValue reports whether a field's type holds an ir.Value: Value itself,
// or a pointer, slice, array or map of one, looking through the ir package's
// own declarations as mentionsInteger does, so a field typed `type Samples
// []*Value` is a carrier as much as a []*Value field is. A struct declaration
// is not followed: the Value fields it holds are carriers of its own, derived
// where it is declared. depth bounds the walk through declarations, as it does
// there.
func holdsValue(t *testing.T, decls map[string]ast.Expr, expr ast.Expr, depth int) bool {
	t.Helper()
	require.Less(t, depth, maxTypeChain, "resolving a field type exceeded %d steps", maxTypeChain)
	switch e := expr.(type) {
	case *ast.Ident:
		if e.Name == "Value" {
			return true
		}
		declared, isDeclared := decls[e.Name]
		if !isDeclared {
			return false // the chain ended at a builtin
		}
		if _, isStruct := declared.(*ast.StructType); isStruct {
			return false
		}
		return holdsValue(t, decls, declared, depth+1)
	case *ast.StarExpr:
		return holdsValue(t, decls, e.X, depth+1)
	case *ast.ArrayType:
		return holdsValue(t, decls, e.Elt, depth+1)
	case *ast.MapType:
		return holdsValue(t, decls, e.Value, depth+1)
	default:
		return false
	}
}
