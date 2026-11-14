// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package irverify

import (
	"go/types"
	"os"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
)

// declaredEnums returns the name of every string type the ir package declares
// that has a constant of its own type, sorted: the wire enums and every other
// string whose values a source can only name from a list.
func declaredEnums(t *testing.T) []string {
	t.Helper()
	found := map[string]bool{}
	for c := range irDecls(t).Objects[*types.Const]() {
		named, isNamed := types.Unalias(c.Type()).(*types.Named)
		if !isNamed || named.Obj().Pkg() == nil || named.Obj().Pkg().Path() != irPath {
			continue
		}
		basic, isBasic := named.Underlying().(*types.Basic)
		if isBasic && basic.Info()&types.IsString != 0 {
			found[named.Obj().Name()] = true
		}
	}
	return slices.Sorted(func(yield func(string) bool) {
		for name := range found {
			if !yield(name) {
				return
			}
		}
	})
}

// tableKeys returns the keys of table, a map keyed by a type name or by a
// reflect.Type, as the names of the types they stand for.
func tableKeys(t *testing.T, table any) map[string]bool {
	t.Helper()
	v := reflect.ValueOf(table)
	require.Equal(t, reflect.Map, v.Kind(), "a table of enums is a map")
	keys := map[string]bool{}
	for _, key := range v.MapKeys() {
		switch k := key.Interface().(type) {
		case string:
			keys[k] = true
		case reflect.Type:
			keys[k.Name()] = true
		default:
			require.Failf(t, "unexpected key", "a table of enums is keyed by a name or a type, not %T", k)
		}
	}
	return keys
}

// TestEnums_EveryDeclaredEnumIsHeld holds heldEnums and enumExceptions to the
// string enums the ir package declares: each is held to its set or excepted
// with a reason, never both, and neither table names a type ir does not
// declare. A type added to ir without a decision fails here until it has one.
func TestEnums_EveryDeclaredEnumIsHeld(t *testing.T) {
	t.Parallel()
	held := tableKeys(t, heldEnums)
	excepted := tableKeys(t, enumExceptions)
	declared := declaredEnums(t)
	require.NotEmpty(t, declared, "the ir package must declare string enums")

	for _, name := range declared {
		assert.True(t, held[name] || excepted[name],
			"ir.%s is a string enum neither heldEnums nor enumExceptions decides: hold it to its set or say why not", name)
		assert.False(t, held[name] && excepted[name], "ir.%s is both held and excepted", name)
	}
	for name := range held {
		assert.Contains(t, declared, name, "heldEnums holds %s, which ir does not declare", name)
	}
	for name := range excepted {
		assert.Contains(t, declared, name, "enumExceptions excepts %s, which ir does not declare", name)
	}
}

// TestEnums_TheWireEnumsAreHeld names the seven enums of the documents that
// irverify did not hold, so that dropping one from heldEnums for an exception
// reddens here and not only in the table's own pin.
func TestEnums_TheWireEnumsAreHeld(t *testing.T) {
	t.Parallel()
	held := tableKeys(t, heldEnums)
	for _, name := range []string{
		"AdditionalMode", "PresenceKind", "HTTPLocation", "IdempotencyKind", "MsgDirection", "PageStrategy", "Severity",
		"PrimKind", "AuthKind", "UnmodeledReason", "ValueKind", "StreamingMode", "Stability", "HTTPInbound", "PageStop",
		"LinkValueKind",
	} {
		assert.True(t, held[name], "%s is a wire enum the check holds", name)
	}
	excepted := tableKeys(t, enumExceptions)
	for _, name := range []string{"TypeKind", "Undistributed", "treeClass"} {
		assert.True(t, excepted[name], "%s is excepted, with its reason", name)
	}
}

// TestEnumPath_ListsEveryCarrier counts the fields of each type checkEnums
// reads on its way to a value, and of each enum it holds, in the encoded shape,
// which reaches through every sealed sum, by their type and however it is held:
// one more field is a path the check prunes away from, so it fails here until
// enumPath takes it.
func TestEnumPath_ListsEveryCarrier(t *testing.T) {
	t.Parallel()
	shape, err := os.ReadFile("../testdata/shape.golden.txt")
	require.NoError(t, err)
	for typeName, carriers := range map[string]int{
		"AdditionalMode":   1,
		"PresenceKind":     1,
		"HTTPLocation":     1,
		"IdempotencyKind":  1,
		"MsgDirection":     1,
		"PageStrategy":     1,
		"Severity":         1,
		"HTTPParamBinding": 1,
		"MessageBinding":   1,
		"Pagination":       1,
		"Idempotency":      1,
		"Diagnostic":       1,
		"OpBindings":       1,
		"HTTPBinding":      1,
	} {
		field := regexp.MustCompile(`(?m)^\t"[^"]+" \S*\b` + typeName + `\b`)
		assert.Len(t, field.FindAllString(string(shape), -1), carriers, typeName)
	}
}

// TestPrunedChecks_SeeOnlyTheirPathWhateverTheDocumentHolds gives each check
// that prunes to a path a document of one parameter and one of a hundred, beside
// an operation whose fields each carry a value the check reads: it is shown the
// same number of values in both. A check returning true for a value of another
// type stays in the walk beneath every one of them, and sees the difference.
func TestPrunedChecks_SeeOnlyTheirPathWhateverTheDocumentHolds(t *testing.T) {
	t.Parallel()
	build := func(params int) *ir.Document {
		op := ir.Operation{
			Idempotency: ir.Idempotency{Kind: ir.IdempotencySafe},
			Pagination:  &ir.Pagination{Strategy: ir.PageStrategyCursor},
			Bindings: ir.OpBindings{HTTP: []ir.HTTPBinding{{
				Method: "GET", ParamBindings: []ir.HTTPParamBinding{{Location: ir.HTTPLocationQuery}},
			}}},
		}
		for i := range params {
			op.Params = append(op.Params, ir.Parameter{ID: ir.ParamID("param/x/o/" + strconv.Itoa(i))})
		}
		return &ir.Document{
			Diagnostics: []ir.Diagnostic{{Severity: ir.SeverityInfo}},
			Services:    []ir.Service{{Groups: []ir.OperationGroup{{Operations: []ir.Operation{op}}}}},
		}
	}
	for name, check := range map[string]func(*ir.Document) walkCheck{
		"checkIdempotency":   checkIdempotency,
		"checkWebhookRoutes": checkWebhookRoutes,
		"checkEnums":         checkEnums,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			shown := func(doc *ir.Document) int {
				visitor, count := check(doc), 0
				ir.WalkValues(doc, "doc", ir.VisitFunc(func(v reflect.Value, at ir.WalkPath) bool {
					count++
					return visitor.visit(v, at)
				}))
				return count
			}
			assert.Equal(t, shown(build(1)), shown(build(100)), "the check reads the same values for any number of parameters")
		})
	}
}
