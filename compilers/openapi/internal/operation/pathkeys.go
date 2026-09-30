package operation

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/ir"
)

// The defect clauses one warning's message joins. They are constants rather than
// inline strings so the classifier's table can name the same text the message
// carries.
const (
	// pathKeyNoSlashDefect is OpenAPI's Patterned Fields rule for the Paths
	// Object, stated identically at every version (3.0.3 §4.7.8.1, 3.1.0
	// §4.8.8.1, 3.2.0 §4.8.1): "The field name MUST begin with a forward slash
	// (`/`)". The empty key omits the slash too.
	pathKeyNoSlashDefect = `missing leading "/"`
	// pathKeyQueryDefect is RFC 3986 §3.4's: the query is not part of the path,
	// and OpenAPI states query data as Parameter Objects with in: query.
	pathKeyQueryDefect = `carries a query ("?")`
	// pathKeyCharDefectFormat is RFC 3986 §3.3's pchar set (unreserved,
	// pct-encoded, sub-delims, ":" and "@") plus "/" and OpenAPI's "{}"
	// templating braces.
	pathKeyCharDefectFormat = "contains %q, which is not allowed in a URI path"
	// pathKeyPercentDefect is the pct-encoded half of the same rule: "%" is
	// admitted only as the start of a triplet (RFC 3986 §2.1).
	pathKeyPercentDefect = `contains "%" that begins no percent-encoded triplet`
)

// pathKeyAllowed is every byte a URI path admits here: RFC 3986 §3.3's pchar —
// unreserved, sub-delims, ":" and "@" — plus "/" for the separators and "{}" for
// OpenAPI path templating. "%" is handled separately, since it is admitted only
// as the start of a pct-encoded triplet, and "#" is left out entirely because
// the fragment is GitHub #602's to strip and report.
const pathKeyAllowed = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz" +
	"0123456789-._~!$&'()*+,;=:@/{}"

// pctTripletLen is the width of a pct-encoded octet: the "%" and its two
// hexadecimal digits.
const pctTripletLen = 3

// pathKeyDiags reports a Paths Object key that is not a path. It returns at most
// one diagnostic, however many defects the key carries: the key is one position,
// and one report naming all its defects is what a reader fixes (GitHub #641).
//
// The key is not rewritten — it reaches uriTemplate, the no-operationId name
// hint and the path-prefix group exactly as written — so this says only that the
// spelling is not a path; see diag.InvalidPathKey for why that is a warning, and
// why "#" is #602's.
func pathKeyDiags(c lowering.Ctx, key string) []ir.Diagnostic {
	defects := pathKeyDefects(key)
	if len(defects) == 0 {
		return nil
	}
	return []ir.Diagnostic{c.DiagAt(ir.SeverityWarning, diag.InvalidPathKey, ids.Ptr("paths", key),
		"path key %q is not a valid path: %s; the key is lowered as written",
		key, strings.Join(defects, "; "))}
}

// pathKeyDefects returns why key is not a valid Paths Object key, in a fixed
// order, or nil when it is one — or when the only thing wrong with it is a "#"
// fragment, which #602 owns.
func pathKeyDefects(key string) []string {
	var defects []string
	if !strings.HasPrefix(key, "/") {
		defects = append(defects, pathKeyNoSlashDefect)
	}
	if strings.ContainsRune(key, '?') {
		defects = append(defects, pathKeyQueryDefect)
	}
	if defect, bad := pathKeyCharacterDefect(key); bad {
		defects = append(defects, defect)
	}
	return defects
}

// pathKeyCharacterDefect returns the defect for the first byte of key that a URI
// path does not admit, and whether it found one. A non-ASCII byte is admitted:
// the IR field is an RFC 6570 template (ir/bindings.go), whose §2.1 literals
// admit ucschar and iprivate and pct-encode them on expansion.
func pathKeyCharacterDefect(key string) (string, bool) {
	for i := 0; i < len(key); {
		b := key[i]
		switch {
		case b >= utf8.RuneSelf:
			_, size := utf8.DecodeRuneInString(key[i:])
			i += size
		case b == '#':
			// A fragment is #602's to strip and report; see diag.InvalidPathKey.
			i++
		case b == '?':
			// Named by pathKeyQueryDefect instead, which says what a query is.
			i++
		case b == '%':
			if !pctTriplet(key, i) {
				return pathKeyPercentDefect, true
			}
			i += pctTripletLen
		case strings.IndexByte(pathKeyAllowed, b) >= 0:
			i++
		default:
			return fmt.Sprintf(pathKeyCharDefectFormat, rune(b)), true
		}
	}
	return "", false
}

// pctTriplet reports whether key[i:] begins a pct-encoded octet: the "%" at i
// followed by two hexadecimal digits (RFC 3986 §2.1).
func pctTriplet(key string, i int) bool {
	return i+pctTripletLen <= len(key) && isHexDigit(key[i+1]) && isHexDigit(key[i+2])
}

// isHexDigit reports whether b is one of the sixteen hexadecimal digits, in
// either case.
func isHexDigit(b byte) bool {
	return b >= '0' && b <= '9' || b >= 'a' && b <= 'f' || b >= 'A' && b <= 'F'
}
