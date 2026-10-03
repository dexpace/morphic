package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
)

const explainSpec = `openapi: 3.1.0
info: {title: t, version: v}
components:
  schemas:
    S:
      type: object
      properties: {a: {type: string}}
      not: {type: string}
`

// TestRun_ExplainReportsTheNodeAtACoordinate is the end-to-end path: --explain
// reports what compiling produced at one source pointer instead of the document.
func TestRun_ExplainReportsTheNodeAtACoordinate(t *testing.T) {
	t.Parallel()
	spec := writeFile(t, "spec.yaml", explainSpec)
	var stdout, stderr bytes.Buffer

	code := run([]string{"compile", spec, "--explain", "/components/schemas/S"}, &stdout, &stderr)

	require.Equal(t, 0, code, "stderr: %s", stderr.String())
	out := stdout.String()
	assert.Contains(t, out, "coordinate /components/schemas/S")
	assert.Contains(t, out, "node t/openapi/components/schemas/S (model)")
	assert.Contains(t, out, "openapi/validation-only-keyword",
		"a diagnostic stamped at the coordinate is part of what happened there")
	assert.NotContains(t, out, `"irVersion"`, "--explain replaces the document, it does not add to it")
}

// TestRun_ExplainTakesTheWholeDocumentAsTheEmptyPointer pins that an empty
// --explain is the root coordinate rather than an absent flag: the empty JSON
// Pointer names the whole document, and it is the only query everything
// interned sits beneath.
func TestRun_ExplainTakesTheWholeDocumentAsTheEmptyPointer(t *testing.T) {
	t.Parallel()
	spec := writeFile(t, "spec.yaml", explainSpec)
	var stdout, stderr bytes.Buffer

	code := run([]string{"compile", spec, "--explain", ""}, &stdout, &stderr)

	require.Equal(t, 0, code, "stderr: %s", stderr.String())
	out := stdout.String()
	assert.Contains(t, out, `coordinate "" (the whole document)`)
	assert.Contains(t, out, "/components/schemas/S -> t/openapi/components/schemas/S (model)",
		"the whole document holds every coordinate that interned a node")
	assert.NotContains(t, out, `"irVersion"`, "an empty --explain is still --explain, not the document")
}

// TestRun_ExplainNamesNoPrimitiveAtTheWholeDocument pins GitHub #528 end to
// end: a shared primitive's empty Pointer sits on NoSource, so the root query
// does not name it. Both assertions on the explanation hold on a compile that
// interned no primitive, so the document is compiled too, to show this one did.
func TestRun_ExplainNamesNoPrimitiveAtTheWholeDocument(t *testing.T) {
	t.Parallel()
	spec := writeFile(t, "spec.yaml", explainSpec)
	var document, explained, stderr bytes.Buffer

	require.Equal(t, 0, run([]string{"compile", spec}, &document, &stderr), "stderr: %s", stderr.String())
	require.Equal(t, 0, run([]string{"compile", spec, "--explain", ""}, &explained, &stderr),
		"stderr: %s", stderr.String())

	require.Contains(t, document.String(), `"t/prim/string"`, `property "a" interns the shared string primitive`)
	out := explained.String()
	assert.Contains(t, out, "no type node was interned at this coordinate",
		"nothing is declared at the document root itself")
	assert.NotContains(t, out, "t/prim/", "a shared primitive is at no coordinate")
}

