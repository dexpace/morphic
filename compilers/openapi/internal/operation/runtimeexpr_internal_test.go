// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package operation

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestParseExpression_ReadsTheGrammar holds parseExpression to the Runtime
// Expressions ABNF of the OpenAPI Specification: what it reads as an
// expression, what it leaves a constant, and what it refuses as malformed.
func TestParseExpression_ReadsTheGrammar(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		in        string
		want      expression
		isExpr    bool
		malformed bool
	}{
		{in: "$url", want: expression{source: exprURL}, isExpr: true},
		{in: "$method", want: expression{source: exprMethod}, isExpr: true},
		{in: "$statusCode", want: expression{source: exprStatusCode}, isExpr: true},
		{in: "$request.path.id", want: expression{source: exprRequest, part: exprPath, name: "id"}, isExpr: true},
		{in: "$request.query.page.size", want: expression{source: exprRequest, part: exprQuery, name: "page.size"}, isExpr: true},
		{in: "$response.header.X-Rate_1", want: expression{source: exprResponse, part: exprHeader, name: "X-Rate_1"}, isExpr: true},
		{in: "$response.body", want: expression{source: exprResponse, part: exprBody}, isExpr: true},
		{in: "$response.body#", want: expression{source: exprResponse, part: exprBody}, isExpr: true},
		{in: "$request.body#/a~1b/c~0d/", want: expression{
			source: exprRequest, part: exprBody, tokens: []string{"a/b", "c~d", ""},
		}, isExpr: true},
		{in: "$5.00"},
		{in: "$urls"},
		{in: "plain"},
		{in: "$request.cookie.c", isExpr: true, malformed: true},
		{in: "$request.query", isExpr: true, malformed: true},
		{in: "$request.query.", isExpr: true, malformed: true},
		{in: "$response.header.bad name", isExpr: true, malformed: true},
		{in: "$response.bodies", isExpr: true, malformed: true},
		{in: "$response.body/id", isExpr: true, malformed: true},
		{in: "$response.body#a", isExpr: true, malformed: true},
		{in: "$response.body#/a~2", isExpr: true, malformed: true},
		{in: "$response.body#/a~", isExpr: true, malformed: true},
	} {
		t.Run(tc.in, func(t *testing.T) {
			t.Parallel()
			got, isExpr, err := parseExpression(tc.in)
			assert.Equal(t, tc.isExpr, isExpr)
			assert.Equal(t, tc.isExpr, startsExpression(tc.in), "startsExpression agrees")
			if tc.malformed {
				assert.ErrorIs(t, err, errMalformedExpression)
				return
			}
			assert.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

// TestEmbedsExpression_FindsAnExpressionInBraces pins what counts as a string
// an expression is substituted into: an expression in braces, wherever it sits,
// whether or not the braces hold the whole string and whether or not the rest
// of it keeps the grammar, but not braces holding what starts as none.
func TestEmbedsExpression_FindsAnExpressionInBraces(t *testing.T) {
	t.Parallel()
	for in, want := range map[string]bool{
		"item-{$response.body#/id}":   true,
		"{$url}":                      true,
		"{$nope} then {$method}":      true,
		"{$request.cookie.c}":         true,
		"{$response.body#/a~2}":       true,
		"{$requestx}":                 false,
		"{$response.body#/id":         false,
		"no braces at all":            false,
		"{not an expression}":         false,
		"$response.body#/id":          false,
		"{${$statusCode}":             true,
		"{${$statusCode":              false,
		"prefix {$request.query.q}x}": true,
	} {
		assert.Equal(t, want, embedsExpression(in), "%q", in)
	}
}

// TestEmbedsExpression_AllocatesNothing pins that a string opening braces many
// times before one close is read without parsing what each opening holds: a
// parse, which splits a body pointer into tokens, allocates for each one. It
// takes the fewest of single runs, and does not run in parallel, since the
// count is process-wide.
func TestEmbedsExpression_AllocatesNothing(t *testing.T) {
	s := strings.Repeat("{$request.body#/a/b/c", 200) + "~2}"
	require.True(t, embedsExpression(s))
	least := testing.AllocsPerRun(1, func() { embedsExpression(s) })
	for range 4 {
		least = min(least, testing.AllocsPerRun(1, func() { embedsExpression(s) }))
	}
	assert.Zero(t, least)
}
