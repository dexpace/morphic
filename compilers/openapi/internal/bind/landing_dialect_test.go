// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package bind_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/bind"
)

// TestDialectRule_ALandingNamingNoDialectRootsNone pins that a `$schema` whose
// value the configuration says names no dialect (DialectRule.NamesNone), in a
// schema a $ref lands on in content no grammar reads, roots no dialect there,
// as one in the source roots none; and that one the configuration says names a
// dialect roots it.
func TestDialectRule_ALandingNamingNoDialectRootsNone(t *testing.T) {
	t.Parallel()
	const src = head + "components:\n  schemas:\n    S: {$ref: 'o.yaml#/x-lib/X'}\n"
	docs := map[string]string{"o.yaml": head + "x-lib:\n  X: {$schema: 'rel/x', type: string}\n"}
	dialectsOf := func(none bool) []bind.DialectRoot {
		cfg := withDocuments(docs)
		cfg.Dialect = func(_, dialect string) bind.DialectRule {
			return bind.DialectRule{NamesNone: none && dialect == "rel/x"}
		}
		table := bound(t, src, cfg)
		o := docNamed(t, table, "o.yaml")
		e := entryAt(t, table, "/components/schemas/S")
		require.Equal(t, bind.Resolved, e.Hop.Status, e.Hop.Why())
		return table.Document(o).Dialects
	}

	assert.Empty(t, dialectsOf(true), "the value roots no dialect")
	named := dialectsOf(false)
	require.Len(t, named, 1, "a value naming a dialect roots it at the landing")
	assert.Equal(t, "rel/x", named[0].URI)
}
