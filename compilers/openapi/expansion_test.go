// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package openapi_test

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/engine/reader"
	"github.com/dexpace/morphic/ir"
)

// compileExpanding compiles src with the reference-expansion budget set to
// limit, every other budget at its default.
func compileExpanding(t *testing.T, src string, limit int) (*ir.Document, []ir.Diagnostic) {
	t.Helper()
	doc, diags, err := openapi.New().Compile(t.Context(), []compilers.Source{openapitest.SourceOf(src)},
		compilers.Options{FormatOptions: openapi.Options{Limits: openapi.Limits{MaxReferenceExpansion: limit}}})
	require.NoError(t, err, "a document past the budget is a spec problem, not a Go error")
	return doc, diags
}

// requireRefused asserts a compile refused for its $refs' copies: no Document,
// and the refusal alone, at the source and no position in it.
func requireRefused(t *testing.T, doc *ir.Document, diags []ir.Diagnostic, limit string) {
	t.Helper()
	assert.Nil(t, doc)
	require.Len(t, diags, 1, "the refusal alone: %+v", diags)
	assert.Equal(t, ir.SeverityError, diags[0].Severity)
	assert.Equal(t, diag.BudgetExceeded, diags[0].Code)
	assert.Equal(t, ir.Provenance{Source: 0}, diags[0].Provenance)
	assert.Contains(t, diags[0].Message, "the "+limit+" budgeted")
}

// pathsWith returns a 3.1 document of the paths given, or of none, then the
// components.
func pathsWith(paths, components string) string {
	head := "openapi: 3.1.0\ninfo: {title: T, version: \"1\"}\npaths:"
	if paths == "" {
		return head + " {}\ncomponents:\n" + components
	}
	return head + "\n" + paths + "components:\n" + components
}

// getWith returns a path item at path holding one get operation of the body
// given, indented beneath it.
func getWith(path, body string) string {
	return "  " + path + ":\n    get:\n" + body
}

const okResponses = "      responses: {'200': {description: ok}}\n"

// paramP is a component parameter weighing 37: a mapping (1), the keys name (5),
// in (3) and schema (7), their values q (2), query (6) and a mapping (1)
// holding type (5) and string (7).
const paramP = "  parameters:\n    P: {name: q, in: query, schema: {type: string}}\n"

// TestCompile_ReferenceExpansionBindsOnlyPastTheLimit pins the charge at its
// boundary: two $refs to one parameter cost twice what it weighs, the first
// copy among them.
func TestCompile_ReferenceExpansionBindsOnlyPastTheLimit(t *testing.T) {
	t.Parallel()
	use := "      parameters: [{$ref: '#/components/parameters/P'}]\n" + okResponses
	src := pathsWith(getWith("/a", "      operationId: a\n"+use)+getWith("/b", "      operationId: b\n"+use), paramP)

	doc, diags := compileExpanding(t, src, 2*37)
	require.NotNil(t, doc, "copies weighing the budget are within it")
	openapitest.RequireNoErrorDiags(t, diags)
	assert.Len(t, openapitest.FindOp(t, doc, "b").Params, 1, "the second copy is lowered")

	doc, diags = compileExpanding(t, src, 2*37-1)
	requireRefused(t, doc, diags, "73")
}

// TestCompile_EveryCopiedDeclarationIsCharged runs a $ref at each position
// the lowering copies what one names, under a budget no declaration fits:
// each is refused, and admitted with the budget off. A schema's $ref names one
// registry entry, and a link's is kept as written, so neither is charged.
func TestCompile_EveryCopiedDeclarationIsCharged(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		src     string
		charged bool
	}{
		{"a parameter", pathsWith(getWith("/p", "      parameters: [{$ref: '#/components/parameters/P'}]\n"+okResponses),
			paramP), true},
		{"a header", pathsWith(getWith("/p", "      responses:\n        '200':\n          description: ok\n"+
			"          headers: {H: {$ref: '#/components/headers/H'}}\n"),
			"  headers:\n    H: {schema: {type: string}}\n"), true},
		{"a response", pathsWith(getWith("/p", "      responses: {'200': {$ref: '#/components/responses/R'}}\n"),
			"  responses:\n    R: {description: ok}\n"), true},
		{"a default response", pathsWith(getWith("/p", "      responses: {default: {$ref: '#/components/responses/R'}}\n"),
			"  responses:\n    R: {description: ok}\n"), true},
		{"a request body", pathsWith(getWith("/p", "      requestBody: {$ref: '#/components/requestBodies/B'}\n"+okResponses),
			"  requestBodies:\n    B: {content: {application/json: {schema: {type: string}}}}\n"), true},
		{"an example", pathsWith(getWith("/p", "      parameters: [{name: q, in: query, schema: {type: string}, "+
			"examples: {e: {$ref: '#/components/examples/E'}}}]\n"+okResponses),
			"  examples:\n    E: {value: 1}\n"), true},
		{"a media type", strings.Replace(pathsWith(getWith("/p",
			"      requestBody: {content: {application/json: {$ref: '#/components/mediaTypes/M'}}}\n"+okResponses),
			"  mediaTypes:\n    M: {schema: {type: string}}\n"), "3.1.0", "3.2.0", 1), true},
		{"a callback", pathsWith(getWith("/p", "      callbacks: {c: {$ref: '#/components/callbacks/C'}}\n"+okResponses),
			"  callbacks:\n    C: {'{$request.body#/u}': {post: {responses: {'200': {description: ok}}}}}\n"), true},
		{"a path item", pathsWith("  /p: {$ref: '#/components/pathItems/P'}\n",
			"  pathItems:\n    P: {get: {responses: {'200': {description: ok}}}}\n"), true},
		{"a webhook", strings.Replace(pathsWith("", "  pathItems:\n    P: {post: {responses: {'200': {description: ok}}}}\n"),
			"components:\n", "webhooks:\n  w: {$ref: '#/components/pathItems/P'}\ncomponents:\n", 1), true},
		{"a security scheme", pathsWith("", "  securitySchemes:\n    A: {$ref: '#/components/securitySchemes/B'}\n"+
			"    B: {type: http, scheme: basic}\n"), true},
		{"a schema", pathsWith(getWith("/p", "      parameters: [{name: q, in: query, schema: {$ref: '#/components/schemas/S'}}]\n"+
			okResponses), "  schemas:\n    S: {type: string}\n"), false},
		{"a link", pathsWith(getWith("/p", "      responses:\n        '200':\n          description: ok\n"+
			"          links: {l: {$ref: '#/components/links/L'}}\n"), "  links:\n    L: {operationId: x}\n"), false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc, diags := compileExpanding(t, tc.src, -1)
			require.NotNil(t, doc, "with the budget off: %+v", diags)
			assert.False(t, openapitest.HasDiag(diags, diag.BudgetExceeded))

			doc, diags = compileExpanding(t, tc.src, 1)
			if tc.charged {
				requireRefused(t, doc, diags, "1")
				return
			}
			require.NotNil(t, doc, "%+v", diags)
			assert.False(t, openapitest.HasDiag(diags, diag.BudgetExceeded))
		})
	}
}

