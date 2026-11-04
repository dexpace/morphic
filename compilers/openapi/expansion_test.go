// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package openapi_test

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "go.yaml.in/yaml/v3"

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

// refusal is the error refusing a document whose $refs copied past limit: at
// the source, and no position in it.
func refusal(limit int) ir.Diagnostic {
	return ir.Diagnostic{Severity: ir.SeverityError, Code: diag.BudgetExceeded, Provenance: ir.Provenance{Source: 0},
		Message: fmt.Sprintf("the copies its $refs make weigh more than the %d budgeted for them: each $ref to "+
			"a declaration other than a schema or a link lowers its own copy of it", limit)}
}

// assertRefused asserts a compile refused for its $refs' copies: no Document,
// and the refusal alone.
func assertRefused(t *testing.T, doc *ir.Document, diags []ir.Diagnostic, limit int) {
	t.Helper()
	assert.Nil(t, doc)
	assert.Equal(t, []ir.Diagnostic{refusal(limit)}, diags)
}

// weightAt weighs the declaration at pointer in src as MaxReferenceExpansion's
// documentation says, apart from the compiler's own weigher: one for each YAML
// node, an alias counting as a copy of what it names, one for each byte of a
// scalar's text, and one for a schema whatever it holds. A schema is the value
// of a schema or itemSchema key, which no document here writes elsewhere.
func weightAt(t *testing.T, src, pointer string) int {
	t.Helper()
	var root yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte(src), &root))
	n := root.Content[0]
	for _, token := range strings.Split(pointer, "/")[1:] {
		key := strings.ReplaceAll(strings.ReplaceAll(token, "~1", "/"), "~0", "~")
		i := slices.IndexFunc(n.Content, func(k *yaml.Node) bool { return k.Value == key })
		require.True(t, i >= 0 && i%2 == 0, "%s has no %q", pointer, key)
		n = n.Content[i+1]
	}
	var weigh func(*yaml.Node) int
	weigh = func(n *yaml.Node) int {
		if n.Kind == yaml.AliasNode {
			return weigh(n.Alias)
		}
		w := 1 + len(n.Value)
		for i, c := range n.Content {
			schema := n.Kind == yaml.MappingNode && i%2 == 1 &&
				(n.Content[i-1].Value == "schema" || n.Content[i-1].Value == "itemSchema")
			if schema {
				w++
				continue
			}
			w += weigh(c)
		}
		return w
	}
	return weigh(n)
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

// paramP is a component parameter weighing 25: a mapping (1), the keys name (5),
// in (3) and schema (7), and their values q (2), query (6) and a schema (1).
const paramP = "  parameters:\n    P: {name: q, in: query, schema: {type: string}}\n"

// TestCompile_ReferenceExpansionBindsOnlyPastTheLimit pins the charge at its
// boundary: two $refs to one parameter cost twice what it weighs, the first
// copy among them.
func TestCompile_ReferenceExpansionBindsOnlyPastTheLimit(t *testing.T) {
	t.Parallel()
	use := "      parameters: [{$ref: '#/components/parameters/P'}]\n" + okResponses
	src := pathsWith(getWith("/a", "      operationId: a\n"+use)+getWith("/b", "      operationId: b\n"+use), paramP)

	doc, diags := compileExpanding(t, src, 2*25)
	require.NotNil(t, doc, "copies weighing the budget are within it")
	openapitest.RequireNoErrorDiags(t, diags)
	assert.Len(t, openapitest.FindOp(t, doc, "b").Params, 1, "the second copy is lowered")

	doc, diags = compileExpanding(t, src, 2*25-1)
	assertRefused(t, doc, diags, 49)
}

