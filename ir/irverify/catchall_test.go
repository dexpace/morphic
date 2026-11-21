// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package irverify_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
)

// catchAllDoc is a closed document whose one operation takes a form body
// holding catch, a catch-all encoding keyed by the leaf type.
func catchAllDoc(catch map[ir.TypeID]ir.CatchAllParts) *ir.Document {
	doc := closedDoc()
	doc.Services = []ir.Service{{ID: "service/x/S", Name: named("s"), Groups: []ir.OperationGroup{{
		ID: "operation-group/x/S/g", Name: named("g"), Operations: []ir.Operation{{
			ID: "operation/x/S/op", Name: named("op"),
			Request: &ir.Payload{Contents: []ir.Content{{
				MediaType: "multipart/form-data", Type: ir.TypeRef{Target: "type/x/Leaf"}, CatchAllEncoding: catch,
			}}},
		}},
	}}}}
	return stamped(doc)
}

// TestVerify_ACatchAllEncodingStatesSomething holds an entry to stating a value
// or a pattern: one stating neither is a finding at the entry, and one stating
// either is clean.
func TestVerify_ACatchAllEncodingStatesSomething(t *testing.T) {
	t.Parallel()
	value := &ir.PartEncoding{ContentTypes: []string{"text/plain"}}
	for _, tc := range []struct {
		name  string
		parts ir.CatchAllParts
		want  []string
	}{
		{"a value", ir.CatchAllParts{Value: value}, nil},
		{"a pattern", ir.CatchAllParts{Patterns: map[string]ir.PartEncoding{"^f_": {HasFilename: true}}}, nil},
		{"neither", ir.CatchAllParts{}, []string{"ir/catch-all-encoding-empty"}},
		{"an empty pattern map", ir.CatchAllParts{Patterns: map[string]ir.PartEncoding{}}, []string{"ir/catch-all-encoding-empty"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := verifyDeclared(catchAllDoc(map[ir.TypeID]ir.CatchAllParts{"type/x/Leaf": tc.parts}))
			if tc.want == nil {
				assert.Empty(t, got)
				return
			}
			require.Len(t, got, 1)
			assert.Equal(t, tc.want[0], got[0].Code)
			assert.Contains(t, got[0].Path, "CatchAllEncoding[type/x/Leaf]")
		})
	}
}

// TestVerify_ACatchAllEncodingNamesAType holds the key to naming a type: an
// empty one is the empty-reference finding a type position draws.
func TestVerify_ACatchAllEncodingNamesAType(t *testing.T) {
	t.Parallel()
	doc := catchAllDoc(map[ir.TypeID]ir.CatchAllParts{"": {Value: &ir.PartEncoding{HasFilename: true}}})
	assert.Equal(t, []string{"ir/empty-type-ref"}, codesOf(emptyRefViolations(doc)))
}
