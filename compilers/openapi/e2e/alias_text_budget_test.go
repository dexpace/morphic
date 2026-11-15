// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/ir"
)

// aliasTextDoc writes one scalar of size bytes as an extension and n extensions
// that alias it: the document adds n times size bytes of scalar text by aliases
// and no node an alias budget counts.
func aliasTextDoc(n, size int) string {
	var b strings.Builder
	b.WriteString("openapi: 3.1.0\ninfo: {title: t, version: \"1\"}\npaths: {}\nx-long: &a " + strings.Repeat("y", size) + "\n")
	for i := range n {
		fmt.Fprintf(&b, "x-e%d: *a\n", i)
	}
	return b.String()
}

// textBudgetLimits holds the byte budget at maxBytes.
func textBudgetLimits(maxBytes int) openapi.Options {
	return openapi.Options{Limits: openapi.Limits{MaxSourceBytes: maxBytes}}
}

// TestCompile_ScalarTextAliasesAddIsHeldToTheByteBudget holds the text that
// aliases add to a document to the byte budget the source is held to: a
// document of one long scalar and many aliases of it is refused, at the document,
// saying how much text they add.
func TestCompile_ScalarTextAliasesAddIsHeldToTheByteBudget(t *testing.T) {
	t.Parallel()

	doc, diags := compileText(t, "spec.yaml", aliasTextDoc(1000, 100_000), openapi.Options{})

	assert.Nil(t, doc)
	require.Len(t, diags, 1, "%+v", diags)
	assert.Equal(t, diag.BudgetExceeded, diags[0].Code)
	assert.Equal(t, ir.SeverityError, diags[0].Severity)
	assert.Equal(t, 0, diags[0].Provenance.Source)
	assert.Equal(t, "YAML aliases add 100000000 bytes of scalar text to the document, past the 67108864-byte budget",
		diags[0].Message)
}

// TestCompile_ScalarTextAliasesAddIsHeldToTheBudgetAtItsEdge holds the budget at
// a size a test can compile: text added up to it compiles and past it is
// refused, with the budget raised the refused document compiles, so the check
// reads the budget set, not a constant.
func TestCompile_ScalarTextAliasesAddIsHeldToTheBudgetAtItsEdge(t *testing.T) {
	t.Parallel()
	const size = 10_000

	within, withinDiags := compileText(t, "spec.yaml", aliasTextDoc(90, size), textBudgetLimits(1_000_000))
	past, pastDiags := compileText(t, "spec.yaml", aliasTextDoc(150, size), textBudgetLimits(1_000_000))
	raised, raisedDiags := compileText(t, "spec.yaml", aliasTextDoc(150, size), textBudgetLimits(2_000_000))

	require.NotNil(t, within)
	assert.Empty(t, withinDiags, "900000 bytes are within 1000000")
	assert.Nil(t, past)
	require.Len(t, pastDiags, 1, "%+v", pastDiags)
	assert.Equal(t, "YAML aliases add 1500000 bytes of scalar text to the document, past the 1000000-byte budget",
		pastDiags[0].Message)
	require.NotNil(t, raised, "%+v", raisedDiags)
	assert.Empty(t, raisedDiags)
}

// TestCompile_ScalarTextAliasesAddCompilesAtTheDefaultBudgetBelowIt holds 600
// aliases of a 100,000-byte scalar, 60,000,000 bytes, to compiling under the
// default budget of 67,108,864.
func TestCompile_ScalarTextAliasesAddCompilesAtTheDefaultBudgetBelowIt(t *testing.T) {
	if testing.Short() {
		t.Skip("the document compiles to tens of megabytes")
	}
	t.Parallel()

	doc, diags := compileText(t, "spec.yaml", aliasTextDoc(600, 100_000), openapi.Options{})

	require.NotNil(t, doc, "%+v", diags)
	assert.Empty(t, diags)
}

// TestCompile_TextWrittenWithoutAliasesIsNotChargedForWhatItDecodesTo holds a
// document of no alias, whose escapes decode to more bytes than they are
// written in, to compiling under a budget its written size fits: the budget
// bounds what aliases add, not the decoded text of a document without any.
func TestCompile_TextWrittenWithoutAliasesIsNotChargedForWhatItDecodesTo(t *testing.T) {
	t.Parallel()
	// \L is two bytes and decodes to the three of U+2028.
	spec := "openapi: 3.1.0\ninfo:\n  title: t\n  version: \"1\"\n  description: \"" +
		strings.Repeat(srBS+"L", 1_000_000) + "\"\npaths: {}\n"
	require.Len(t, spec, 2_000_075)

	doc, diags := compileText(t, "spec.yaml", spec, textBudgetLimits(2_500_000))

	require.NotNil(t, doc, "%+v", diags)
	assert.Empty(t, diags)
	assert.Equal(t, strings.Repeat(srLS, 1_000_000), doc.Docs.Description, "a description of 3,000,000 bytes")
}

// TestCompile_AnOverlayDocumentIsHeldToTheAliasTextBudget holds an overlay
// document whose update aliases one long scalar many times to the budget, in
// the overlay's own entry, with nothing applied.
func TestCompile_AnOverlayDocumentIsHeldToTheAliasTextBudget(t *testing.T) {
	t.Parallel()
	var overlay strings.Builder
	overlay.WriteString("overlay: 1.0.0\ninfo: {title: o, version: \"1\"}\nx-long: &a " + strings.Repeat("y", 100_000) +
		"\nactions:\n  - target: $.info\n    update:\n")
	for i := range 1000 {
		fmt.Fprintf(&overlay, "      x-%d: *a\n", i)
	}

	doc, diags := compileText(t, "spec.yaml", srMin, openapi.Options{
		Overlay: &openapi.Overlay{Path: "ov.yaml", Data: []byte(overlay.String())},
	})

	assert.Nil(t, doc, "nothing applied")
	require.Len(t, diags, 1, "%+v", diags)
	assert.Equal(t, diag.BudgetExceeded, diags[0].Code)
	assert.Equal(t, 1, diags[0].Provenance.Source, "the overlay's entry")
	assert.Contains(t, diags[0].Message, "YAML aliases add 100000000 bytes of scalar text to the document, past the 67108864-byte budget")
}

// TestCompile_AnExternalDocumentIsHeldToTheAliasTextBudget holds a document a
// reference reads, whose reached schema aliases one long scalar many times, to
// the budget: the reference is unresolved and its reason quotes the refusal.
func TestCompile_AnExternalDocumentIsHeldToTheAliasTextBudget(t *testing.T) {
	t.Parallel()
	var big strings.Builder
	big.WriteString("x-long: &a " + strings.Repeat("y", 100_000) + "\nS:\n  type: string\n")
	for i := range 1000 {
		fmt.Fprintf(&big, "  x-%d: *a\n", i)
	}
	dir := t.TempDir()
	layout(t, dir, map[string]string{
		"big.yaml":  big.String(),
		"main.yaml": srMin + "components: {schemas: {S: {$ref: \"big.yaml#/S\"}}}\n",
	})

	_, diags := compileDir(t, dir, openapi.Options{})

	refs := byCode(diags, diag.UnresolvedRef)
	require.Len(t, refs, 1, "%+v", diags)
	assert.Equal(t, "/components/schemas/S", string(refs[0].Provenance.Pointer))
	assert.Contains(t, refs[0].Message, "YAML aliases add 100000000 bytes of scalar text to the document, past the 67108864-byte budget")
}