// TestCompile_EveryCopiedDeclarationIsCharged runs a $ref at each position
// the lowering copies what one names: each is admitted at the weight of the
// declaration at its target, as weightAt weighs it apart from the compiler,
// and refused one below, so a position charged twice or not at all fails. A
// schema's $ref names one registry entry, and a link's is kept as written, so
// neither is charged, and each is admitted under a budget of one.
func TestCompile_EveryCopiedDeclarationIsCharged(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		src    string
		target string
	}{
		{"a parameter", pathsWith(getWith("/p", "      parameters: [{$ref: '#/components/parameters/P'}]\n"+okResponses),
			paramP), "/components/parameters/P"},
		{"a header", pathsWith(getWith("/p", "      responses:\n        '200':\n          description: ok\n"+
			"          headers: {H: {$ref: '#/components/headers/H'}}\n"),
			"  headers:\n    H: {schema: {type: string}}\n"), "/components/headers/H"},
		{"a response", pathsWith(getWith("/p", "      responses: {'200': {$ref: '#/components/responses/R'}}\n"),
			"  responses:\n    R: {description: ok}\n"), "/components/responses/R"},
		{"a default response", pathsWith(getWith("/p", "      responses: {default: {$ref: '#/components/responses/R'}}\n"),
			"  responses:\n    R: {description: ok}\n"), "/components/responses/R"},
		{"a request body", pathsWith(getWith("/p", "      requestBody: {$ref: '#/components/requestBodies/B'}\n"+okResponses),
			"  requestBodies:\n    B: {content: {application/json: {schema: {type: string}}}}\n"), "/components/requestBodies/B"},
		{"an example", pathsWith(getWith("/p", "      parameters: [{name: q, in: query, schema: {type: string}, "+
			"examples: {e: {$ref: '#/components/examples/E'}}}]\n"+okResponses),
			"  examples:\n    E: {value: 1}\n"), "/components/examples/E"},
		{"a media type", strings.Replace(pathsWith(getWith("/p",
			"      requestBody: {content: {application/json: {$ref: '#/components/mediaTypes/M'}}}\n"+okResponses),
			"  mediaTypes:\n    M: {schema: {type: string}}\n"), "3.1.0", "3.2.0", 1), "/components/mediaTypes/M"},
		{"a callback", pathsWith(getWith("/p", "      callbacks: {c: {$ref: '#/components/callbacks/C'}}\n"+okResponses),
			"  callbacks:\n    C: {'{$request.body#/u}': {post: {responses: {'200': {description: ok}}}}}\n"), "/components/callbacks/C"},
		{"a path item", pathsWith("  /p: {$ref: '#/components/pathItems/P'}\n",
			"  pathItems:\n    P: {get: {responses: {'200': {description: ok}}}}\n"), "/components/pathItems/P"},
		{"a webhook", strings.Replace(pathsWith("", "  pathItems:\n    P: {post: {responses: {'200': {description: ok}}}}\n"),
			"components:\n", "webhooks:\n  w: {$ref: '#/components/pathItems/P'}\ncomponents:\n", 1), "/components/pathItems/P"},
		{"a security scheme", pathsWith("", "  securitySchemes:\n    A: {$ref: '#/components/securitySchemes/B'}\n"+
			"    B: {type: http, scheme: basic}\n"), "/components/securitySchemes/B"},
		{"a schema", pathsWith(getWith("/p", "      parameters: [{name: q, in: query, schema: {$ref: '#/components/schemas/S'}}]\n"+
			okResponses), "  schemas:\n    S: {type: string}\n"), ""},
		{"a link", pathsWith(getWith("/p", "      responses:\n        '200':\n          description: ok\n"+
			"          links: {l: {$ref: '#/components/links/L'}}\n"), "  links:\n    L: {operationId: x}\n"), ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc, diags := compileExpanding(t, tc.src, -1)
			require.NotNil(t, doc, "with the budget off: %+v", diags)
			assert.False(t, openapitest.HasDiag(diags, diag.BudgetExceeded))

			if tc.target == "" {
				doc, diags = compileExpanding(t, tc.src, 1)
				require.NotNil(t, doc, "%+v", diags)
				assert.False(t, openapitest.HasDiag(diags, diag.BudgetExceeded))
				return
			}
			weight := weightAt(t, tc.src, tc.target)
			doc, diags = compileExpanding(t, tc.src, weight)
			require.NotNil(t, doc, "at the declaration's weight, %d: %+v", weight, diags)
			assert.False(t, openapitest.HasDiag(diags, diag.BudgetExceeded))
			doc, diags = compileExpanding(t, tc.src, weight-1)
			assertRefused(t, doc, diags, weight-1)
		})
	}
}

// TestCompile_AReferenceIntoAnotherDeclarationIsChargedInFull pins the charge
// of a $ref naming a declaration nested in another one a $ref names: the IR
// holds the inner one twice, once inside each copy, so each $ref is charged
// its own declaration's weight, though the outer copy already weighed the inner.
func TestCompile_AReferenceIntoAnotherDeclarationIsChargedInFull(t *testing.T) {
	t.Parallel()
	src := pathsWith(getWith("/a", "      responses: {'200': {$ref: '#/components/responses/R'}}\n")+
		getWith("/b", "      responses:\n        '200':\n          description: ok\n"+
			"          headers: {H: {$ref: '#/components/responses/R/headers/H'}}\n"),
		"  responses:\n    R:\n      description: ok\n      headers: {H: {schema: {type: string}}}\n")
	both := weightAt(t, src, "/components/responses/R") + weightAt(t, src, "/components/responses/R/headers/H")

	doc, diags := compileExpanding(t, src, both)
	require.NotNil(t, doc, "%+v", diags)
	assert.False(t, openapitest.HasDiag(diags, diag.BudgetExceeded))

	doc, diags = compileExpanding(t, src, both-1)
	assertRefused(t, doc, diags, both-1)
}

