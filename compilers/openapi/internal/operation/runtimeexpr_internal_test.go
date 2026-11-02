// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package operation

import (
	"testing"

	"github.com/stretchr/testify/assert"
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
// an expression is substituted into: an expression in braces, wherever it sits
// and whether or not the braces hold the whole string, but not braces holding
// no expression.
func TestEmbedsExpression_FindsAnExpressionInBraces(t *testing.T) {
	t.Parallel()
	for in, want := range map[string]bool{
		"item-{$response.body#/id}":   true,
		"{$url}":                      true,
		"{$nope} then {$method}":      true,
		"{$request.cookie.c}":         false,
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
