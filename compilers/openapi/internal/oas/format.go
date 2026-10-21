package oas

import (
	"net/url"
	"strings"
	"unicode"
)

// validFormat reports whether s has the syntax. The checks refuse what no
// reading of the grammar admits and nothing finer: a URI is judged by the
// standard library's parser, which takes the braces real documents write in a
// path.
func validFormat(format Format, s string) bool {
	switch format {
	case URI:
		return validURI(s) != nil
	case AbsoluteURI:
		u := validURI(s)
		return u != nil && u.Scheme != ""
	case Email:
		return validEmail(s)
	case ComponentName:
		return validComponentName(s)
	case Token:
		return validToken(s)
	case AuthScheme:
		return s == "" || validToken(s)
	case RuntimeExpression:
		return validEmbedding(s)
	default:
		return true
	}
}

// validURI parses s as a URI reference, or returns nil when it holds a space
// or a control character, or the parser refuses it.
func validURI(s string) *url.URL {
	if strings.ContainsFunc(s, func(r rune) bool { return r <= ' ' || unicode.IsControl(r) }) {
		return nil
	}
	u, err := url.Parse(s)
	if err != nil {
		return nil
	}
	return u
}

// validEmail reports whether s is a name, one @, and a domain, with no space.
func validEmail(s string) bool {
	local, domain, ok := strings.Cut(s, "@")
	if !ok || local == "" || domain == "" || strings.Contains(domain, "@") {
		return false
	}
	return !strings.ContainsFunc(s, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) })
}

// validComponentName reports whether s matches ^[a-zA-Z0-9\.\-_]+$.
func validComponentName(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.', r == '-', r == '_':
		default:
			return false
		}
	}
	return true
}

// validToken reports whether s is one or more tchar (RFC 9110 §5.6.2).
func validToken(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r < 0x80 && strings.ContainsRune("!#$%&'*+-.^_`|~", r):
		default:
			return false
		}
	}
	return true
}

// validEmbedding reports whether s is a string a callback key may be: text
// whose every braced group is a runtime expression, or, with no brace and a
// leading $, a runtime expression bare. Text with no brace and no leading $
// holds no expression and is a fixed URL.
func validEmbedding(s string) bool {
	if !strings.ContainsAny(s, "{}") {
		return !strings.HasPrefix(s, "$") || validExpression(s)
	}
	rest := s
	for {
		before, after, open := strings.Cut(rest, "{")
		if strings.Contains(before, "}") {
			return false
		}
		if !open {
			return true
		}
		group, tail, closed := strings.Cut(after, "}")
		if !closed || !validExpression(group) {
			return false
		}
		rest = tail
	}
}

// validExpression reports whether s is a runtime expression, by the ABNF each
// specification gives under "Runtime Expressions". A name is any string, so
// "$request.query." holds an empty one.
func validExpression(s string) bool {
	switch s {
	case "$url", "$method", "$statusCode":
		return true
	}
	rest, ok := strings.CutPrefix(s, "$request.")
	if !ok {
		rest, ok = strings.CutPrefix(s, "$response.")
	}
	if !ok {
		return false
	}
	if header, isHeader := strings.CutPrefix(rest, "header."); isHeader {
		return validToken(header)
	}
	if strings.HasPrefix(rest, "query.") || strings.HasPrefix(rest, "path.") {
		return true
	}
	if rest == "body" {
		return true
	}
	pointer, isBody := strings.CutPrefix(rest, "body#")
	return isBody && validPointer(pointer)
}

// validPointer reports whether s is a JSON pointer as RFC 6901 writes it: empty,
// or "/"-led tokens in which "~" is followed by 0 or 1.
func validPointer(s string) bool {
	if s == "" {
		return true
	}
	if !strings.HasPrefix(s, "/") {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] == '~' && (i+1 == len(s) || (s[i+1] != '0' && s[i+1] != '1')) {
			return false
		}
	}
	return true
}
