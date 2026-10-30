// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test

import (
	"fmt"
	"math/rand/v2"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
)

// refGraphDoc is a document of schemas, path items and parameters whose
// references form loops, tails into loops, pointers passing through other
// references or back through their own, with a trailing empty token or
// without, and $defs and $anchor chains, drawn from r.
func refGraphDoc(r *rand.Rand) string {
	schemas, paths, params := 2+r.IntN(4), r.IntN(4), r.IntN(3)
	var b strings.Builder
	b.WriteString("openapi: 3.1.0\ninfo: {title: t, version: '1'}\npaths: {")
	for i := range paths {
		fmt.Fprintf(&b, "'/p%d': %s, ", i, []string{
			fmt.Sprintf("{$ref: '#/components/pathItems/P%d'}", r.IntN(paths)),
			fmt.Sprintf("{$ref: '#/paths/~1p%d'}", r.IntN(paths)),
			fmt.Sprintf("{$ref: '#/paths/~1p%d/get'}", r.IntN(paths)),
			fmt.Sprintf("{get: {parameters: [{$ref: '#/components/parameters/Q%d'}], responses: {'200': {description: ok}}}}",
				r.IntN(params+1)),
		}[r.IntN(4)])
	}
	b.WriteString("}\ncomponents:\n  schemas: {")
	for i := range schemas {
		j := r.IntN(schemas)
		fmt.Fprintf(&b, "S%d: %s, ", i, []string{
			fmt.Sprintf("{$ref: '#/components/schemas/S%d'}", j),
			fmt.Sprintf("{$ref: '#/components/schemas/S%d'}", r.IntN(schemas)),
			fmt.Sprintf("{$ref: '#/components/schemas/S%d/properties/p'}", j),
			fmt.Sprintf("{$ref: '#/components/schemas/S%d/properties/p/items'}", j),
			fmt.Sprintf("{type: object, properties: {p: {$ref: '#/components/schemas/S%d'}}}", j),
			fmt.Sprintf("{$anchor: a%d, $ref: '#a%d'}", i, j),
			fmt.Sprintf("{$defs: {d: {$ref: '#/components/schemas/S%d'}}, $ref: '#/$defs/d'}", j),
			fmt.Sprintf("{$ref: '#/components/schemas/S%d/properties/p', properties: {p: {$ref: '#/components/schemas/S%d'}}}",
				i, j),
			fmt.Sprintf("{$ref: '#/components/schemas/S%d/properties/', properties: {'': {$ref: '#/components/schemas/S%d'}}}",
				i, j),
			"{type: string}",
		}[r.IntN(10)])
	}
	b.WriteString("}\n  pathItems: {")
	for i := range paths {
		fmt.Fprintf(&b, "P%d: %s, ", i, []string{
			fmt.Sprintf("{$ref: '#/components/pathItems/P%d'}", r.IntN(paths)),
			fmt.Sprintf("{$ref: '#/paths/~1p%d'}", r.IntN(paths)),
			"{get: {responses: {'200': {description: ok}}}}",
		}[r.IntN(3)])
	}
	fmt.Fprintf(&b, "}\n  parameters: {Q%d: {name: q, in: query, schema: {type: string}}", params)
	for i := range params {
		fmt.Fprintf(&b, ", Q%d: %s", i, []string{
			fmt.Sprintf("{$ref: '#/components/parameters/Q%d'}", r.IntN(params+1)),
			fmt.Sprintf("{$ref: '#/components/parameters/Q%d/schema'}", r.IntN(params+1)),
			fmt.Sprintf("{name: q%d, in: query, schema: {$ref: '#/components/schemas/S%d'}}", i, r.IntN(schemas)),
		}[r.IntN(3)])
	}
	b.WriteString("}\n")
	return b.String()
}

// cycleRefusals compiles data and returns its cycle refusals and cycle-scan
// warnings, each as its code, pointer and message, sorted.
func cycleRefusals(t *testing.T, data []byte) []string {
	t.Helper()
	_, diags, err := openapi.New().Compile(t.Context(), []compilers.Source{{Path: "refs.yaml", Data: data}},
		compilers.Options{})
	require.NoError(t, err)
	var out []string
	for _, d := range diags {
		if d.Code == diag.CyclicRef || d.Code == diag.CycleScanFailed {
			out = append(out, fmt.Sprintf("%s %s %s", d.Code, d.Provenance.Pointer, d.Message))
		}
	}
	slices.Sort(out)
	return out
}

// TestCompile_ACycleRefusalIgnoresDeclarationOrder is GitHub #912 across both
// checks that refuse a cycle, the pre-parse scan and reach: generated
// reference graphs compile to the same refusals, at the same sites, with
// every mapping's entries reversed. Only a schema cycle is refused; one of path
// items or parameters is the resolver's to report. The harness's order oracle
// stops at a spec's first error (GitHub #685), so it cannot see this.
func TestCompile_ACycleRefusalIgnoresDeclarationOrder(t *testing.T) {
	t.Parallel()
	const docs = 1500
	r := rand.New(rand.NewPCG(912, 934))
	refused := 0
	for range docs {
		src := refGraphDoc(r)
		asWritten := cycleRefusals(t, []byte(src))
		require.Equal(t, asWritten, cycleRefusals(t, reversed(t, src)), "in\n%s", src)
		if len(asWritten) > 0 {
			refused++
		}
	}
	assert.Greater(t, refused, docs/3, "a third of generated graphs hold a schema cycle, so the sites are compared")
}
