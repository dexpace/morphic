// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package report_test

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
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
	for _, typeDef := range nodes {
		types[typeDef.Common().ID] = typeDef
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
				model("type/aaa", "/components/schemas/Zed"),
				model("type/zzz", "/components/schemas/Alpha"),
				// Shares the query's text but not its path boundary.
				model("type/other", "/components/schemasOther/X"),
			)},
			pointer: "/components/schemas",
			want: report.Explanation{
				Pointer: "/components/schemas",
				Below: []report.Interned{
					interned("/components/schemas/Alpha", "type/zzz"),
					interned("/components/schemas/Zed", "type/aaa"),
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
			doc:     &ir.Document{Types: registry(model("type/a", "/a"), model("type/b", "/a/b"), model("type/c", "/c"))},
			pointer: "/a",
			want: report.Explanation{
				Pointer: "/a",
				Node:    &report.Interned{Pointer: "/a", ID: "type/a", Kind: ir.KindModel},
				Below:   []report.Interned{interned("/a/b", "type/b")},
			},
		},
		{
			name: "a nil registry entry is skipped, and the real one is found",
			doc: &ir.Document{Types: ir.TypeRegistry{
				"type/nil": nil, "type/real": model("type/real", "/p"),
			}},
			pointer: "/p",
			want: report.Explanation{
				Pointer: "/p",
				Node:    &report.Interned{Pointer: "/p", ID: "type/real", Kind: ir.KindModel},
			},
		},
		{
			name: "a typed nil registry entry is skipped too",
			doc: &ir.Document{Types: ir.TypeRegistry{
				"type/nil": (*ir.Model)(nil), "type/real": model("type/real", "/p"),
			}},
			pointer: "/p",
			want: report.Explanation{
				Pointer: "/p",
				Node:    &report.Interned{Pointer: "/p", ID: "type/real", Kind: ir.KindModel},
			},
		},
		{
			name: "a nil registry entry is skipped, and the real one is below the root",
			doc: &ir.Document{Types: ir.TypeRegistry{
				"type/nil": nil, "type/real": model("type/real", "/p"),
			}},
			pointer: "",
			want:    report.Explanation{Below: []report.Interned{interned("/p", "type/real")}},
		},
		{
			name: "a trailing slash is a token of its own",
			doc: &ir.Document{Types: registry(
				model("type/empty", "/components/schemas/"),
				model("type/emptyprop", "/components/schemas//properties/x"),
				model("type/otherprop", "/components/schemas/User/properties/y"),
			)},
			pointer: "/components/schemas/",
			want: report.Explanation{
				Pointer: "/components/schemas/",
				Node:    &report.Interned{Pointer: "/components/schemas/", ID: "type/empty", Kind: ir.KindModel},
				Below:   []report.Interned{interned("/components/schemas//properties/x", "type/emptyprop")},
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
		&ir.Scalar{ID: "type/s", Provenance: ir.Provenance{Pointer: "/k"}},
		&ir.Enum{ID: "type/e", Provenance: ir.Provenance{Pointer: "/k/e"}},
	)}

	got := report.Explain(doc, nil, "/k")

	want := report.Explanation{
		Pointer: "/k",
		Node:    &report.Interned{Pointer: "/k", ID: "type/s", Kind: ir.KindScalar},
		Below:   []report.Interned{{Pointer: "/k/e", ID: "type/e", Kind: ir.KindEnum}},
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
	atRoot := &ir.Model{ID: "type/root", Provenance: ir.Provenance{Source: 0}}
	decoys := map[string]ir.TypeDef{
		"shared primitive": &ir.Primitive{
			ID: "type/primitive/string", PrimitiveKind: ir.PrimitiveString, Provenance: ir.Provenance{Source: ir.NoSource},
		},
		"node located by position": &ir.Model{
			ID: "type/positioned", Provenance: ir.Provenance{Source: 0, Position: ir.Position{Line: 3, Column: 1}},
		},
	}
	for name, decoy := range decoys {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			doc := &ir.Document{Types: registry(decoy, atRoot)}

			got := report.Explain(doc, nil, "")

			want := report.Explanation{Node: &report.Interned{ID: "type/root", Kind: ir.KindModel}}
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
			id := ir.TypeID(fmt.Sprintf("type/n/%02d", i))
			types[id] = model(id, sharedPointer)
		}

		got := report.Explain(&ir.Document{Types: types}, nil, sharedPointer)

		require.NotNil(t, got.Node, "round %d: the coordinate holds %d nodes", round, nodes)
		require.Equal(t, ir.TypeID("type/n/00"), got.Node.ID, "round %d", round)
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
			id := ir.TypeID(fmt.Sprintf("type/%d/%d", n, p))
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
		diag("pass", ir.Provenance{Source: ir.NoSource, Pointer: "/p", Node: "operation/x"}),
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
	byNode := diag("n", ir.Provenance{Source: ir.NoSource, Node: "operation/x"})
	besideSource := diag("s", ir.Provenance{Source: 0, Node: "operation/x"})
	atRoot := diag("r", ir.Provenance{Source: 0})

	got := report.Explain(nil, []ir.Diagnostic{byPosition, byNode, besideSource, atRoot}, "")

	assert.Empty(t, cmp.Diff([]ir.Diagnostic{besideSource, atRoot}, got.Diagnostics),
		"only findings about the source as a whole are at the root")
}

// fullExplanation is an explanation with every section populated, and the text
// WriteExplanation renders it as, one entry per line.
func fullExplanation() (report.Explanation, []string) {
	explanation := report.Explanation{
		Pointer: "/components/schemas/S",
		Node:    &report.Interned{Pointer: "/components/schemas/S", ID: "type/s", Kind: ir.KindModel},
		Below: []report.Interned{
			{Pointer: "/components/schemas/S/properties/a", ID: "type/a", Kind: ir.KindScalar},
			{Pointer: "/components/schemas/S/properties/b", ID: "type/b", Kind: ir.KindModel},
		},
		Diagnostics: []ir.Diagnostic{
			{Severity: ir.SeverityWarning, Code: "openapi/w", Message: "careful"},
			{Severity: ir.SeverityError, Code: "openapi/e", Message: "broken"},
		},
		Unmodeled: []report.Kept{
			{Pointer: "/components/schemas/S/not", Key: "openapi:not", Reason: ir.ReasonValidationOnly, Holder: "document.Types[type/s]"},
			{Pointer: "/components/schemas/S/x-a", Key: "openapi:x-a", Reason: ir.ReasonVendorExtension, Holder: "document.Types[type/s]"},
		},
		Truncated: true,
	}
	lines := []string{
		"coordinate /components/schemas/S\n",
		"  node type/s (model)\n",
		"  interned below it (2):\n",
		"    /components/schemas/S/properties/a -> type/a (scalar)\n",
		"    /components/schemas/S/properties/b -> type/b (model)\n",
		"  kept unmodeled at or below it (2):\n",
		"    /components/schemas/S/not -> openapi:not (validation_only) on document.Types[type/s]\n",
		"    /components/schemas/S/x-a -> openapi:x-a (vendor_extension) on document.Types[type/s]\n",
		"  the document nests deeper than the walk reads, so more may be kept below\n",
		"  diagnostics (2):\n",
		"    warning openapi/w: careful\n",
		"    error openapi/e: broken\n",
	}
	return explanation, lines
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
				"  the document holds no type node at this coordinate\n" +
				"  diagnostics (0):\n",
		},
		{
			"the root with a node",
			report.Explanation{Node: &report.Interned{ID: "type/root", Kind: ir.KindModel}},
			"coordinate \"\" (the whole document)\n" +
				"  node type/root (model)\n" +
				"  diagnostics (0):\n",
		},
		{
			"a miss with something below it",
			report.Explanation{
				Pointer: "/c",
				Below:   []report.Interned{{Pointer: "/c/x", ID: "type/x", Kind: ir.KindEnum}},
			},
			"coordinate /c\n" +
				"  the document holds no type node at this coordinate\n" +
				"  interned below it (1):\n" +
				"    /c/x -> type/x (enum)\n" +
				"  diagnostics (0):\n",
		},
		{
			"a miss with only something kept below it",
			report.Explanation{
				Pointer:   "/paths/~1p",
				Unmodeled: []report.Kept{{Pointer: "/paths/~1p/x-a", Key: "openapi:pathItem/x-a", Reason: ir.ReasonVendorExtension, Holder: "document.Services[0].Groups[0].Operations[0]"}},
			},
			"coordinate /paths/~1p\n" +
				"  the document holds no type node at this coordinate\n" +
				"  kept unmodeled at or below it (1):\n" +
				"    /paths/~1p/x-a -> openapi:pathItem/x-a (vendor_extension) on document.Services[0].Groups[0].Operations[0]\n" +
				"  diagnostics (0):\n",
		},
		{
			"a node with nothing below it and a diagnostic",
			report.Explanation{
				Pointer:     "/c",
				Node:        &report.Interned{Pointer: "/c", ID: "type/c", Kind: ir.KindPrimitive},
				Diagnostics: []ir.Diagnostic{{Severity: ir.SeverityInfo, Code: "x/i", Message: "fyi"}},
			},
			"coordinate /c\n" +
				"  node type/c (primitive)\n" +
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

// TestWriteExplanation_EscapesWhatASpecWrites holds each string of the report a
// spec can write, the coordinate, the IDs and pointers it names, and a
// finding's code and message, to one printable line, as WriteText does.
func TestWriteExplanation_EscapesWhatASpecWrites(t *testing.T) {
	t.Parallel()
	explanation := report.Explanation{
		Pointer: "/S\x1b[31m",
		Node:    &report.Interned{Pointer: "/S\x1b[31m", ID: "type/openapi/S\x1b[31m", Kind: "model\n"},
		Below:   []report.Interned{{Pointer: "/S\x1b[31m/x\a", ID: "type/x\r", Kind: ir.KindEnum}},
		Diagnostics: []ir.Diagnostic{{Severity: ir.SeverityInfo, Code: "x/\x00",
			Message: "M\x1b[31mY\nerror openapi-forged spec.yaml: forged"}},
	}
	var w strings.Builder
	require.NoError(t, report.WriteExplanation(&w, explanation))
	assert.Empty(t, cmp.Diff(`coordinate /S\x1b[31m`+"\n"+
		`  node type/openapi/S\x1b[31m (model\n)`+"\n"+
		"  interned below it (1):\n"+
		`    /S\x1b[31m/x\a -> type/x\r (enum)`+"\n"+
		"  diagnostics (1):\n"+
		`    info x/\x00: M\x1b[31mY\nerror openapi-forged spec.yaml: forged`+"\n", w.String()))
}

// TestWriteExplanation_StopsAtFirstFailingWrite pins that a destination refusing
// any line ends the rendering there: for each of the lines a full explanation
// writes, exactly that many writes are attempted, none after the failing one,
// the lines before it are whole, and the destination's own error comes back.
func TestWriteExplanation_StopsAtFirstFailingWrite(t *testing.T) {
	t.Parallel()
	explanation, lines := fullExplanation()
	for failAt := 1; failAt <= len(lines); failAt++ {
		t.Run(fmt.Sprintf("write %d fails", failAt), func(t *testing.T) {
			t.Parallel()
			failingWriterValue := &failingWriter{failAt: failAt}

			err := report.WriteExplanation(failingWriterValue, explanation)

			require.ErrorIs(t, err, errDisk)
			assert.Equal(t, failAt, failingWriterValue.attempts, "no write may follow the failing one")
			assert.Equal(t, strings.Join(lines[:failAt-1], ""), failingWriterValue.text.String())
		})
	}
}

// TestExplain_ReadsOnlyTheSourcesCoordinates pins which positions a coordinate
// of the compiled source matches: the source's own, whoever wrote them, an
// overlay included, but not the overlay's own document, nor another document,
// whose pointer spells a position of that document though it is written alike,
// nor an entry the table does not hold.
func TestExplain_ReadsOnlyTheSourcesCoordinates(t *testing.T) {
	t.Parallel()
	const pet = jsontext.Pointer("/components/schemas/Pet")
	sources := []ir.SourceInfo{{Format: "openapi@3.1"}, {Format: "overlay@1.0.0"}, {Format: "openapi@3.1"}}
	at := func(id ir.TypeID, prov ir.Provenance) *ir.Model { return &ir.Model{ID: id, Provenance: prov} }
	doc := &ir.Document{Sources: sources, Types: registry(
		at("type/0-other", ir.Provenance{Source: 2, Pointer: pet}),
		at("type/1-source", ir.Provenance{Source: 0, Pointer: pet}),
		at("type/2-other", ir.Provenance{Source: 2, Pointer: pet + "/properties/owner"}),
		at("type/3-written", ir.Provenance{Source: 0, Pointer: pet + "/properties/tag", WrittenBy: 1}),
		at("type/4-unheld", ir.Provenance{Source: 7, Pointer: pet + "/properties/gone"}),
		at("type/5-overlay-file", ir.Provenance{Source: 1, Pointer: pet + "/properties/x"}),
	)}
	written := diag("written", ir.Provenance{Source: 0, Pointer: pet, WrittenBy: 1})
	diags := []ir.Diagnostic{
		diag("source", ir.Provenance{Source: 0, Pointer: pet}), written,
		diag("overlay-file", ir.Provenance{Source: 1, Pointer: pet}),
		diag("other", ir.Provenance{Source: 2, Pointer: pet}), diag("unheld", ir.Provenance{Source: 7, Pointer: pet}),
	}

	got := report.Explain(doc, diags, pet)

	assert.Empty(t, sameExplanation(report.Explanation{
		Pointer:     pet,
		Node:        &report.Interned{Pointer: pet, ID: "type/1-source", Kind: ir.KindModel},
		Below:       []report.Interned{interned(pet+"/properties/tag", "type/3-written")},
		Diagnostics: []ir.Diagnostic{diags[0], written},
	}, got))
}

// TestExplanation_EncodesAsData pins the JSON a driver reads an explanation as,
// so an editor integration never parses WriteExplanation's text: each member by
// its lower-camel name, a miss without a node, empty sections left out, and the
// value read back unchanged.
func TestExplanation_EncodesAsData(t *testing.T) {
	t.Parallel()
	full, _ := fullExplanation()
	tests := []struct {
		name string
		e    report.Explanation
		want string
	}{
		{"every section", full, `{"pointer":"/components/schemas/S",` +
			`"node":{"pointer":"/components/schemas/S","id":"type/s","kind":"model"},` +
			`"below":[{"pointer":"/components/schemas/S/properties/a","id":"type/a","kind":"scalar"},` +
			`{"pointer":"/components/schemas/S/properties/b","id":"type/b","kind":"model"}],` +
			`"diagnostics":[{"severity":"warning","code":"openapi/w","message":"careful","provenance":{"source":0}},` +
			`{"severity":"error","code":"openapi/e","message":"broken","provenance":{"source":0}}],` +
			`"unmodeled":[{"pointer":"/components/schemas/S/not","key":"openapi:not","reason":"validation_only","holder":"document.Types[type/s]"},` +
			`{"pointer":"/components/schemas/S/x-a","key":"openapi:x-a","reason":"vendor_extension","holder":"document.Types[type/s]"}],` +
			`"truncated":true}`},
		{"a miss at the root", report.Explanation{}, `{"pointer":""}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			raw, err := json.Marshal(tt.e, json.Deterministic(true))
			require.NoError(t, err)
			var back report.Explanation
			require.NoError(t, json.Unmarshal(raw, &back))

			assert.Equal(t, tt.want, string(raw))
			assert.Empty(t, cmp.Diff(tt.e, back))
		})
	}
}

// kept returns an Unmodeled map of one entry, keyed key and written at pointer
// in source.
func kept(key string, source int, pointer jsontext.Pointer) ir.Unmodeled {
	return ir.Unmodeled{key: {Reason: ir.ReasonVendorExtension, Value: ir.RawValue("1"),
		Provenance: ir.Provenance{Source: source, Pointer: pointer}}}
}

// TestExplain_ListsWhatIsKeptAtAndBelow is the table over the Unmodeled
// entries an explanation lists: those written at the coordinate or beneath a
// pointer token of it, on whatever node holds them, the document's own and an
// operation's among them, and none written beside it under a shared prefix of
// text, or in another document. Entries at one pointer list by key, and one
// entry kept on several nodes, as a path item's on each operation, in the
// order the document holds them, the eleventh operation after the tenth.
func TestExplain_ListsWhatIsKeptAtAndBelow(t *testing.T) {
	t.Parallel()
	const pet = jsontext.Pointer("/components/schemas/Pet")
	irModel := model("type/pet", pet)
	irModel.Unmodeled = ir.Unmodeled{
		"openapi:x-b": kept("", 0, pet+"/x-b")[""],
		"openapi:x-a": kept("", 0, pet+"/x-a")[""],
		"openapi:not": {Reason: ir.ReasonValidationOnly, Value: ir.RawValue("{}"), Provenance: ir.Provenance{Pointer: pet}},
		// Keys at one pointer in one map, put in against their order, which no
		// order a map is read in turns back.
		"openapi:x-z": kept("", 0, pet+"/x-y")[""],
		"openapi:x-y": kept("", 0, pet+"/x-y")[""],
		"openapi:x-x": kept("", 0, pet+"/x-y")[""],
	}
	irModel.Properties = []ir.Property{{Name: ir.Naming{Source: "p"}, Unmodeled: kept("openapi:x-p", 0, pet+"/properties/p/x-p")}}
	other := model("type/other", "/components/schemas/PetOther")
	other.Unmodeled = kept("openapi:x-o", 0, "/components/schemas/PetOther/x-o")
	elsewhere := model("type/else", pet)
	elsewhere.Unmodeled = kept("openapi:x-e", 2, pet+"/x-e")
	shared := kept("openapi:pathItem/x-s", 0, pet+"/x-s")
	operations := make([]ir.Operation, 11)
	for i := range operations {
		operations[i] = ir.Operation{ID: ir.OperationID(fmt.Sprintf("operation/%d", i))}
	}
	operations[9].Unmodeled, operations[10].Unmodeled = shared, shared
	doc := &ir.Document{
		Types:     registry(irModel, other, elsewhere),
		Unmodeled: kept("openapi:x-d", 0, pet+"/x-d"),
		Services:  []ir.Service{{Groups: []ir.OperationGroup{{Operations: operations}}}},
	}
	entry := func(pointer jsontext.Pointer, key, holder string) report.Kept {
		return report.Kept{Pointer: pointer, Key: key, Reason: ir.ReasonVendorExtension, Holder: holder}
	}

	got := report.Explain(doc, nil, pet)

	assert.Empty(t, cmp.Diff([]report.Kept{
		{Pointer: pet, Key: "openapi:not", Reason: ir.ReasonValidationOnly, Holder: "document.Types[type/pet]"},
		entry(pet+"/properties/p/x-p", "openapi:x-p", "document.Types[type/pet].Properties[0]"),
		entry(pet+"/x-a", "openapi:x-a", "document.Types[type/pet]"),
		entry(pet+"/x-b", "openapi:x-b", "document.Types[type/pet]"),
		entry(pet+"/x-d", "openapi:x-d", "document"),
		entry(pet+"/x-s", "openapi:pathItem/x-s", "document.Services[0].Groups[0].Operations[9]"),
		entry(pet+"/x-s", "openapi:pathItem/x-s", "document.Services[0].Groups[0].Operations[10]"),
		entry(pet+"/x-y", "openapi:x-x", "document.Types[type/pet]"),
		entry(pet+"/x-y", "openapi:x-y", "document.Types[type/pet]"),
		entry(pet+"/x-y", "openapi:x-z", "document.Types[type/pet]"),
	}, got.Unmodeled))
	assert.False(t, got.Truncated)
	assert.Empty(t, report.Explain(doc, nil, pet+"/properties/q").Unmodeled, "nothing is kept there")
	assert.Len(t, report.Explain(doc, nil, "").Unmodeled, 11, "the root holds every entry of the source")
	assert.Empty(t, report.Explain(nil, nil, "").Unmodeled, "a nil document keeps nothing")
}

// TestExplain_ListsAnEntryOnManyNodesInTheirOrder pins the order of one entry
// kept on every operation, beside an entry of each operation's own: the
// document's. The list is longer than a sort orders by insertion, which keeps
// equal entries in place whether or not the sort is stable.
func TestExplain_ListsAnEntryOnManyNodesInTheirOrder(t *testing.T) {
	t.Parallel()
	const at, key = jsontext.Pointer("/paths/~1p"), "openapi:pathItem/x-s"
	operations := make([]ir.Operation, 12)
	want := make([]string, 0, len(operations))
	for i := range operations {
		own := fmt.Sprintf("openapi:x-%02d", i)
		operations[i] = ir.Operation{ID: ir.OperationID(fmt.Sprintf("operation/%d", i)), Unmodeled: ir.Unmodeled{
			key: kept(key, 0, at+"/x-s")[key],
			own: kept(own, 0, at+jsontext.Pointer(fmt.Sprintf("/x-%02d", i)))[own],
		}}
		want = append(want, fmt.Sprintf("document.Services[0].Groups[0].Operations[%d]", i))
	}
	doc := &ir.Document{Services: []ir.Service{{Groups: []ir.OperationGroup{{Operations: operations}}}}}

	var got []string
	for _, kept := range report.Explain(doc, nil, at).Unmodeled {
		if kept.Key == key {
			got = append(got, kept.Holder)
		}
	}

	assert.Equal(t, want, got)
}

// TestExplain_SaysWhenTheWalkWasTruncated pins that an entry the walk could
// not reach is not passed over in silence: a document nested past
// ir.MaxWalkDepth marks the explanation, whose list can then lack one.
func TestExplain_SaysWhenTheWalkWasTruncated(t *testing.T) {
	t.Parallel()
	value := ir.Value{Kind: ir.ValueNull}
	for range ir.MaxWalkDepth {
		value = ir.Value{Kind: ir.ValueList, List: []ir.Value{value}}
	}
	irModel := model("type/m", "/m")
	irModel.Examples = []ir.Example{{Value: &value}}
	irModel.Unmodeled = kept("openapi:x-m", 0, "/m/x-m")

	got := report.Explain(&ir.Document{Types: registry(irModel)}, nil, "/m")

	assert.True(t, got.Truncated)
	assert.Len(t, got.Unmodeled, 1, "what the walk reached is listed still")
}

// TestWriteExplanation_EscapesWhatItListsAsKept holds each string a kept
// entry's line spells, all of them the document's, to one printable line.
func TestWriteExplanation_EscapesWhatItListsAsKept(t *testing.T) {
	t.Parallel()
	explanation := report.Explanation{Pointer: "/p", Unmodeled: []report.Kept{{
		Pointer: "/p/x-\x1b[31m", Key: "openapi:x-\nerror forged", Reason: "vendor\a", Holder: "document.Types[t/\xff\u202e]",
	}}}
	var w strings.Builder

	require.NoError(t, report.WriteExplanation(&w, explanation))

	assert.Equal(t, "coordinate /p\n"+
		"  the document holds no type node at this coordinate\n"+
		"  kept unmodeled at or below it (1):\n"+
		`    /p/x-\x1b[31m -> openapi:x-\nerror forged (vendor\a) on document.Types[t/\xff\u202e]`+"\n"+
		"  diagnostics (0):\n", w.String())
}
