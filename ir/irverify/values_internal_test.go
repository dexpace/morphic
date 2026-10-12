package irverify

import (
	"go/types"
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
	declared, err := irDecls(t).StringConsts("ValueKind")
	require.NoError(t, err)
	require.NotEmpty(t, declared, "the ir sources must declare ValueKind constants")

	declaredValues := make([]string, 0, len(declared))
	for _, c := range declared {
		declaredValues = append(declaredValues, c.Value)
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

// valueCarriers derives, from the ir package, every struct field that holds an
// ir.Value — a Value, a pointer to one, or a slice, array or map of either —
// named "Type.Field", sorted. Value, Field and CtorValue are left out: they are
// a value's own payload, reached only below a value the walk has already
// found, which TestVerify_NestedStrayPayloadIsReportedAtItsPath covers.
func valueCarriers(t *testing.T) []string {
	t.Helper()
	value, err := irDecls(t).Named("Value")
	require.NoError(t, err)
	payloadTypes := map[string]bool{"Value": true, "Field": true, "CtorValue": true}

	var out []string
	for _, f := range declaredFields(t) {
		if payloadTypes[f.owner] || !holdsValue(t, value, f.field.Type(), 0) {
			continue
		}
		out = append(out, f.key())
	}
	slices.Sort(out)
	return out
}

// holdsValue reports whether a field's type holds an ir.Value: Value itself, or
// a pointer, slice, array or map of one, looking through every named type, so a
// field typed `type Samples []*Value` is a carrier as much as a []*Value field
// is. A struct type is not followed: the Value fields it holds are carriers of
// its own, derived where it is declared. depth bounds the walk, as it does in
// mentionsInteger.
func holdsValue(t *testing.T, value *types.Named, typ types.Type, depth int) bool {
	t.Helper()
	require.Less(t, depth, maxTypeChain, "resolving a field type exceeded %d steps", maxTypeChain)
	if types.Identical(typ, value) {
		return true
	}
	switch u := typ.Underlying().(type) {
	case *types.Pointer:
		return holdsValue(t, value, u.Elem(), depth+1)
	case *types.Slice:
		return holdsValue(t, value, u.Elem(), depth+1)
	case *types.Array:
		return holdsValue(t, value, u.Elem(), depth+1)
	case *types.Map:
		return holdsValue(t, value, u.Elem(), depth+1)
	default:
		return false
	}
}
