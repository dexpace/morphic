// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/ir"
)

// nodesOf returns the options of a compile holding a source to limit nodes.
func nodesOf(limit int) openapi.Options {
	return openapi.Options{Limits: openapi.Limits{MaxSourceNodes: limit}}
}

// TestCompile_ASourceIsHeldToTheNodeBudgetAsItIsRead holds a JSON source and a
// YAML source with the same 22 nodes to one verdict, worded by what each read
// knows: the JSON read stopped where it crossed, the YAML tree was counted whole.
func TestCompile_ASourceIsHeldToTheNodeBudgetAsItIsRead(t *testing.T) {
	t.Parallel()
	const jsonText = `{"openapi":"3.1.0","info":{"title":"t","version":"1"},"paths":{},"x-p":[0,0,0,0,0,0,0,0]}`
	yamlText := srMin + "x-p: [0, 0, 0, 0, 0, 0, 0, 0]\n"
	tests := []struct {
		name, file, text, want string
	}{
		{"JSON", "spec.json", jsonText, "source document holds more than 12 nodes, the node budget; reading stopped there"},
		{"YAML", "spec.yaml", yamlText, "source document parses to 22 nodes, past the 12-node budget"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			doc, diags := compileText(t, tc.file, tc.text, nodesOf(12))

			require.Len(t, diags, 1, "%+v", diags)
			assert.Equal(t, diag.BudgetExceeded, diags[0].Code)
			assert.Equal(t, ir.SeverityError, diags[0].Severity)
			assert.Equal(t, 0, diags[0].Provenance.Source)
			assert.Equal(t, tc.want, diags[0].Message)
			assert.Nil(t, doc)

			doc, diags = compileText(t, tc.file, tc.text, nodesOf(22))
			require.NotNil(t, doc, "a document of exactly the budget compiles")
			assert.Empty(t, diags)
		})
	}
}

// TestCompile_AnOverlayDoesNotLiftTheNodeBudgetOfTheSource holds the one verdict
// the budget changes on valid input: a source past it is refused although an
// overlay would shrink it under the budget, because the budget bounds the
// document as read.
func TestCompile_AnOverlayDoesNotLiftTheNodeBudgetOfTheSource(t *testing.T) {
	t.Parallel()
	const remove = "overlay: 1.0.0\ninfo: {title: o, version: \"1\"}\nactions:\n  - target: \"$['x-p']\"\n    remove: true\n"
	src := srMin + "x-p: [0, 0, 0, 0, 0, 0, 0, 0]\n"
	run := func(limit int) (*ir.Document, []ir.Diagnostic) {
		opts := nodesOf(limit)
		opts.Overlay = &openapi.Overlay{Path: "ov.yaml", Data: []byte(remove)}
		return compileText(t, "spec.yaml", src, opts)
	}

	doc, diags := run(15)

	require.Len(t, diags, 1, "%+v", diags)
	assert.Equal(t, diag.BudgetExceeded, diags[0].Code)
	assert.Equal(t, 0, diags[0].Provenance.Source, "the source's entry")
	assert.Contains(t, diags[0].Message, "22 nodes")
	assert.Contains(t, diags[0].Message, "15-node budget")
	assert.Nil(t, doc)

	doc, diags = run(22)
	require.NotNil(t, doc)
	assert.Empty(t, diags)
}

// TestCompile_AnOverlayIsHeldToTheNodeBudgetAsItIsRead holds an overlay document
// to the budget the way a source is, in its own entry.
func TestCompile_AnOverlayIsHeldToTheNodeBudgetAsItIsRead(t *testing.T) {
	t.Parallel()
	overlay := `{"overlay":"1.0.0","info":{"title":"o","version":"1"},"actions":[{"target":"$.info","update":` +
		`{"x-p":[` + strings.TrimSuffix(strings.Repeat("0,", 24), ",") + `]}}]}`

	doc, diags := compileOverlayWith(t, overlay, nodesOf(30))

	require.Len(t, diags, 1, "%+v", diags)
	assert.Equal(t, diag.BudgetExceeded, diags[0].Code)
	assert.Equal(t, ir.SeverityError, diags[0].Severity)
	assert.Equal(t, 1, diags[0].Provenance.Source, "the overlay's entry")
	assert.Equal(t, "overlay document holds more than 30 nodes, the node budget; reading stopped there", diags[0].Message)
	assert.Nil(t, doc)
}

// compileOverlayWith compiles srMin with overlay and opts' limits.
func compileOverlayWith(t *testing.T, overlay string, opts openapi.Options) (*ir.Document, []ir.Diagnostic) {
	t.Helper()
	opts.Overlay = &openapi.Overlay{Path: "ov.json", Data: []byte(overlay)}
	return compileText(t, "spec.yaml", srMin, opts)
}

// TestCompile_AnExternalDocumentIsHeldToTheNodeBudgetAsItIsRead holds a JSON
// document a reference reads to the budget as it is read: the reason says it
// holds more than the budget, not that it parses to some count, and nothing of
// it is recorded among the sources.
func TestCompile_AnExternalDocumentIsHeldToTheNodeBudgetAsItIsRead(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	layout(t, dir, map[string]string{
		"big.json":  `{"a":[` + strings.TrimSuffix(strings.Repeat("0,", 30), ",") + `]}`,
		"main.yaml": srMin + "components: {schemas: {S: {$ref: \"big.json#/a\"}}}\n",
	})

	doc, diags := compileDir(t, dir, nodesOf(20))

	refs := byCode(diags, diag.UnresolvedRef)
	require.Len(t, refs, 1, "%+v", diags)
	assert.Equal(t, "/components/schemas/S", string(refs[0].Provenance.Pointer))
	assert.Contains(t, refs[0].Message, "it holds more than 20 nodes, the node budget")
	assert.NotContains(t, refs[0].Message, "it parses to")
	require.NotNil(t, doc)
	assert.Len(t, doc.Sources, 1, "nothing of the refused document is recorded")
}
