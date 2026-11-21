// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test

import (
	"fmt"
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
	"github.com/dexpace/morphic/ir"
)

// pathItemChain returns n path items, /p0 to /p(n-1), each a $ref to the next,
// and /pn, which declares one operation: n+1 mounts of it, the first n hops
// from it.
func pathItemChain(n int) string {
	var b strings.Builder
	b.WriteString("openapi: 3.1.0\ninfo: {title: t, version: \"1\"}\npaths:\n")
	for i := range n {
		fmt.Fprintf(&b, "  /p%d: {$ref: \"#/paths/~1p%d\"}\n", i, i+1)
	}
	fmt.Fprintf(&b, "  /p%d: {get: {responses: {\"200\": {description: ok}}}}\n", n)
	return b.String()
}

// parameterChain returns n+1 operations, each using the parameter P0, which is
// a $ref to P1 and so on down to Pn, which declares the parameter q.
func parameterChain(n int) string {
	var b strings.Builder
	b.WriteString("openapi: 3.1.0\ninfo: {title: t, version: \"1\"}\npaths:\n")
	for i := range n + 1 {
		fmt.Fprintf(&b, "  /o%d: {get: {parameters: [{$ref: \"#/components/parameters/P%d\"}], "+
			"responses: {\"200\": {description: ok}}}}\n", i, i)
	}
	b.WriteString("components:\n  parameters:\n")
	for i := range n {
		fmt.Fprintf(&b, "    P%d: {$ref: '#/components/parameters/P%d'}\n", i, i+1)
	}
	fmt.Fprintf(&b, "    P%d: {name: q, in: query, schema: {type: string}}\n", n)
	return b.String()
}

// TestCompile_ALongRefChainCompiles pins that a chain of $refs through
// declarations other than schemas costs the budget what its hops copy, which
// for a hop writing nothing beside its $ref is its mapping alone: chains of
// thousands compile with no finding, each use mounting the end's operation or
// taking its parameter, while one far past the budget is refused, and at no
// length does a document lose a use.
func TestCompile_ALongRefChainCompiles(t *testing.T) {
	t.Parallel()
	for _, n := range []int{1, 2, 1500, 2000} {
		t.Run(fmt.Sprintf("path items, %d hops", n), func(t *testing.T) {
			t.Parallel()
			doc, diags := parseVerified(t, pathItemChain(n))
			assert.Empty(t, diags)
			ids := operationIDs(doc)
			assert.Len(t, ids, n+1, "each path mounts the end's operation")
			assert.Len(t, slices.Compact(slices.Sorted(slices.Values(ids))), n+1, "no two operations share an ID")
		})
	}
	for _, n := range []int{1, 2, 1000} {
		t.Run(fmt.Sprintf("parameters, %d hops", n), func(t *testing.T) {
			t.Parallel()
			doc, diags := parseVerified(t, parameterChain(n))
			assert.Empty(t, diags)
			operations := allOperations(doc)
			require.Len(t, operations, n+1)
			for _, operation := range operations {
				require.Len(t, operation.Params, 1, "%s", operation.ID)
				assert.Equal(t, "q", operation.Params[0].Name.Source)
			}
		})
	}
}

// TestCompile_AChainFarPastTheExpansionBudgetIsRefused is the other side of
// the chains above: the weight of a chain grows with the square of its length,
// so a long enough one is refused, once, with no document.
func TestCompile_AChainFarPastTheExpansionBudgetIsRefused(t *testing.T) {
	t.Parallel()
	for name, src := range map[string]string{
		"path items": pathItemChain(8000),
		"parameters": parameterChain(8000),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			doc, diags := parseVerified(t, src)
			assert.Nil(t, doc)
			require.Len(t, diags, 1)
			assert.Equal(t, diag.BudgetExceeded, diags[0].Code)
			assert.Equal(t, ir.SeverityError, diags[0].Severity)
		})
	}
}

// TestCompile_ALongRefChainAcrossTwoDocumentsCompiles is a chain of path items
// whose first hops are written here and the rest in another file, which ends on
// the operation every path mounts.
func TestCompile_ALongRefChainAcrossTwoDocumentsCompiles(t *testing.T) {
	t.Parallel()
	for _, n := range []int{1, 2, 600} {
		t.Run(fmt.Sprintf("%d hops in each", n), func(t *testing.T) {
			t.Parallel()
			var root, other strings.Builder
			root.WriteString("openapi: 3.1.0\ninfo: {title: t, version: \"1\"}\npaths:\n")
			for i := range n {
				next := fmt.Sprintf("\"#/paths/~1p%d\"", i+1)
				if i == n-1 {
					next = "\"other.yaml#/chain/c0\""
				}
				fmt.Fprintf(&root, "  /p%d: {$ref: %s}\n", i, next)
			}
			root.WriteString("  /p" + fmt.Sprint(n) + ": {$ref: \"other.yaml#/chain/c0\"}\n")
			other.WriteString("chain:\n")
			for i := range n {
				fmt.Fprintf(&other, "  c%d: {$ref: \"#/chain/c%d\"}\n", i, i+1)
			}
			fmt.Fprintf(&other, "  c%d: {get: {responses: {\"200\": {description: ok}}}}\n", n)

			dir := t.TempDir()
			layout(t, dir, map[string]string{"other.yaml": other.String()})
			doc, diags := compileVerified(t,
				compilers.Source{Path: filepath.Join(dir, "root.yaml"), Data: []byte(root.String())},
				openapi.Options{AllowExternalRefs: true})
			require.NotNil(t, doc)
			openapitest.RequireNoErrorDiags(t, diags)
			assert.Len(t, operationIDs(doc), n+1)
		})
	}
}
