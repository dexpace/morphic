package report_test

import (
	"encoding/json/jsontext"
	"fmt"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/engine/report"
	"github.com/dexpace/morphic/ir"
)

// model returns a model node at pointer in source 0.
func model(id ir.TypeID, pointer jsontext.Pointer) *ir.Model {
	return &ir.Model{ID: id, Provenance: ir.Provenance{Pointer: pointer}}
}

// registry returns a registry of the given nodes, keyed by their IDs.
func registry(nodes ...ir.TypeDef) ir.TypeRegistry {
	types := make(ir.TypeRegistry, len(nodes))
	for _, n := range nodes {
		types[n.Common().ID] = n
	}
	return types
}

// interned returns the Interned entry of a model at pointer.
func interned(pointer jsontext.Pointer, id ir.TypeID) report.Interned {
	return report.Interned{Pointer: pointer, ID: id, Kind: ir.KindModel}
}

// sameExplanation compares explanations, an absent list equal to an empty one:
// the renderer reads only their lengths.
func sameExplanation(want, got report.Explanation) string {
	return cmp.Diff(want, got, cmpopts.EquateEmpty())
}

// TestExplain_SelectsTheNodeAndWhatIsBelowIt is the table over the selection
// rules. A miss at a coordinate lists what interned beneath it, which is the
// difference between "nothing happened here" and "the node moved"; below means
// beneath a pointer token, not sharing a prefix of text; a trailing slash is a
// token of its own, the member keyed "", so only that member's subtree is
// beneath it.
func TestExplain_SelectsTheNodeAndWhatIsBelowIt(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		doc     *ir.Document
		pointer jsontext.Pointer
		want    report.Explanation
	}{
		{
			// The IDs sort in the opposite order to their pointers, so listing by
			// pointer is distinguishable from listing in ID order.
			name: "a miss lists what interned below, in pointer order",
			doc: &ir.Document{Types: registry(
				model("t/aaa", "/components/schemas/Zed"),
				model("t/zzz", "/components/schemas/Alpha"),
				// Shares the query's text but not its path boundary.
				model("t/other", "/components/schemasOther/X"),
			)},
			pointer: "/components/schemas",
			want: report.Explanation{
				Pointer: "/components/schemas",
				Below: []report.Interned{
					interned("/components/schemas/Alpha", "t/zzz"),
					interned("/components/schemas/Zed", "t/aaa"),
				},
			},
		},
		{
			name:    "a miss with nothing below lists nothing",
			doc:     &ir.Document{Types: ir.TypeRegistry{}},
			pointer: "/nowhere",
			want:    report.Explanation{Pointer: "/nowhere"},
		},
		{
			name:    "a node at the coordinate and one beneath it",
			doc:     &ir.Document{Types: registry(model("t/a", "/a"), model("t/b", "/a/b"), model("t/c", "/c"))},
			pointer: "/a",
			want: report.Explanation{
				Pointer: "/a",
				Node:    &report.Interned{Pointer: "/a", ID: "t/a", Kind: ir.KindModel},
				Below:   []report.Interned{interned("/a/b", "t/b")},
			},
		},
		{
			name: "a nil registry entry is skipped, and the real one is found",
			doc: &ir.Document{Types: ir.TypeRegistry{
				"t/nil": nil, "t/real": model("t/real", "/p"),
			}},
			pointer: "/p",
			want: report.Explanation{
				Pointer: "/p",
				Node:    &report.Interned{Pointer: "/p", ID: "t/real", Kind: ir.KindModel},
			},
		},
		{
			name: "a typed nil registry entry is skipped too",
			doc: &ir.Document{Types: ir.TypeRegistry{
				"t/nil": (*ir.Model)(nil), "t/real": model("t/real", "/p"),
			}},
			pointer: "/p",
			want: report.Explanation{
				Pointer: "/p",
				Node:    &report.Interned{Pointer: "/p", ID: "t/real", Kind: ir.KindModel},
			},
		},
		{
			name: "a nil registry entry is skipped, and the real one is below the root",
			doc: &ir.Document{Types: ir.TypeRegistry{
				"t/nil": nil, "t/real": model("t/real", "/p"),
			}},
			pointer: "",
			want:    report.Explanation{Below: []report.Interned{interned("/p", "t/real")}},
		},
		{
			name: "a trailing slash is a token of its own",
			doc: &ir.Document{Types: registry(
				model("t/empty", "/components/schemas/"),
				model("t/emptyprop", "/components/schemas//properties/x"),
				model("t/otherprop", "/components/schemas/User/properties/y"),
			)},
			pointer: "/components/schemas/",
			want: report.Explanation{
				Pointer: "/components/schemas/",
				Node:    &report.Interned{Pointer: "/components/schemas/", ID: "t/empty", Kind: ir.KindModel},
				Below:   []report.Interned{interned("/components/schemas//properties/x", "t/emptyprop")},
			},
		},
		{
			name:    "a nil document has no types",
			doc:     nil,
			pointer: "/p",
			want:    report.Explanation{Pointer: "/p"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := report.Explain(tt.doc, nil, tt.pointer)

			assert.Empty(t, sameExplanation(tt.want, got))
		})
	}
}

