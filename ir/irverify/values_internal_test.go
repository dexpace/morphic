package irverify

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
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
// row. A typo in the first direction is a payload checkValues can never find
// set or missing; a gap in the second is a field no kind selects, so one that
// stray-payload can never catch — silently, on the exact hazard this check
// exists for.
func TestValuePayloads_NameValueFields(t *testing.T) {
	t.Parallel()
	valueType := reflect.TypeFor[ir.Value]()

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
