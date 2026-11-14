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
		name  string
		src   lowering.ExtensionSource
		value string
		want  lowering.PageStatement
	}{
		{"an expression", expression, `{"cursor":"$request.c"}`,
			lowering.PageStatement{Expression: "$request.c", Stated: true, Written: true}},
		{"an expression key written false", expression, `false`, lowering.PageStatement{Written: true}},
		{"a mark", mark, `{"purpose":"cursor"}`, lowering.PageStatement{Stated: true, Written: true}},
		{"a mark key written false", mark, `false`, lowering.PageStatement{Written: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			entries := ir.Unmodeled{"openapi:" + tc.src.Key: vendorExtension(tc.value)}
			got, diags := lowering.PageStatementOf(p, lowering.TargetPaginationCursor, tc.src, entries)
			assert.Equal(t, tc.want.Expression, got.Expression)
			assert.Equal(t, tc.want.Stated, got.Stated)
			assert.Equal(t, tc.want.Written, got.Written)
			assert.Empty(t, diags)
			want := "/components/schemas/S"
			if got.Stated {
				want += "/" + tc.src.Member
			}
			assert.Equal(t, want, string(got.At.Pointer))
		})
	}
	for _, src := range []lowering.ExtensionSource{expression, mark} {
		got, diags := lowering.PageStatementOf(p, lowering.TargetPaginationCursor, src,
			ir.Unmodeled{"openapi:x-other": vendorExtension(`{"cursor":"$request.c","purpose":"cursor"}`)})
		assert.Equal(t, lowering.PageStatement{}, got, "a node keeping another key: %s", src)
		assert.Empty(t, diags)
	}
}

// pageValuesPolicy maps the cursor and the limit each to a member of x-page
// holding an expression and to a word of x-role's purpose, the next link to
// x-link's value whole, and the cursor also to x-mark's word and the limit to
// x-flag's. x-page's description fills a deprecation's message, x-role's next
// member and x-page whole each a page value, and x-role's kind marks the page
// count, so a judgement across the sources of one key shows which it reads.
func pageValuesPolicy() lowering.ExtensionPromotions {
	return lowering.ExtensionPromotions{Targets: map[lowering.ExtensionTarget][]lowering.ExtensionSource{
		lowering.TargetPaginationCursor: {{Key: "x-page", Member: "cursor"},
			{Key: "x-role", Member: "purpose", Values: []string{"cursor"}}, {Key: "x-mark", Values: []string{"cursor"}}},
		lowering.TargetPaginationLimit: {{Key: "x-page", Member: "limit"},
			{Key: "x-role", Member: "purpose", Values: []string{"limit"}}, {Key: "x-flag", Values: []string{"false"}}},
		lowering.TargetPaginationNextLink:   {{Key: "x-link"}},
		lowering.TargetPaginationNextCursor: {{Key: "x-role", Member: "next"}},
		lowering.TargetPaginationTotalCount: {{Key: "x-page"}},
		lowering.TargetPaginationTotalPages: {{Key: "x-role", Member: "kind", Values: []string{"total"}}},
		lowering.TargetDeprecationMessage:   {{Key: "x-page", Member: "description"}},
	}}
}

