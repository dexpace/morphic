package irtest_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/ir/irtest"
)

func model(id string) *ir.Model { return &ir.Model{ID: ir.TypeID(id)} }

// TestSpacesUsed_ReadsEveryKindSortedWithoutRepeats holds the result to the form
// Document.IDSpaces takes: each kind's namespaces sorted, and each once however
// many IDs live in it. The kinds are spread over a registry, a map and nested
// nodes so that no one way of reaching an ID stands in for the rest, and the
// services are declared out of order because walk order is not sorted order.
func TestSpacesUsed_ReadsEveryKindSortedWithoutRepeats(t *testing.T) {
	t.Parallel()
	doc := &ir.Document{
		Types: ir.TypeRegistry{"t/b/x": model("t/b/x"), "t/a/x": model("t/a/x"), "t/a/y": model("t/a/y")},
		Auth:  map[ir.AuthID]ir.AuthScheme{"auth/k/x": {ID: "auth/k/x"}},
		Services: []ir.Service{
			{
				ID: "s/svc/x",
				Groups: []ir.OperationGroup{{
					ID:         "g/tags/x",
					Operations: []ir.Operation{{ID: "op/paths/x"}, {ID: "op/paths/y"}},
				}},
			},
			{ID: "s/api/x"},
		},
	}
	want := map[string][]string{
		ir.IDKindType: {"a", "b"}, ir.IDKindAuth: {"k"}, ir.IDKindService: {"api", "svc"},
		ir.IDKindGroup: {"tags"}, ir.IDKindOp: {"paths"},
	}
	assert.Empty(t, cmp.Diff(want, irtest.SpacesUsed(doc)), "(-want +got)")
}

// TestSpacesUsed_LeavesOutWhatNeedsNoDeclaration pins the three kinds of ID that
// add nothing: the primitive namespace, which ir owns, an ID too malformed to
// have a namespace, and an ID of no kind. A kind none of whose IDs count is left
// out altogether, and a document with no IDs gets nil.
func TestSpacesUsed_LeavesOutWhatNeedsNoDeclaration(t *testing.T) {
	t.Parallel()
	prim := &ir.Primitive{ID: ir.PrimTypeID(ir.PrimString), Prim: ir.PrimString}
	tests := []struct {
		name string
		doc  *ir.Document
		want map[string][]string
	}{
		{"a nil document", nil, nil},
		{"a document with no IDs", &ir.Document{}, nil},
		{"only the primitive namespace", &ir.Document{Types: ir.TypeRegistry{prim.ID: prim}}, nil},
		{"the primitive namespace beside another", &ir.Document{Types: ir.TypeRegistry{prim.ID: prim, "t/a/x": model("t/a/x")}},
			map[string][]string{ir.IDKindType: {"a"}}},
		{"the primitive namespace of another kind", &ir.Document{Services: []ir.Service{{ID: "s/prim/x"}}},
			map[string][]string{ir.IDKindService: {ir.IDSpacePrim}}},
		{"a malformed ID", &ir.Document{Types: ir.TypeRegistry{"t//x": model("t//x")}}, nil},
		{"an ID with no separator", &ir.Document{Types: ir.TypeRegistry{"tx": model("tx")}}, nil},
		{"an ID of no kind", &ir.Document{Types: ir.TypeRegistry{"z/a/x": model("z/a/x")}}, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Empty(t, cmp.Diff(tc.want, irtest.SpacesUsed(tc.doc)), "(-want +got)")
		})
	}
}