// TestExplain_ReportsEachNodesKind pins that the kind reported for a node, the
// one at the coordinate and those below it, is that node's own. Every other case
// here uses models, which a fixed kind would also pass.
func TestExplain_ReportsEachNodesKind(t *testing.T) {
	t.Parallel()
	doc := &ir.Document{Types: registry(
		&ir.Scalar{ID: "t/s", Provenance: ir.Provenance{Pointer: "/k"}},
		&ir.Enum{ID: "t/e", Provenance: ir.Provenance{Pointer: "/k/e"}},
	)}

	got := report.Explain(doc, nil, "/k")

	want := report.Explanation{
		Pointer: "/k",
		Node:    &report.Interned{Pointer: "/k", ID: "t/s", Kind: ir.KindScalar},
		Below:   []report.Interned{{Pointer: "/k/e", ID: "t/e", Kind: ir.KindEnum}},
	}
	assert.Empty(t, sameExplanation(want, got))
}

// TestExplain_AnEmptyPointerIsNotAlwaysTheRoot pins that the root query matches
// a node by where its provenance locates it, not by its Pointer being empty. A
// shared primitive on NoSource and a node located by position both record an
// empty Pointer without being at the whole document. Each decoy's ID sorts
// ahead of t/root's, so a match on the Pointer alone names the decoy. No
// committed spec lowers a type node at the root, so only a hand-built document
// holds one.
func TestExplain_AnEmptyPointerIsNotAlwaysTheRoot(t *testing.T) {
	t.Parallel()
	atRoot := &ir.Model{ID: "t/root", Provenance: ir.Provenance{Source: 0}}
	decoys := map[string]ir.TypeDef{
		"shared primitive": &ir.Primitive{
			ID: "t/prim/string", Prim: ir.PrimString, Provenance: ir.Provenance{Source: ir.NoSource},
		},
		"node located by position": &ir.Model{
			ID: "t/positioned", Provenance: ir.Provenance{Source: 0, Position: ir.Position{Line: 3, Column: 1}},
		},
	}
	for name, decoy := range decoys {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			doc := &ir.Document{Types: registry(decoy, atRoot)}

			got := report.Explain(doc, nil, "")

			want := report.Explanation{Node: &report.Interned{ID: "t/root", Kind: ir.KindModel}}
			assert.Empty(t, sameExplanation(want, got), "the root names the node declared there, never the decoy")
		})
	}
}

// sharedPointer is the coordinate of a oneOf branch the compiler lowers twice,
// as an anonymous scalar and as the composed model that distributes it
// (testdata/conformance/openapi/allof-oneof-cooccurrence.yaml, at
// /components/schemas/Combo/oneOf/0): two nodes, one coordinate.
const sharedPointer = "/components/schemas/Combo/oneOf/0"

