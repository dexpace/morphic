package operation

import (
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/ir"
)

// pathLiteralBytes is every byte RFC 3986 §3.3's pchar admits as written:
// unreserved, sub-delims, ":" and "@". Its pct-encoded form is read by percent,
// and the "/" between segments by read.
const pathLiteralBytes = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789" +
	"-._~!$&'()*+,;=:@"

// pathKeyDiags reports a Paths Object key that is not a path template in one
// warning naming every rule it breaks, or nothing when it is one. The key is
// used as written either way: uriTemplate takes it verbatim, and the name hint
// of an operation with no operationId and the path-prefix group derive from it
// unrepaired.
func pathKeyDiags(c lowering.Ctx, key string) []ir.Diagnostic {
	defects := pathKeyDefects(key)
	if len(defects) == 0 {
		return nil
	}
	return []ir.Diagnostic{c.DiagAt(ir.SeverityWarning, diag.InvalidPathKey, ids.Ptr("paths", key),
		"path key %q is not a valid path template: %s; the key is used as written",
		key, strings.Join(defects, "; "))}
}

// pathKeyDefects returns a phrase for each path templating rule key breaks, in
// a fixed order, or nil when it breaks none. The rules are those of 3.2.0
// §4.8.2, its ABNF and its ban on using an expression twice, read at every
// version: 3.0 and 3.1 describe path templating only in prose (§3.2), and a
// maintainer of the specification states that a 3.x minor cannot restrict it
// further (OAI/OpenAPI-Specification#5062).
//
// Only the path is read. A "?" ends it in a query, and a "#" in a fragment,
// which GitHub #602 owns, so nothing after a "#" is judged.
func pathKeyDefects(key string) []string {
	s := pathKeyScan{key: key}
	s.read()
	return s.defects()
}

// pathKeyScan is one left-to-right reading of a Paths key, recording the path
// templating rules it breaks.
type pathKeyScan struct {
	key string
	// query is the query the key carries, from its "?" to any "#", or "".
	query string
	// unencoded lists each character the path admits only percent-encoded,
	// once, in the order first met; seen is the set behind it.
	unencoded []string
	seen      map[string]bool
	// uses counts each template expression's name, and repeated lists each
	// name used twice, once, in the order its second use is met.
	uses     map[string]int
	repeated []string
	// strayBrace, badPercent and emptySegment record a brace outside a
	// template expression, a "%" beginning no pct-encoded octet, and "//".
	strayBrace, badPercent, emptySegment bool
}

// read walks the key's path, stopping at the "?" or "#" that ends it. Each
// step advances by at least one byte, so it ends within len(key) steps.
func (s *pathKeyScan) read() {
	key := s.key
	for i := 0; i < len(key); {
		switch key[i] {
		case '?':
			s.query, _, _ = strings.Cut(key[i:], "#")
			return
		case '#':
			return
		case '{':
			i = s.expression(i)
		case '}':
			s.strayBrace = true
			i++
		case '%':
			i = s.percent(i)
		case '/':
			if i > 0 && key[i-1] == '/' {
				s.emptySegment = true
			}
			i++
		default:
			i = s.literal(i)
		}
	}
}

// expression reads the template expression opening at key[i] and returns where
// reading resumes. An expression is "{", a name of one or more characters other
// than a brace, then "}". A "{" that opens none is stray, and reading resumes
// just past it, so what follows is read as path.
func (s *pathKeyScan) expression(i int) int {
	n := strings.IndexAny(s.key[i+1:], "{}")
	if n <= 0 || s.key[i+1+n] == '{' {
		s.strayBrace = true
		return i + 1
	}
	name := s.key[i+1 : i+1+n]
	if s.uses == nil {
		s.uses = make(map[string]int)
	}
	s.uses[name]++
	if s.uses[name] == 2 {
		s.repeated = append(s.repeated, name)
	}
	return i + n + 2
}

// percent reads the "%" at key[i] and returns where reading resumes. It must
// begin a pct-encoded octet, two hexadecimal digits following it (RFC 3986
// §2.1); one that does not is read alone.
func (s *pathKeyScan) percent(i int) int {
	if i+2 < len(s.key) && isHexDigit(s.key[i+1]) && isHexDigit(s.key[i+2]) {
		return i + 3
	}
	s.badPercent = true
	return i + 1
}

// literal reads the character at key[i] and returns where reading resumes. A
// byte in pathLiteralBytes stands as written; any other character, non-ASCII
// included, a path admits only percent-encoded. A byte beginning no UTF-8
// encoding is read alone.
func (s *pathKeyScan) literal(i int) int {
	if strings.IndexByte(pathLiteralBytes, s.key[i]) >= 0 {
		return i + 1
	}
	_, size := utf8.DecodeRuneInString(s.key[i:])
	char := s.key[i : i+size]
	if !s.seen[char] {
		if s.seen == nil {
			s.seen = make(map[string]bool)
		}
		s.seen[char] = true
		s.unencoded = append(s.unencoded, char)
	}
	return i + size
}

// defects returns a phrase for each rule the read key broke, in a fixed order.
func (s *pathKeyScan) defects() []string {
	var out []string
	if !strings.HasPrefix(s.key, "/") {
		out = append(out, `does not begin with "/"`)
	}
	if len(s.unencoded) > 0 {
		out = append(out, "contains "+quotedList(s.unencoded, "", "")+
			", which a path admits only percent-encoded")
	}
	if s.badPercent {
		out = append(out, `has a "%" that begins no percent-encoded octet`)
	}
	if s.strayBrace {
		out = append(out, `has a brace that is not part of a "{name}" template expression`)
	}
	if len(s.repeated) > 0 {
		out = append(out, "repeats "+quotedList(s.repeated, "{", "}"))
	}
	if s.emptySegment {
		out = append(out, `has an empty segment ("//")`)
	}
	if s.query != "" {
		out = append(out, "carries the query "+strconv.Quote(s.query))
	}
	return out
}

// quotedList Go-quotes each of items between prefix and suffix, so a control
// character or invalid byte is escaped rather than written raw, and joins them
// as an English list: "a", "a" and "b", or "a", "b" and "c".
func quotedList(items []string, prefix, suffix string) string {
	quoted := make([]string, len(items))
	for i, item := range items {
		quoted[i] = strconv.Quote(prefix + item + suffix)
	}
	if len(quoted) < 2 {
		return strings.Join(quoted, "")
	}
	last := len(quoted) - 1
	return strings.Join(quoted[:last], ", ") + " and " + quoted[last]
}

// isHexDigit reports whether b is one of the sixteen hexadecimal digits, in
// either case.
func isHexDigit(b byte) bool {
	return b >= '0' && b <= '9' || b >= 'a' && b <= 'f' || b >= 'A' && b <= 'F'
}