// TestPageStatementOf_ReportsWhatStatesNoValueOfAWalk pins each value a
// pagination key may hold, as an expression's and as a mark's, to what it
// states and reports. false says nothing is there and reports nothing. A value
// stating nothing to every pagination source reading the key as this one
// does, such as true, null, an object holding no member they read or a word
// none lists, is reported once, at the key or its member, saying that, being
// written, it keeps an inferred pagination off. A value another of them reads
// reports nothing here.
func TestPageStatementOf_ReportsWhatStatesNoValueOfAWalk(t *testing.T) {
	t.Parallel()
	const (
		onOperation = ", and as the key is written, no pagination is inferred for the operation"
		onMark      = ", and as the key is written, no pagination is inferred for an operation reaching it"
		noWalk      = ", so it states no value of a walk"
	)
	var (
		cursor = lowering.ExtensionSource{Key: "x-page", Member: "cursor"}
		role   = lowering.ExtensionSource{Key: "x-role", Member: "purpose", Values: []string{"cursor"}}
		link   = lowering.ExtensionSource{Key: "x-link"}
		mark   = lowering.ExtensionSource{Key: "x-mark", Values: []string{"cursor"}}
		flag   = lowering.ExtensionSource{Key: "x-flag", Values: []string{"false"}}
	)
	p := promoter(pageValuesPolicy())
	for _, tc := range []struct {
		name   string
		member bool // the report sits at the member the source reads
		src    lowering.ExtensionSource
		value  string
		stated bool
		report string
	}{
		{"an expression's false", false, cursor, `false`, false, ""},
		{"an expression's true", false, cursor, `true`, false, `extension "x-page" is true, not an object` + noWalk + onOperation},
		{"an expression's null", false, cursor, `null`, false, `extension "x-page" is null, not an object` + noWalk + onOperation},
		{"an expression's string", false, cursor, `"$request.c"`, false,
			`extension "x-page" is a string, not an object` + noWalk + onOperation},
		{"an expression's number", false, cursor, `3`, false, `extension "x-page" is a number, not an object` + noWalk + onOperation},
		{"an expression's list", false, cursor, `["$request.c"]`, false,
			`extension "x-page" is a list, not an object` + noWalk + onOperation},
		{"an expression's empty object", false, cursor, `{}`, false,
			`extension "x-page" holds none of the members the policy reads (cursor, limit)` + noWalk + onOperation},
		{"an expression's object of a member another target reads", false, cursor, `{"description":"old"}`, false,
			`extension "x-page" holds none of the members the policy reads (cursor, limit)` + noWalk + onOperation},
		{"an expression's object of a member another source reads", false, cursor, `{"limit":"$request.n"}`, false, ""},
		{"an expression of a number", true, cursor, `{"cursor":3}`, false,
			`extension "x-page.cursor" is a number, not a string, so it states no pagination.cursor` + onOperation},
		{"an expression of false", true, cursor, `{"cursor":false}`, false,
			`extension "x-page.cursor" is false, not a string, so it states no pagination.cursor` + onOperation},
		{"an expression of an object", true, cursor, `{"cursor":{}}`, false,
			`extension "x-page.cursor" is an object, not a string, so it states no pagination.cursor` + onOperation},
		{"an empty expression", true, cursor, `{"cursor":""}`, false,
			`extension "x-page.cursor" is an empty string, so it states no pagination.cursor` + onOperation},
		{"a mark's false", false, role, `false`, false, ""},
		{"a mark's true", false, role, `true`, false, `extension "x-role" is true, not an object` + noWalk + onMark},
		{"a mark's null", false, role, `null`, false, `extension "x-role" is null, not an object` + noWalk + onMark},
		{"a mark's string", false, role, `"cursor"`, false, `extension "x-role" is a string, not an object` + noWalk + onMark},
		{"a mark's empty object", false, role, `{}`, false,
			`extension "x-role" holds none of the members the policy reads (kind, purpose)` + noWalk + onMark},
		{"a mark's word", false, role, `{"purpose":"cursor"}`, true, ""},
		{"a mark's word another source lists", false, role, `{"purpose":"limit"}`, false, ""},
		{"a mark's word none lists", true, role, `{"purpose":"items"}`, false,
			`extension "x-role.purpose" holds none of the words the policy lists (cursor, limit)` + noWalk + onMark},
		{"a mark's word another member lists", true, role, `{"purpose":"total"}`, false,
			`extension "x-role.purpose" holds none of the words the policy lists (cursor, limit)` + noWalk + onMark},
		{"a mark's list", true, role, `{"purpose":["cursor"]}`, false,
			`extension "x-role.purpose" holds none of the words the policy lists (cursor, limit)` + noWalk + onMark},
		{"a whole expression", false, link, `"$response.next"`, true, ""},
		{"a whole expression's false", false, link, `false`, false, ""},
		{"a whole expression's true", false, link, `true`, false,
			`extension "x-link" is true, not a string, so it states no pagination.nextLink` + onOperation},
		{"a whole empty expression", false, link, `""`, false,
			`extension "x-link" is an empty string, so it states no pagination.nextLink` + onOperation},
		{"a whole mark's word", false, mark, `"cursor"`, true, ""},
		{"a whole mark's false", false, mark, `false`, false, ""},
		{"a whole mark's word none lists", false, mark, `"limit"`, false,
			`extension "x-mark" holds none of the words the policy lists (cursor)` + noWalk + onMark},
		{"a whole mark's null", false, mark, `null`, false,
			`extension "x-mark" holds none of the words the policy lists (cursor)` + noWalk + onMark},
		{"a whole mark listing false", false, flag, `false`, true, ""},
		{"a whole mark listing false, written true", false, flag, `true`, false,
			`extension "x-flag" holds none of the words the policy lists (false)` + noWalk + onMark},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			target := lowering.TargetPaginationCursor
			if tc.src.Key == link.Key {
				target = lowering.TargetPaginationNextLink
			}
			got, diags := lowering.PageStatementOf(p, target, tc.src,
				ir.Unmodeled{"openapi:" + tc.src.Key: vendorExtension(tc.value)})
			assert.True(t, got.Written, "a key the node keeps is written, whatever it holds")
			assert.Equal(t, tc.stated, got.Stated)
			if tc.report == "" {
				assert.Empty(t, diags)
				return
			}
			require.Len(t, diags, 1)
			assert.Equal(t, ir.SeverityInfo, diags[0].Severity)
			assert.Equal(t, "openapi/degraded-construct", diags[0].Code)
			assert.Equal(t, tc.report, diags[0].Message)
			want := "/components/schemas/S"
			if tc.member {
				want += "/" + tc.src.Member
			}
			assert.Equal(t, want, string(diags[0].Provenance.Pointer), "the report sits at what it is about")
		})
	}
}