// TestExplain_ASharedPointerNamesTheLeastID pins which node an explanation names
// when several sit at one coordinate: the first in ID order, every time. Each
// round builds a fresh registry, so each draws its own map iteration order, and
// an unsorted walk would have to name the least ID by luck in all thirty-two.
func TestExplain_ASharedPointerNamesTheLeastID(t *testing.T) {
	t.Parallel()
	const nodes, rounds = 16, 32
	for round := range rounds {
		types := make(ir.TypeRegistry, nodes)
		for i := range nodes {
			id := ir.TypeID(fmt.Sprintf("t/n/%02d", i))
			types[id] = model(id, sharedPointer)
		}

		got := report.Explain(&ir.Document{Types: types}, nil, sharedPointer)

		require.NotNil(t, got.Node, "round %d: the coordinate holds %d nodes", round, nodes)
		require.Equal(t, ir.TypeID("t/n/00"), got.Node.ID, "round %d", round)
		require.Empty(t, got.Below, "round %d: nodes sharing the coordinate are not beneath it", round)
	}
}

// TestExplain_TiedPointersListInIDOrder pins the order of nodes that share a
// coordinate below the query: the registry's ID order, which is also how the
// node at a coordinate is chosen. A sort that is not stable leaves them in an
// order that depends on the walk, and is stable by accident below thirteen
// elements, so eight pointers of eight nodes each make a scrambled answer all
// but certain.
func TestExplain_TiedPointersListInIDOrder(t *testing.T) {
	t.Parallel()
	const pointers, perPointer = 8, 8
	types := make(ir.TypeRegistry, pointers*perPointer)
	for n := range perPointer {
		for p := range pointers {
			id := ir.TypeID(fmt.Sprintf("t/%d/%d", n, p))
			types[id] = model(id, jsontext.Pointer(fmt.Sprintf("/components/schemas/P%d", p)))
		}
	}

	got := report.Explain(&ir.Document{Types: types}, nil, "/components/schemas").Below

	require.Len(t, got, pointers*perPointer)
	for i := 1; i < len(got); i++ {
		assert.LessOrEqual(t, got[i-1].Pointer, got[i].Pointer, "coordinates are in pointer order")
		if got[i-1].Pointer == got[i].Pointer {
			assert.Less(t, got[i-1].ID, got[i].ID, "%s lists its nodes out of ID order", got[i].Pointer)
		}
	}
}

// TestExplain_SelectsOnlyTheCoordinatesDiagnostics pins which diagnostics an
// explanation lists: those stamped at the pointer, in the order they were
// reported. A finding of a pass sits on NoSource, in no source at all, so it
// is at no coordinate even when it names a pointer.
func TestExplain_SelectsOnlyTheCoordinatesDiagnostics(t *testing.T) {
	t.Parallel()
	diags := []ir.Diagnostic{
		diag("a", ir.Provenance{Pointer: "/p"}),
		diag("b", ir.Provenance{Pointer: "/q"}),
		diag("pass", ir.Provenance{Source: ir.NoSource, Pointer: "/p", Node: "op/x"}),
		diag("c", ir.Provenance{Pointer: "/p"}),
	}

	got := report.Explain(nil, diags, "/p")

	assert.Empty(t, cmp.Diff([]ir.Diagnostic{diags[0], diags[3]}, got.Diagnostics))
}

// TestExplain_RootExcludesPositionAndNodeLocatedFindings pins what listing under
// the root pointer means now that Position and Node exist beside it. A finding
// located by position, or by a pass's node on NoSource, also carries an empty
// Pointer, but neither is about the whole document: listing them under "" would
// put every such finding in the compile at the root. A node beside a real source
// locates nothing inside it, so that finding is about the source as a whole and
// is listed, as the CLI prints it bare.
func TestExplain_RootExcludesPositionAndNodeLocatedFindings(t *testing.T) {
	t.Parallel()
	byPosition := diag("p", ir.Provenance{Source: 0, Position: ir.Position{Line: 5, Column: 1}})
	byNode := diag("n", ir.Provenance{Source: ir.NoSource, Node: "op/x"})
	besideSource := diag("s", ir.Provenance{Source: 0, Node: "op/x"})
	atRoot := diag("r", ir.Provenance{Source: 0})

	got := report.Explain(nil, []ir.Diagnostic{byPosition, byNode, besideSource, atRoot}, "")

	assert.Empty(t, cmp.Diff([]ir.Diagnostic{besideSource, atRoot}, got.Diagnostics),
		"only findings about the source as a whole are at the root")
}

