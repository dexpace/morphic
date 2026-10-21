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