// TestCompile_AReferenceExpansionRefusalIsTheSameInEitherOrder pins what a
// refusal reports: nothing the lowering found. Either parameter fits the budget
// alone, so the one lowered first is copied and the other crosses; reporting
// what the copy found would report the reserved header in one order alone.
func TestCompile_AReferenceExpansionRefusalIsTheSameInEitherOrder(t *testing.T) {
	t.Parallel()
	reserved := getWith("/a", "      parameters: [{$ref: '#/components/parameters/Accept'}]\n"+okResponses)
	long := getWith("/b", "      parameters: [{$ref: '#/components/parameters/P'}]\n"+okResponses)
	// Accept weighs 43, as paramP does with Accept and header for q and query.
	// P weighs 90: paramP's 37, a description key (12) and a 40-byte value (41).
	components := "  parameters:\n    Accept: {name: Accept, in: header, schema: {type: string}}\n" +
		"    P: {name: q, in: query, schema: {type: string}, description: " + strings.Repeat("x", 40) + "}\n"

	doc, diags := compileExpanding(t, pathsWith(reserved+long, components), -1)
	require.NotNil(t, doc)
	openapitest.AssertHasCode(t, diags, diag.ReservedHeaderName, ir.SeverityWarning)

	for name, paths := range map[string]string{"reserved first": reserved + long, "reserved last": long + reserved} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			doc, diags := compileExpanding(t, pathsWith(paths, components), 90)
			requireRefused(t, doc, diags, "90")
		})
	}
}

// TestCompile_AReferenceExpansionRefusalKeepsWhatItsTableCanPlace pins what a
// refused document reports besides its refusal: what the load phase found,
// which no $ref's place in the walk changes, in the documents SourceTable
// lists. A refusal's findings are read against that table, which names no
// document a $ref read, so what the load phase found there is left out.
func TestCompile_AReferenceExpansionRefusalKeepsWhatItsTableCanPlace(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	// ext.yaml's P declares neither schema nor content, which the load phase
	// reports there.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "ext.yaml"), []byte("P: {name: q, in: query}\n"), 0o600))
	src := pathsWith(getWith("/p", "      parameters:\n        - $ref: './ext.yaml#/P'\n"+
		"        - $ref: '#/components/parameters/Missing'\n"+okResponses), paramP)
	sources := []compilers.Source{{Path: filepath.Join(dir, "root.yaml"), Data: []byte(src)}}
	compile := func(limit int) (*ir.Document, []ir.Diagnostic, int) {
		opts := compilers.Options{Reader: reader.Disk{},
			FormatOptions: openapi.Options{Limits: openapi.Limits{MaxReferenceExpansion: limit}}}
		doc, diags, err := openapi.New().Compile(t.Context(), sources, opts)
		require.NoError(t, err)
		return doc, diags, len(openapi.New().SourceTable(sources, opts))
	}

	doc, diags, _ := compile(-1)
	require.NotNil(t, doc)
	require.True(t, slices.ContainsFunc(diags, func(d ir.Diagnostic) bool { return d.Provenance.Source == 1 }),
		"the load phase reports in ext.yaml: %+v", diags)

	doc, diags, table := compile(1)
	assert.Nil(t, doc)
	_, outside := ir.FirstOutsideTable(diags, table)
	assert.False(t, outside, "every finding of a refusal indexes its table: %+v", diags)
	require.Len(t, diags, 2, "%+v", diags)
	assert.Equal(t, diag.UnresolvedRef, diags[0].Code, "the load phase's finding in the source comes first")
	assert.Equal(t, diag.BudgetExceeded, diags[1].Code)
}