// fullExplanation is an explanation with every section populated, and the text
// WriteExplanation renders it as, one entry per line.
func fullExplanation() (report.Explanation, []string) {
	e := report.Explanation{
		Pointer: "/components/schemas/S",
		Node:    &report.Interned{Pointer: "/components/schemas/S", ID: "t/s", Kind: ir.KindModel},
		Below: []report.Interned{
			{Pointer: "/components/schemas/S/properties/a", ID: "t/a", Kind: ir.KindScalar},
			{Pointer: "/components/schemas/S/properties/b", ID: "t/b", Kind: ir.KindModel},
		},
		Diagnostics: []ir.Diagnostic{
			{Severity: ir.SeverityWarning, Code: "openapi/w", Message: "careful"},
			{Severity: ir.SeverityError, Code: "openapi/e", Message: "broken"},
		},
	}
	lines := []string{
		"coordinate /components/schemas/S\n",
		"  node t/s (model)\n",
		"  interned below it (2):\n",
		"    /components/schemas/S/properties/a -> t/a (scalar)\n",
		"    /components/schemas/S/properties/b -> t/b (model)\n",
		"  diagnostics (2):\n",
		"    warning openapi/w: careful\n",
		"    error openapi/e: broken\n",
	}
	return e, lines
}

// TestWriteExplanation_RendersEachShape pins the exact text for each shape an
// explanation takes: every section present; the root, whose pointer printed bare
// would read as a missing argument; a miss, which says so rather than printing
// nothing; a node with nothing below it, which prints no empty section; and the
// diagnostics line, written even for none.
func TestWriteExplanation_RendersEachShape(t *testing.T) {
	t.Parallel()
	full, fullLines := fullExplanation()
	tests := []struct {
		name string
		e    report.Explanation
		want string
	}{
		{"every section", full, strings.Join(fullLines, "")},
		{
			"the root with nothing interned there",
			report.Explanation{},
			"coordinate \"\" (the whole document)\n" +
				"  no type node was interned at this coordinate\n" +
				"  diagnostics (0):\n",
		},
		{
			"the root with a node",
			report.Explanation{Node: &report.Interned{ID: "t/root", Kind: ir.KindModel}},
			"coordinate \"\" (the whole document)\n" +
				"  node t/root (model)\n" +
				"  diagnostics (0):\n",
		},
		{
			"a miss with something below it",
			report.Explanation{
				Pointer: "/c",
				Below:   []report.Interned{{Pointer: "/c/x", ID: "t/x", Kind: ir.KindEnum}},
			},
			"coordinate /c\n" +
				"  no type node was interned at this coordinate\n" +
				"  interned below it (1):\n" +
				"    /c/x -> t/x (enum)\n" +
				"  diagnostics (0):\n",
		},
		{
			"a node with nothing below it and a diagnostic",
			report.Explanation{
				Pointer:     "/c",
				Node:        &report.Interned{Pointer: "/c", ID: "t/c", Kind: ir.KindPrimitive},
				Diagnostics: []ir.Diagnostic{{Severity: ir.SeverityInfo, Code: "x/i", Message: "fyi"}},
			},
			"coordinate /c\n" +
				"  node t/c (primitive)\n" +
				"  diagnostics (1):\n" +
				"    info x/i: fyi\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var w strings.Builder

			err := report.WriteExplanation(&w, tt.e)

			require.NoError(t, err)
			assert.Empty(t, cmp.Diff(tt.want, w.String()))
		})
	}
}

// TestWriteExplanation_StopsAtFirstFailingWrite pins that a destination refusing
// any line ends the rendering there: for each of the lines a full explanation
// writes, exactly that many writes are attempted, none after the failing one,
// the lines before it are whole, and the destination's own error comes back.
func TestWriteExplanation_StopsAtFirstFailingWrite(t *testing.T) {
	t.Parallel()
	e, lines := fullExplanation()
	for failAt := 1; failAt <= len(lines); failAt++ {
		t.Run(fmt.Sprintf("write %d fails", failAt), func(t *testing.T) {
			t.Parallel()
			w := &failingWriter{failAt: failAt}

			err := report.WriteExplanation(w, e)

			require.ErrorIs(t, err, errDisk)
			assert.Equal(t, failAt, w.attempts, "no write may follow the failing one")
			assert.Equal(t, strings.Join(lines[:failAt-1], ""), w.text.String())
		})
	}
}
