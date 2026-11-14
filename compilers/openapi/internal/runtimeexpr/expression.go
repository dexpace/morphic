// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package runtimeexpr

import (
	"errors"
	"strings"
)

// The sources a runtime expression reads, as OpenAPI's grammar spells them.
const (
	SourceURL        = "url"
	SourceMethod     = "method"
	SourceStatusCode = "statusCode"
	SourceRequest    = "request"
	SourceResponse   = "response"
)

// The parts of a request or a response an expression reads.
const (
	PartHeader = "header"
	PartQuery  = "query"
	PartPath   = "path"
	PartBody   = "body"
)

// errMalformed reports a string that starts as a runtime expression and breaks
// its grammar.
var errMalformed = errors.New("malformed runtime expression")

// Expression is one runtime expression, as the Runtime Expressions section of
// the OpenAPI Specification defines it: a value of the exchange a response
// ends.
type Expression struct {
	// Source is SourceURL, SourceMethod, SourceStatusCode, SourceRequest or
	// SourceResponse.
	Source string
	// Part is what of the request or response it reads, "" for the other
	// sources.
	Part string
	// Name is the header, query or path parameter it reads.
	Name string
	// Tokens are the decoded tokens of the pointer into the body, none for the
	// body whole.
	Tokens []string
}

// Parse reads s as a runtime expression. It returns false for a string that is
// not one, which a link passes as a constant, and an error for one that starts
// as an expression and breaks the grammar: `$request.` or `$response.`
// followed by no part it names.
func Parse(s string) (Expression, bool, error) {
	if !startsExpression(s) {
		return Expression{}, false, nil
	}
	switch s {
	case "$" + SourceURL, "$" + SourceMethod, "$" + SourceStatusCode:
		return Expression{Source: s[1:]}, true, nil
	}
	source, rest, _ := strings.Cut(s[1:], ".")
	e, err := parseSource(source, rest)
	return e, true, err
}

// startsExpression reports whether s reads as a runtime expression, well
// formed or not: one of the three sources a name alone spells, or `$request.`
// or `$response.` and whatever follows.
func startsExpression(s string) bool {
	switch s {
	case "$" + SourceURL, "$" + SourceMethod, "$" + SourceStatusCode:
		return true
	}
	return strings.HasPrefix(s, "$"+SourceRequest+".") || strings.HasPrefix(s, "$"+SourceResponse+".")
}

// parseSource reads rest, what follows `$request.` or `$response.`.
func parseSource(source, rest string) (Expression, error) {
	e := Expression{Source: source}
	if pointer, ok := strings.CutPrefix(rest, PartBody); ok {
		e.Part = PartBody
		return e.withPointer(pointer)
	}
	part, name, ok := strings.Cut(rest, ".")
	if !ok || name == "" {
		return e, errMalformed
	}
	switch part {
	case PartHeader:
		if !isToken(name) {
			return e, errMalformed
		}
	case PartQuery, PartPath:
	default:
		return e, errMalformed
	}
	e.Part, e.Name = part, name
	return e, nil
}

// withPointer reads what follows `body`: nothing, or `#` and an RFC 6901
// pointer, whose tokens it decodes.
func (e Expression) withPointer(rest string) (Expression, error) {
	if rest == "" {
		return e, nil
	}
	pointer, ok := strings.CutPrefix(rest, "#")
	if !ok {
		return e, errMalformed
	}
	if pointer == "" {
		return e, nil
	}
	raw, ok := strings.CutPrefix(pointer, "/")
	if !ok {
		return e, errMalformed
	}
	for token := range strings.SplitSeq(raw, "/") {
		decoded, ok := decodeToken(token)
		if !ok {
			return e, errMalformed
		}
		e.Tokens = append(e.Tokens, decoded)
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

// Embeds reports whether s holds a runtime expression in braces, as
// a template such as `item-{$response.body#/id}` does: a string an expression
// is substituted into, which no link value spells, even when the braces hold
// the whole string. Braces count when what they hold starts as an expression,
// whether or not the rest keeps the grammar, as Parse tells an
// expression from a constant by its start. So no candidate is parsed, and
// braces opened many times before one close cost a look at each opening.
func Embeds(s string) bool {
	closing := -1
	for i := 0; i+1 < len(s); i++ {
		if s[i] != '{' || s[i+1] != '$' {
			continue
		}
		if closing < i {
			end := strings.IndexByte(s[i:], '}')
			if end < 0 {
				return false
			}
			closing = i + end
		}
		if startsExpression(s[i+1 : closing]) {
			return true
		}
	}
	return false
}
