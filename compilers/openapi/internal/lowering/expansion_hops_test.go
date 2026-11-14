// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package lowering_test

import (
	"fmt"
	"strings"
	"testing"

	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/budget"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
)

// chainedParameter returns a path using a parameter through aliases aliases, one
// of which writes the description, and the end they name, which weighs 25: a
// mapping (1), name (5), q (2), in (3), query (6), schema (7) and the schema (1).
func chainedParameter(aliases int, description string) string {
	var b strings.Builder
	b.WriteString(`openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /a:
    get:
      parameters: [{$ref: '#/components/parameters/A0'}]
      responses: {"200": {description: ok}}
components:
  parameters:
`)
	for i := range aliases {
		fmt.Fprintf(&b, "    A%d: {$ref: '#/components/parameters/A%d'%s}\n", i, i+1, description)
	}
	fmt.Fprintf(&b, "    A%d: {name: q, in: query, schema: {type: string, maxLength: 3}}\n", aliases)
	return b.String()
}

// admittedUnder reports whether the use of the parameter through src's aliases
// resolves with its $refs' copies held to limit.
func admittedUnder(t *testing.T, src string, limit int) bool {
	t.Helper()
	c := loaded(t, src, budget.AtMost(limit))
	a, ok := c.Doc.Paths.Get("/a")
	require.True(t, ok)
	p, _, _ := lowering.Declared[soa.Parameter](c, a.GetObject().Get().GetParameters()[0], "/paths/~1a/get/parameters/0")
	_, refused := c.ExpansionRefusal()
	require.Equal(t, p == nil, refused, "a copy resolves exactly when it is not refused, under %d", limit)
	return p != nil
}

// leastLimit returns the smallest budget under which src's use resolves, found
// by bisection: admission only ever grows with the budget.
func leastLimit(t *testing.T, src string) int {
	t.Helper()
	low, high := 0, 1000
	require.True(t, admittedUnder(t, src, high), "a budget of %d admits it", high)
	for low < high {
		mid := (low + high) / 2
		if admittedUnder(t, src, mid) {
			high = mid
		} else {
			low = mid + 1
		}
	}
	return low
}

// TestDeclared_ChargesEachAliasWhatItWritesBesideItsRef pins the charge of one
// alias more or less in a chain, which is what the use copies of it: a mapping,
// and nothing for its $ref, whatever the $ref's length, where it writes nothing
// else; a mapping, description (12) and its value (3) where it writes a
// description. The end is charged the same at every length of the chain, so the
// least budget admitting the use grows by exactly that much for each alias.
func TestDeclared_ChargesEachAliasWhatItWritesBesideItsRef(t *testing.T) {
	t.Parallel()
	for name, hop := range map[string]struct {
		beside string
		weight int
	}{
		"a bare alias":                {"", 1},
		"an alias with a description": {", description: dd", 1 + 12 + 3},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			none := leastLimit(t, chainedParameter(0, hop.beside))
			assert.Contains(t, []int{25, 26}, none, "the end alone, weighing 25, and at most one for the use's own mapping")
			for _, aliases := range []int{1, 2, 10} {
				assert.Equal(t, none+aliases*hop.weight, leastLimit(t, chainedParameter(aliases, hop.beside)),
					"%d aliases", aliases)
			}
		})
	}
}
