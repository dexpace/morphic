// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package lowering_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/ir"
)

// TestPageStatementOf_ReadsAnExpressionOrAMark pins the two ways a source
// of a pagination target speaks: one listing no values holds an expression as
// text, and one listing values marks the node it sits on where its value is
// one of them. Neither resolves anything here; the operation's walk does.
func TestPageStatementOf_ReadsAnExpressionOrAMark(t *testing.T) {
	t.Parallel()
	expression := lowering.ExtensionSource{Key: "x-page", Member: "cursor"}
	mark := lowering.ExtensionSource{Key: "x-role", Member: "purpose", Values: []string{"cursor"}}
	p := promoter(mapping(lowering.TargetPaginationCursor, expression, mark))
	assert.Equal(t, []lowering.ExtensionSource{expression, mark}, p.Sources(lowering.TargetPaginationCursor))

	for _, tc := range []struct {
		name     string
		src      lowering.ExtensionSource
		value    string
		want     lowering.PageStatement
		reported bool
	}{
		{"an expression", expression, `{"cursor":"$request.c"}`,
			lowering.PageStatement{Expression: "$request.c", Stated: true}, false},
		{"an expression of no text", expression, `{"cursor":3}`, lowering.PageStatement{}, true},
		{"a mark", mark, `{"purpose":"cursor"}`, lowering.PageStatement{Stated: true}, false},
		{"a mark of another word", mark, `{"purpose":"items"}`, lowering.PageStatement{}, true},
		{"neither, where the member is absent", mark, `{}`, lowering.PageStatement{}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			entries := ir.Unmodeled{"openapi:" + tc.src.Key: vendorExtension(tc.value)}
			got, diags := lowering.PageStatementOf(p, lowering.TargetPaginationCursor, tc.src, entries)
			assert.Equal(t, tc.want.Expression, got.Expression)
			assert.Equal(t, tc.want.Stated, got.Stated)
			require.Equal(t, tc.reported, len(diags) == 1, "reports: %+v", diags)
			if got.Stated {
				assert.Equal(t, "/components/schemas/S/"+tc.src.Member, string(got.At.Pointer))
			}
		})
	}
}
