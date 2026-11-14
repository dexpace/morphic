// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package overlay

import (
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/budget"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/ir"
)

// budgetSpec and budgetOverlay are a document and a patch small enough to hold
// in a sentence and still more than a two-node budget: the point of the cases
// below is where the walk runs out, not what either one is looking at.
const (
	budgetSpec = `openapi: 3.1.0
info: {title: T, version: "1"}
`
	budgetOverlay = "overlay: 1.0.0\ninfo: {title: O, version: \"1\"}\n" +
		"actions:\n  - target: $.info\n    update: {description: d}\n"
)

// TestApplyWithin_DegradesToTheSourceAtTheNodeBudget pins what exhausting the
// attribution walk's bound costs and what it must not: the overlay still
// applies, and attribution is abandoned wholesale rather than half-taken, and
// reported, since a caller reading provenance has no other way to tell the
// degradation from a compile with no overlay.
func TestApplyWithin_DegradesToTheSourceAtTheNodeBudget(t *testing.T) {
	t.Parallel()
	var root yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte(budgetSpec), &root))

	origin, diags := applyWithin(t.Context(), 1, &root, docOf(t, budgetOverlay), Options{Data: []byte(budgetOverlay)}, budget.AtMost(2))

	assert.True(t, origin.Applied(), "the overlay applied, so it stays an input of the document")
	assert.Equal(t, "overlay@1.0.0", origin.Source().Format, "and keeps its identity for Document.Sources")
	assert.Equal(t, 0, origin.Attribution().WriterAt("/info/description"), "no position is attributed to it, not even one it introduced")

	require.Len(t, diags, 1)
	assert.Equal(t, diag.OverlayOriginIncomplete, diags[0].Code)
	assert.Equal(t, ir.SeverityWarning, diags[0].Severity, "the compile still proceeds")

	description := root.Content[0].Content[3].Content[5]
	assert.Equal(t, "d", description.Value, "the overlay applied; only the attribution of it was given up")
}

// TestApplyRecovered_QuotesAPanicOnOneBoundedLine pins that a fault in the
// application reaches the refusal as diag.Catch quotes it, whatever the
// application panicked with, and that nothing the application left is
// reported.
func TestApplyRecovered_QuotesAPanicOnOneBoundedLine(t *testing.T) {
	t.Parallel()
	at := ir.Provenance{Source: 1}
	huge := strings.Repeat("the selector faulted here\n", 1<<10)

	diags, applied := applyRecovered(at, func() ([]ir.Diagnostic, bool) { panic(huge) })

	assert.False(t, applied)
	require.Len(t, diags, 1)
	assert.Equal(t, diag.OverlayFailed, diags[0].Code)
	assert.Equal(t, at, diags[0].Provenance)
	assert.Equal(t, "cannot apply overlay: applying it panicked ("+diag.OneLine(errors.New(huge))+")",
		diags[0].Message)
	assert.NotContains(t, diags[0].Message, "\n")
	assert.Less(t, len(diags[0].Message), len(huge)/4, "the bound cut the value")
}

// TestApplyWithin_RefusesARootTheDocumentDoesNotHold pins a tree the parser
// never hands over and a caller assembling nodes can: a document node holding
// no root, which a target `$` selects whole. It holds no value to write, and
// is held by nothing a remove removes it from, so either action is refused.
func TestApplyWithin_RefusesARootTheDocumentDoesNotHold(t *testing.T) {
	t.Parallel()
	for _, action := range []string{"update: {a: b}", "remove: true"} {
		root := &yaml.Node{Kind: yaml.DocumentNode}
		ov := "overlay: 1.1.0\ninfo: {title: O, version: \"1\"}\nactions:\n  - target: $\n    " + action + "\n"

		origin, diags := applyWithin(t.Context(), 1, root, docOf(t, ov), Options{Data: []byte(ov)}, budget.AtMost(maxNodes))

		assert.False(t, origin.Applied(), action)
		require.Len(t, diags, 1, action)
		assert.Equal(t, diag.OverlayFailed, diags[0].Code, action)
	}
}
