package oas

import (
	"mime"
	"net/url"
	"slices"
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
	case VersionNumber:
		return validVersion(s)
	case ServerURL:
		return validServerURL(s)
	case MediaRanges:
		return validMediaRanges(s)
	case AnchorName:
		return validAnchorName(s)
	case ResourceURI:
		u := validURI(s)
		return u != nil && u.Fragment == ""
	default:
		return true
	}
}

// validVersion reports whether s is major.minor.patch, three runs of decimal
// digits joined by dots.
func validVersion(s string) bool {
	parts := strings.Split(s, ".")
	return len(parts) == 3 && !slices.ContainsFunc(parts, func(p string) bool {
		return p == "" || strings.ContainsFunc(p, func(r rune) bool { return r < '0' || r > '9' })
	})
}

// validServerURL reports whether s writes only what a URL may hold, its
// variables' braces aside, which 3.2's template rule judges: no space, control
// or other character RFC 3986 leaves out, and a percent sign only as an escape
// of two hex digits. A template is no URL until its variables are substituted,
// so nothing finer is judged.
func validServerURL(s string) bool {
	for i := range len(s) {
		switch c := s[i]; {
		case c <= ' ' || c == 0x7f || strings.IndexByte(`"<>\^`+"`"+`|`, c) >= 0:
			return false
		case c == '%' && (i+2 >= len(s) || !isHex(s[i+1]) || !isHex(s[i+2])):
			return false
		}
	}
	return true
}

// isHex reports whether c is a hexadecimal digit.
func isHex(c byte) bool {
	return '0' <= c && c <= '9' || 'a' <= c && c <= 'f' || 'A' <= c && c <= 'F'
}

// validMediaRanges reports whether s is a comma-separated list of media types
// (RFC 9110 §8.3.1) or ranges of them (§12.5.1). An empty element names
// nothing and is read past, as an HTTP list's is, but a list must name one.
func validMediaRanges(s string) bool {
	named := false
	for element := range strings.SplitSeq(s, ",") {
		element = strings.TrimSpace(element)
		if element == "" {
			continue
		}
		mediaType, _, err := mime.ParseMediaType(element)
		if err != nil || !strings.Contains(mediaType, "/") {
			return false
		}
		named = true
	}
	return named
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

// validAnchorName reports whether s is a name a $anchor may declare. The binder
// reads a $ref's plain-name fragment by the same grammar (bind.isAnchorName),
// a test of the load phase holding the two together.
func validAnchorName(s string) bool {
	for i, c := range s {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c == '_':
		case i > 0 && (c >= '0' && c <= '9' || c == '-' || c == '.'):
		default:
			return false
		}
	}
	return s != ""
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
	for i := range len(s) {
		if s[i] == '~' && (i+1 == len(s) || (s[i+1] != '0' && s[i+1] != '1')) {
			return false
		}
	}
	return true
}
