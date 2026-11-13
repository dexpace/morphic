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
// one of them. Neither resolves anything here; the operation's walk does. A
// key the node keeps is written whatever its value says, and one it does not
// keep is not.
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
			lowering.PageStatement{Expression: "$request.c", Stated: true, Written: true}, false},
		{"an expression of no text", expression, `{"cursor":3}`, lowering.PageStatement{Written: true}, true},
		{"an expression key written false", expression, `false`, lowering.PageStatement{Written: true}, true},
		{"an expression key with no member", expression, `{}`, lowering.PageStatement{Written: true}, false},
		{"a mark", mark, `{"purpose":"cursor"}`, lowering.PageStatement{Stated: true, Written: true}, false},
		{"a mark of another word", mark, `{"purpose":"items"}`, lowering.PageStatement{Written: true}, true},
		{"a mark written null", mark, `null`, lowering.PageStatement{Written: true}, true},
		{"neither, where the member is absent", mark, `{}`, lowering.PageStatement{Written: true}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			entries := ir.Unmodeled{"openapi:" + tc.src.Key: vendorExtension(tc.value)}
			got, diags := lowering.PageStatementOf(p, lowering.TargetPaginationCursor, tc.src, entries)
			assert.Equal(t, tc.want.Expression, got.Expression)
			assert.Equal(t, tc.want.Stated, got.Stated)
			assert.Equal(t, tc.want.Written, got.Written)
			require.Equal(t, tc.reported, len(diags) == 1, "reports: %+v", diags)
			if got.Stated {
				assert.Equal(t, "/components/schemas/S/"+tc.src.Member, string(got.At.Pointer))
			}
		})
	}
	for _, src := range []lowering.ExtensionSource{expression, mark} {
		got, diags := lowering.PageStatementOf(p, lowering.TargetPaginationCursor, src,
			ir.Unmodeled{"openapi:x-other": vendorExtension(`{"cursor":"$request.c","purpose":"cursor"}`)})
		assert.Equal(t, lowering.PageStatement{}, got, "a node keeping another key: %s", src)
		assert.Empty(t, diags)
	}
}