// TestCompile_ASchemaIsChargedAsOneNode pins the charge of a declaration
// holding a schema: one node whatever the schema holds, since it lowers once, to
// a type every copy refers to. Charged in full, the schema's twenty properties
// would cross this budget at the first $ref.
func TestCompile_ASchemaIsChargedAsOneNode(t *testing.T) {
	t.Parallel()
	props := make([]string, 20)
	for j := range props {
		props[j] = fmt.Sprintf("p%d: {type: string}", j)
	}
	use := "      responses: {'200': {$ref: '#/components/responses/R'}}\n"
	src := pathsWith(getWith("/a", "      operationId: a\n"+use)+getWith("/b", "      operationId: b\n"+use),
		"  responses:\n    R: {description: ok, content: {application/json: {schema: {type: object, properties: {"+
			strings.Join(props, ", ")+"}}}}}\n")
	weight := weightAt(t, src, "/components/responses/R")

	doc, diags := compileExpanding(t, src, 2*weight)
	require.NotNil(t, doc, "%+v", diags)
	assert.False(t, openapitest.HasDiag(diags, diag.BudgetExceeded))
	bodyType := func(op string) ir.TypeRef {
		resps := openapitest.FindOp(t, doc, op).Responses
		require.Len(t, resps, 1)
		require.NotNil(t, resps[0].Payload)
		require.Len(t, resps[0].Payload.Contents, 1)
		return resps[0].Payload.Contents[0].Type
	}
	assert.Equal(t, bodyType("a"), bodyType("b"), "both copies refer to one type")

	doc, diags = compileExpanding(t, src, 2*weight-1)
	assertRefused(t, doc, diags, 2*weight-1)
}

// TestCompile_AnAliasIsChargedBesideItsEnd pins the charge of a $ref whose
// chain passes through an alias: what the alias writes beside its own $ref
// lowers at every use, a parameter's description and a path item's every
// field, so the alias is charged its weight beside its end's.
func TestCompile_AnAliasIsChargedBesideItsEnd(t *testing.T) {
	t.Parallel()
	param := pathsWith(getWith("/p", "      parameters: [{$ref: '#/components/parameters/A'}]\n"+okResponses),
		"  parameters:\n"+
			"    A: {$ref: '#/components/parameters/B', description: "+strings.Repeat("d", 40)+"}\n"+
			"    B: {name: q, in: query, schema: {type: string}}\n")
	item := pathsWith("  /p: {$ref: '#/components/pathItems/A'}\n", "  pathItems:\n"+
		"    A: {$ref: '#/components/pathItems/B', post: {responses: {'200': {description: ok}}}}\n"+
		"    B: {get: {responses: {'200': {description: ok}}}}\n")
	tests := []struct {
		name, src, alias, end string
	}{
		{"a parameter's description", param, "/components/parameters/A", "/components/parameters/B"},
		{"a path item's operation", item, "/components/pathItems/A", "/components/pathItems/B"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc, diags := compileExpanding(t, tc.src, -1)
			require.NotNil(t, doc, "with the budget off: %+v", diags)
			openapitest.RequireNoErrorDiags(t, diags)

			weight := weightAt(t, tc.src, tc.alias) + weightAt(t, tc.src, tc.end)
			doc, diags = compileExpanding(t, tc.src, weight)
			require.NotNil(t, doc, "at the chain's weight, %d: %+v", weight, diags)
			assert.False(t, openapitest.HasDiag(diags, diag.BudgetExceeded))
			doc, diags = compileExpanding(t, tc.src, weight-1)
			assertRefused(t, doc, diags, weight-1)
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
	// Accept weighs 31, as paramP does with Accept and header for q and query.
	// P weighs 78: paramP's 25, a description key (12) and a 40-byte value (41).
	components := "  parameters:\n    Accept: {name: Accept, in: header, schema: {type: string}}\n" +
		"    P: {name: q, in: query, schema: {type: string}, description: " + strings.Repeat("x", 40) + "}\n"

	doc, diags := compileExpanding(t, pathsWith(reserved+long, components), -1)
	require.NotNil(t, doc)
	openapitest.AssertHasCode(t, diags, diag.ReservedHeaderName, ir.SeverityWarning)

	for name, paths := range map[string]string{"reserved first": reserved + long, "reserved last": long + reserved} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			doc, diags := compileExpanding(t, pathsWith(paths, components), 78)
			assertRefused(t, doc, diags, 78)
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
	assert.Zero(t, diags[0].Provenance.Source)
	assert.Equal(t, refusal(1), diags[1])
}
