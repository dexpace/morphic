// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package operation

import (
	"errors"
	"strings"
)

// The sources a runtime expression reads, as OpenAPI's grammar spells them.
const (
	exprURL        = "url"
	exprMethod     = "method"
	exprStatusCode = "statusCode"
	exprRequest    = "request"
	exprResponse   = "response"
)

// The parts of a request or a response an expression reads.
const (
	exprHeader = "header"
	exprQuery  = "query"
	exprPath   = "path"
	exprBody   = "body"
)

// errMalformedExpression reports a string that starts as a runtime expression
// and breaks its grammar.
var errMalformedExpression = errors.New("malformed runtime expression")

// expression is one runtime expression, as the Runtime Expressions section of
// the OpenAPI Specification defines it: a value of the exchange a response
// ends.
type expression struct {
	// source is exprURL, exprMethod, exprStatusCode, exprRequest or exprResponse.
	source string
	// part is what of the request or response it reads, "" for the other
	// sources.
	part string
	// name is the header, query or path parameter it reads.
	name string
	// tokens are the decoded tokens of the pointer into the body, none for the
	// body whole.
	tokens []string
}

// parseExpression reads s as a runtime expression. It returns false for a
// string that is not one, which a link passes as a constant, and
// errMalformedExpression for one that starts as an expression and breaks the
// grammar: `$request.` or `$response.` followed by no part it names.
func parseExpression(s string) (expression, bool, error) {
	switch s {
	case "$" + exprURL, "$" + exprMethod, "$" + exprStatusCode:
		return expression{source: s[1:]}, true, nil
	}
	for _, source := range []string{exprRequest, exprResponse} {
		if rest, ok := strings.CutPrefix(s, "$"+source+"."); ok {
			e, err := parseSource(source, rest)
			return e, true, err
		}
	}
	return expression{}, false, nil
}

// parseSource reads rest, what follows `$request.` or `$response.`.
func parseSource(source, rest string) (expression, error) {
	e := expression{source: source}
	if pointer, ok := strings.CutPrefix(rest, exprBody); ok {
		e.part = exprBody
		return e.withPointer(pointer)
	}
	part, name, ok := strings.Cut(rest, ".")
	if !ok || name == "" {
		return e, errMalformedExpression
	}
	switch part {
	case exprHeader:
		if !isToken(name) {
			return e, errMalformedExpression
		}
	case exprQuery, exprPath:
	default:
		return e, errMalformedExpression
	}
	e.part, e.name = part, name
	return e, nil
}

// withPointer reads what follows `body`: nothing, or `#` and an RFC 6901
// pointer, whose tokens it decodes.
func (e expression) withPointer(rest string) (expression, error) {
	if rest == "" {
		return e, nil
	}
	pointer, ok := strings.CutPrefix(rest, "#")
	if !ok {
		return e, errMalformedExpression
	}
	if pointer == "" {
		return e, nil
	}
	raw, ok := strings.CutPrefix(pointer, "/")
	if !ok {
		return e, errMalformedExpression
	}
	for token := range strings.SplitSeq(raw, "/") {
		decoded, ok := decodeToken(token)
		if !ok {
			return e, errMalformedExpression
		}
		e.tokens = append(e.tokens, decoded)
	}
	return e, nil
}

// decodeToken decodes one RFC 6901 reference token, false where a `~` is not
// `~0` or `~1`.
func decodeToken(token string) (string, bool) {
	var b strings.Builder
	for i := 0; i < len(token); i++ {
		if token[i] != '~' {
			b.WriteByte(token[i])
			continue
		}
		if i+1 == len(token) || (token[i+1] != '0' && token[i+1] != '1') {
			return "", false
		}
		if token[i+1] == '0' {
			b.WriteByte('~')
		} else {
			b.WriteByte('/')
		}
		i++
	}
	return b.String(), true
}

// isToken reports whether s is an RFC 9110 token, the grammar a header name
// takes.
func isToken(s string) bool {
	for i := range len(s) {
		c := s[i]
		alnum := c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
		if !alnum && !strings.ContainsRune("!#$%&'*+-.^_`|~", rune(c)) {
			return false
		}
	}
	return s != ""
}

// embedsExpression reports whether s holds a runtime expression in braces, as
// a template such as `item-{$response.body#/id}` does: a string an expression
// is substituted into, which no link value spells, even when the braces hold
// the whole string.
func embedsExpression(s string) bool {
	for i := range len(s) {
		if !strings.HasPrefix(s[i:], "{$") {
			continue
		}
		end := strings.IndexByte(s[i:], '}')
		if end < 0 {
			return false
		}
		if _, ok, err := parseExpression(s[i+1 : i+end]); ok && err == nil {
			return true
		}
	}
	return false
}
