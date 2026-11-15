// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/compilers/openapi/internal/schema"
)

// TestLower_AChainEndTheConjunctBudgetCutsShortIsReadAgain pins that what a
// reference site lands on is kept for the later sites of its chain only where
// the read that made it ran within its conjunct budget: two properties naming
// a chain of two aliases over a schema of a few members read each hop once, and
// over one of more members than the budget (256) reads the hops again for the
// second property, as a read with a budget of its own may read the rest.
func TestLower_AChainEndTheConjunctBudgetCutsShortIsReadAgain(t *testing.T) {
	t.Parallel()
	reads := func(members int) int {
		end := strings.TrimSuffix(strings.Repeat("{type: string}, ", members), ", ")
		src := "openapi: 3.1.0\ninfo: {title: t, version: '1'}\npaths: {}\ncomponents:\n  schemas:\n" +
			"    B1: {type: object, properties: {p: {$ref: '#/components/schemas/Chain'}}}\n" +
			"    B2: {type: object, properties: {p: {$ref: '#/components/schemas/Chain'}}}\n" +
			"    Chain: {$ref: '#/components/schemas/Mid'}\n" +
			"    Mid: {$ref: '#/components/schemas/End'}\n" +
			"    End: {allOf: [" + end + "]}\n"
		_, _, count := schema.LowerSpecCountingLands(t, src)
		return count
	}

	within := reads(10)
	cut := reads(300)

	assert.Equal(t, 3, within, "read once per hop")
	assert.Greater(t, cut, within, "a reading the budget cut short is read again by the next site")
}
