// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package promotion_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/promotion"
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
	expression := promotion.ExtensionSource{Key: "x-page", Member: "cursor"}
	mark := promotion.ExtensionSource{Key: "x-role", Member: "purpose", Values: []string{"cursor"}}
	p := promotion.New(mapping(promotion.TargetPaginationCursor, expression, mark))
	assert.Equal(t, []promotion.ExtensionSource{expression, mark}, p.Sources(promotion.TargetPaginationCursor))

	for _, tc := range []struct {
		name  string
		src   promotion.ExtensionSource
		value string
		want  promotion.PageStatement
	}{
		{"an expression", expression, `{"cursor":"$request.c"}`,
			promotion.PageStatement{Expression: "$request.c", Stated: true, Written: true}},
		{"an expression key written false", expression, `false`, promotion.PageStatement{Written: true}},
		{"a mark", mark, `{"purpose":"cursor"}`, promotion.PageStatement{Stated: true, Written: true}},
		{"a mark key written false", mark, `false`, promotion.PageStatement{Written: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			entries := ir.Unmodeled{"openapi:" + tc.src.Key: vendorExtension(tc.value)}
			got, diags := promotion.PageStatementOf(p, promotion.TargetPaginationCursor, tc.src, entries, nil)
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
	for _, src := range []promotion.ExtensionSource{expression, mark} {
		got, diags := promotion.PageStatementOf(p, promotion.TargetPaginationCursor, src,
			ir.Unmodeled{"openapi:x-other": vendorExtension(`{"cursor":"$request.c","purpose":"cursor"}`)}, nil)
		assert.Equal(t, promotion.PageStatement{}, got, "a node keeping another key: %s", src)
		assert.Empty(t, diags)
	}
}

// pageValuesPolicy maps the cursor and the limit each to a member of x-page
// holding an expression and to a word of x-role's purpose, the next link to
// x-link's value whole, and the cursor also to x-mark's word and the limit to
// x-flag's. x-page's description fills a deprecation's message, x-role's next
// member and x-page whole each a page value, and x-role's kind marks the page
// count, so a judgement across the sources of one key shows which it reads.
func pageValuesPolicy() promotion.ExtensionPromotions {
	return promotion.ExtensionPromotions{Targets: map[promotion.ExtensionTarget][]promotion.ExtensionSource{
		promotion.TargetPaginationCursor: {{Key: "x-page", Member: "cursor"},
			{Key: "x-role", Member: "purpose", Values: []string{"cursor"}}, {Key: "x-mark", Values: []string{"cursor"}}},
		promotion.TargetPaginationLimit: {{Key: "x-page", Member: "limit"},
			{Key: "x-role", Member: "purpose", Values: []string{"limit"}}, {Key: "x-flag", Values: []string{"false"}}},
		promotion.TargetPaginationNextLink:   {{Key: "x-link"}},
		promotion.TargetPaginationNextCursor: {{Key: "x-role", Member: "next"}},
		promotion.TargetPaginationTotalCount: {{Key: "x-page"}},
		promotion.TargetPaginationTotalPages: {{Key: "x-role", Member: "kind", Values: []string{"total"}}},
		promotion.TargetDeprecationMessage:   {{Key: "x-page", Member: "description"}},
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
		cursor = promotion.ExtensionSource{Key: "x-page", Member: "cursor"}
		role   = promotion.ExtensionSource{Key: "x-role", Member: "purpose", Values: []string{"cursor"}}
		link   = promotion.ExtensionSource{Key: "x-link"}
		mark   = promotion.ExtensionSource{Key: "x-mark", Values: []string{"cursor"}}
		flag   = promotion.ExtensionSource{Key: "x-flag", Values: []string{"false"}}
	)
	p := promotion.New(pageValuesPolicy())
	for _, tc := range []struct {
		name   string
		member bool // the report sits at the member the source reads
		src    promotion.ExtensionSource
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
			target := promotion.TargetPaginationCursor
			if tc.src.Key == link.Key {
				target = promotion.TargetPaginationNextLink
			}
			got, diags := promotion.PageStatementOf(p, target, tc.src,
				ir.Unmodeled{"openapi:" + tc.src.Key: vendorExtension(tc.value)}, nil)
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

// TestPageStatementOf_ReadsTheRootForTrue holds an operation's key written true,
// where its source reads a member, to the statement the document's root
// writes under the key: read as if the operation wrote it, and written either
// way. A root writing none, or no object, a true among them, and a path
// item's statement beside the true each state nothing and are reported, the
// root's value at the root's key; a node that is no operation, given no root,
// reads its true as before.
func TestPageStatementOf_ReadsTheRootForTrue(t *testing.T) {
	t.Parallel()
	src := promotion.ExtensionSource{Key: "x-page", Member: "cursor"}
	p := promotion.New(mapping(promotion.TargetPaginationCursor, src))
	onTrue := ir.Unmodeled{"openapi:x-page": opKey(`true`)}
	rootKey := func(value string) ir.Unmodeled {
		return ir.Unmodeled{"openapi:x-page": {Reason: ir.ReasonVendorExtension, Value: ir.RawValue(value),
			Provenance: ir.Provenance{Pointer: "/x-page"}}}
	}
	for _, tc := range []struct {
		name          string
		own, root     ir.Unmodeled
		want          promotion.PageStatement
		code, pointer string
	}{
		{"a root stating the walk", onTrue, rootKey(`{"cursor":"$request.c"}`),
			promotion.PageStatement{Expression: "$request.c", At: ir.Provenance{Pointer: "/x-page/cursor"}, Stated: true,
				Written: true}, "", ""},
		{"a root writing none", onTrue, ir.Unmodeled{},
			promotion.PageStatement{At: opAt(), Written: true}, "openapi/unpromoted-extension", "/paths/~1x/get/x-page"},
		{"a root writing true", onTrue, rootKey(`true`),
			promotion.PageStatement{At: opAt(), Written: true}, "openapi/unpromoted-extension", "/x-page"},
		{"a path item's statement beside the true, nearer than the root's", ir.Unmodeled{"openapi:x-page": opKey(`true`),
			"openapi:pathItem/x-page": opKey(`{"cursor":"$request.d"}`)}, rootKey(`{"cursor":"$request.c"}`),
			promotion.PageStatement{Expression: "$request.d", At: ir.Provenance{Pointer: "/paths/~1x/get/x-page/cursor"},
				Stated: true, Written: true}, "", ""},
		{"no root, as for a node that is no operation", onTrue, nil,
			promotion.PageStatement{At: opAt(), Written: true}, "openapi/degraded-construct", "/paths/~1x/get/x-page"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, diags := promotion.PageStatementOf(p, promotion.TargetPaginationCursor, src, tc.own, tc.root)
			assert.Equal(t, tc.want, got)
			if tc.code == "" {
				assert.Empty(t, diags)
				return
			}
			require.Len(t, diags, 1)
			assert.Equal(t, tc.code, diags[0].Code)
			assert.Equal(t, ir.SeverityInfo, diags[0].Severity)
			assert.Equal(t, tc.pointer, string(diags[0].Provenance.Pointer))
		})
	}
}

// opAt is where opKey writes its value.
func opAt() ir.Provenance { return ir.Provenance{Pointer: "/paths/~1x/get/x-page"} }

// opKey is an operation's key holding rawJSON.
func opKey(rawJSON string) ir.UnmodeledEntry {
	return ir.UnmodeledEntry{Reason: ir.ReasonVendorExtension, Value: ir.RawValue(rawJSON), Provenance: opAt()}
}
