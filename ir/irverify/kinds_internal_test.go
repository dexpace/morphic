// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package irverify

import (
	"os"
	"reflect"
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
)

// TestOperationPath_ListsEveryCarrier counts the fields of each type on
// operationPath, and on inputPath below it, in the encoded shape, which reaches
// through every sealed sum, by their type and however it is held: one more field
// is a path the checks pruning to it skip, where an invented value would pass
// unread, so it fails here until the path takes it.
func TestOperationPath_ListsEveryCarrier(t *testing.T) {
	t.Parallel()
	shape, err := os.ReadFile("../testdata/shape.golden.txt")
	require.NoError(t, err)
	for typeName, carriers := range map[string]int{
		"StreamingMode":   1,
		"Stability":       2,
		"Operation":       1,
		"OperationGroup":  2,
		"Service":         1,
		"InputPath":       3,
		"Pagination":      1,
		"StreamDetail":    2,
		"StreamCondition": 1,
	} {
		field := regexp.MustCompile(`(?m)^\t"[^"]+" \S*\b` + typeName + `\b`)
		assert.Len(t, field.FindAllString(string(shape), -1), carriers, typeName)
	}
}

// TestOperationPathChecks_SeeOnlyThePathToTheField is the counting seam for each
// check pruning to operationPath, which changes no finding: however much a
// document holds beside its operations or inside one, the check is shown the
// same values.
func TestOperationPathChecks_SeeOnlyThePathToTheField(t *testing.T) {
	t.Parallel()
	op := func(params ...ir.Parameter) *ir.Document {
		return &ir.Document{Services: []ir.Service{{Groups: []ir.OperationGroup{{
			Operations: []ir.Operation{{Params: params}},
		}}}}}
	}
	for name, check := range map[string]func(*ir.Document) walkCheck{
		"checkStreamingModes": checkStreamingModes,
		"checkStability":      checkStability,
		"checkInputs":         checkInputs,
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

			plain := op()
			richer := op(ir.Parameter{ID: "p/a"}, ir.Parameter{ID: "p/b"})
			richer.Types = ir.TypeRegistry{"t/a": &ir.Primitive{}, "t/b": &ir.Primitive{}}
			assert.Equal(t, shown(plain), shown(richer))
		})
	}
}
