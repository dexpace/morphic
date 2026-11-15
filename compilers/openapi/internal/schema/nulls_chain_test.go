// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/schema"
	"github.com/dexpace/morphic/ir"
)

// refChain is n component schemas each `$ref`ing the next, ending on a string
// (#1652's generator).
func refChain(n int) string {
	var b strings.Builder
	b.WriteString("openapi: 3.1.0\ninfo: {title: t, version: '1'}\npaths: {}\ncomponents:\n  schemas:\n")
	for i := range n {
		fmt.Fprintf(&b, "    S%d: {$ref: '#/components/schemas/S%d'}\n", i, i+1)
	}
	fmt.Fprintf(&b, "    S%d: {type: string}\n", n)
	return b.String()
}

// TestLower_ARefChainIsReadOncePerHop pins the cost of reading what a chain
// of aliases says about null: every hop is read once per compile, whichever
// of the chain's sites asks first, so n sites cost n-1 readings at each size,
// not n squared over two. The seam assumed is schema.LowerSpecCountingLands,
// which lowers src as LowerSpec does and also returns how many times a hop's
// reading was made while reading what a $ref lands on.
func TestLower_ARefChainIsReadOncePerHop(t *testing.T) {
	t.Parallel()
	for _, n := range []int{1000, 2000, 4000} {
		doc, diags, reads := schema.LowerSpecCountingLands(t, refChain(n))
		require.Empty(t, diags, "n=%d", n)
		assert.Equal(t, n-1, reads, "n=%d", n)

		for _, i := range []int{0, n / 2, n - 1} {
			sc, ok := doc.Types[ir.TypeID(fmt.Sprintf("t/openapi/components/schemas/S%d", i))].(*ir.Scalar)
			require.True(t, ok, "S%d", i)
			require.NotNil(t, sc.Base, "S%d", i)
			next := ir.TypeID(fmt.Sprintf("t/openapi/components/schemas/S%d", i+1))
			assert.Equal(t, next, sc.Base.Target, "n=%d: S%d is based on the component it names, as before", n, i)
		}
	}
}
