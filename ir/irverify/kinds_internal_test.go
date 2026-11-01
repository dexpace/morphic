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

// TestStreamingPath_ListsEveryCarrier counts the fields of each type on
// streamingPath in the encoded shape, which reaches through every sealed sum, by
// their type and however it is held: a second field is a path checkStreamingModes
// prunes, where an invented mode would pass unread, so it fails here until
// streamingPath takes it.
func TestStreamingPath_ListsEveryCarrier(t *testing.T) {
	t.Parallel()
	shape, err := os.ReadFile("../testdata/shape.golden.txt")
	require.NoError(t, err)
	for typeName, carriers := range map[string]int{
		"StreamingMode":  1,
		"Operation":      1,
		"OperationGroup": 2,
		"Service":        1,
	} {
		field := regexp.MustCompile(`(?m)^\t"[^"]+" \S*\b` + typeName + `\b`)
		assert.Len(t, field.FindAllString(string(shape), -1), carriers, typeName)
	}
}

// TestCheckStreamingModes_SeesOnlyThePathToTheField is the counting seam for the
// check's pruning, which changes no finding: however much a document holds beside
// its operations or inside one, the check is shown the same values.
func TestCheckStreamingModes_SeesOnlyThePathToTheField(t *testing.T) {
	t.Parallel()
	shown := func(doc *ir.Document) int {
		check, count := checkStreamingModes(doc), 0
		ir.WalkValues(doc, "doc", ir.VisitFunc(func(v reflect.Value, at ir.WalkPath) bool {
			count++
			return check.visit(v, at)
		}))
		return count
	}
	op := func(params ...ir.Parameter) *ir.Document {
		return &ir.Document{Services: []ir.Service{{Groups: []ir.OperationGroup{{
			Operations: []ir.Operation{{Params: params}},
		}}}}}
	}

	plain := op()
	richer := op(ir.Parameter{ID: "p/a"}, ir.Parameter{ID: "p/b"})
	richer.Types = ir.TypeRegistry{"t/a": &ir.Primitive{}, "t/b": &ir.Primitive{}}
	assert.Equal(t, shown(plain), shown(richer))
}