// TestExplainDocument_MissAtACoordinateNamesWhatInternedBelow covers the case the
// command exists for: a schema that reduced to a shared primitive owns no node at
// its own coordinate, and what interned beneath it is the difference between
// "nothing happened here" and "the node moved".
func TestExplainDocument_MissAtACoordinateNamesWhatInternedBelow(t *testing.T) {
	t.Parallel()
	// The IDs sort in the opposite order to their pointers, so listing by pointer
	// is distinguishable from listing in the registry's own ID order. With a
	// fixture whose two orders agree, dropping the sort changes nothing and the
	// assertion below passes for the wrong reason.
	doc := &ir.Document{Types: ir.TypeRegistry{
		"t/aaa": &ir.Model{
			ID: "t/aaa", Provenance: ir.Provenance{Pointer: "/components/schemas/Zed"},
		},
		"t/zzz": &ir.Model{
			ID: "t/zzz", Provenance: ir.Provenance{Pointer: "/components/schemas/Alpha"},
		},
		// A sibling whose pointer shares the query's text but not its path
		// boundary. "below" means beneath a segment, not sharing a prefix.
		"t/other": &ir.Model{
			ID: "t/other", Provenance: ir.Provenance{Pointer: "/components/schemasOther/X"},
		},
	}}
	var w bytes.Buffer
	explainDocument(&w, doc, nil, "/components/schemas")

	out := w.String()
	assert.Contains(t, out, "no type node was interned at this coordinate")
	assert.Contains(t, out, "interned below it (2)")
	assert.Less(t, strings.Index(out, "/components/schemas/Alpha"), strings.Index(out, "/components/schemas/Zed"),
		"coordinates are listed in pointer order, not in the registry's ID order")
	assert.NotContains(t, out, "schemasOther",
		"a sibling sharing the query's text but not its path boundary is not below it")
}

func TestExplainDocument_MissWithNothingBelow(t *testing.T) {
	t.Parallel()
	var w bytes.Buffer
	explainDocument(&w, &ir.Document{Types: ir.TypeRegistry{}}, nil, "/nowhere")

	out := w.String()
	assert.Contains(t, out, "no type node was interned at this coordinate")
	assert.NotContains(t, out, "interned below it", "an empty subtree prints no subtree section")
	assert.Contains(t, out, "diagnostics (0)")
}

// TestExplainDocument_NilEntriesAreSkipped pins that explain reports a malformed
// registry rather than crashing on it: a nil type definition is what
// pass.Validate and irverify exist to describe, and the explainer must not be
// the thing that faults on one.
func TestExplainDocument_NilEntriesAreSkipped(t *testing.T) {
	t.Parallel()
	doc := &ir.Document{Types: ir.TypeRegistry{
		"t/nil":  nil,
		"t/real": &ir.Model{ID: "t/real", Provenance: ir.Provenance{Pointer: "/p"}},
	}}
	var w bytes.Buffer
	require.NotPanics(t, func() { explainDocument(&w, doc, nil, "/p") })
	assert.Contains(t, w.String(), "node t/real (model)")

	var below bytes.Buffer
	require.NotPanics(t, func() { explainDocument(&below, doc, nil, "") })
	assert.Contains(t, below.String(), "interned below it (1)")
}

// TestExplainDocument_TrailingSlashIsAToken pins that a trailing '/' names a
// member of its own: /components/schemas/ is the schema keyed "", so only its
// subtree is beneath it. Trimming the slash, as the query once was, lists every
// component's subtree instead, and the queried schema as beneath itself.
func TestExplainDocument_TrailingSlashIsAToken(t *testing.T) {
	t.Parallel()
	doc := &ir.Document{Types: ir.TypeRegistry{
		"t/empty": &ir.Model{
			ID: "t/empty", Provenance: ir.Provenance{Pointer: "/components/schemas/"},
		},
		"t/emptyprop": &ir.Model{
			ID: "t/emptyprop", Provenance: ir.Provenance{Pointer: "/components/schemas//properties/x"},
		},
		"t/otherprop": &ir.Model{
			ID: "t/otherprop", Provenance: ir.Provenance{Pointer: "/components/schemas/User/properties/y"},
		},
	}}
	var w bytes.Buffer
	explainDocument(&w, doc, nil, "/components/schemas/")

	out := w.String()
	assert.Contains(t, out, "node t/empty (model)", `the schema keyed "" is found at its own pointer`)
	assert.Contains(t, out, "interned below it (1)")
	assert.Contains(t, out, "/components/schemas//properties/x")
	assert.NotContains(t, out, "/components/schemas/User/properties/y",
		"a sibling schema's property shares the query's text but not its token boundary")
}

func TestExplainDocument_NilDocumentIsReportedNotDereferenced(t *testing.T) {
	t.Parallel()
	var w bytes.Buffer
	require.NotPanics(t, func() { explainDocument(&w, nil, nil, "/p") })
	assert.Contains(t, w.String(), "no type node was interned at this coordinate")
}

// TestNodeAtPointer_AnEmptyPointerIsNotAlwaysTheRoot pins that the root query
// matches a node by where its provenance locates it, not by its Pointer being
// empty. A shared primitive on NoSource and a node located by position both
// record an empty Pointer without being at the whole document. Each decoy's ID
// sorts ahead of t/root's, so a match on the Pointer alone returns the decoy.
// No committed spec lowers a type node at the root, so only a document built by
// hand holds one.
func TestNodeAtPointer_AnEmptyPointerIsNotAlwaysTheRoot(t *testing.T) {
	t.Parallel()
	atRoot := &ir.Model{ID: "t/root", Provenance: ir.Provenance{Source: 0}}
	decoys := map[string]ir.TypeDef{
		"shared primitive": &ir.Primitive{
			ID:         "t/prim/string",
			Prim:       ir.PrimString,
			Provenance: ir.Provenance{Source: ir.NoSource},
		},
		"node located by position": &ir.Model{
			ID:         "t/positioned",
			Provenance: ir.Provenance{Source: 0, Position: ir.Position{Line: 3, Column: 1}},
		},
	}
	for name, decoy := range decoys {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			doc := &ir.Document{Types: ir.TypeRegistry{decoy.Common().ID: decoy, atRoot.ID: atRoot}}

			id, _, ok := nodeAtPointer(doc, "")

			require.True(t, ok, "a node declared at the root is still found there")
			assert.Equal(t, atRoot.ID, id, "the root names that node, never the decoy")
		})
	}
}

func TestDiagnosticsAt_SelectsOnlyTheCoordinate(t *testing.T) {
	t.Parallel()
	diags := []ir.Diagnostic{
		{Code: "a", Provenance: ir.Provenance{Pointer: "/p"}},
		{Code: "b", Provenance: ir.Provenance{Pointer: "/q"}},
		{Code: "c", Provenance: ir.Provenance{Pointer: "/p"}},
	}
	got := diagnosticsAt(diags, "/p")
	require.Len(t, got, 2)
	assert.Equal(t, "a", got[0].Code, "emitted order is preserved")
	assert.Equal(t, "c", got[1].Code)
}

// TestDiagnosticsAt_RootExcludesPositionAndNodeLocatedFindings pins what
// listing under the root pointer means now that Position and Node exist
// beside it. A finding located by position, or by a pass's node on NoSource,
// also carries an empty Pointer, but neither is about the whole document:
// listing them under "" would put every such finding in the compile at the
// root. A node beside a real source locates nothing inside it, so that finding
// is about the source as a whole and is listed, as the CLI prints it bare.
func TestDiagnosticsAt_RootExcludesPositionAndNodeLocatedFindings(t *testing.T) {
	t.Parallel()
	byPosition := ir.Diagnostic{Code: "p",
		Provenance: ir.Provenance{Source: 0, Position: ir.Position{Line: 5, Column: 1}}}
	byNode := ir.Diagnostic{Code: "n", Provenance: ir.Provenance{Source: ir.NoSource, Node: "op/x"}}
	besideSource := ir.Diagnostic{Code: "s", Provenance: ir.Provenance{Source: 0, Node: "op/x"}}
	atRoot := ir.Diagnostic{Code: "r", Provenance: ir.Provenance{Source: 0}}

	got := diagnosticsAt([]ir.Diagnostic{byPosition, byNode, besideSource, atRoot}, "")

	codes := make([]string, 0, len(got))
	for _, d := range got {
		codes = append(codes, d.Code)
	}
	assert.Equal(t, []string{"s", "r"}, codes, "only findings about the source as a whole are at the root")
}
