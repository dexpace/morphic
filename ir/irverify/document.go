package irverify

import (
	"strconv"
	"strings"

	"github.com/dexpace/morphic/ir"
)

// sourcesPath and servicesPath are where a violation about the two document
// tables is reported, spelled as ir.WalkValues would reach each field.
const (
	sourcesPath  = ir.DocumentPath + ".Sources"
	servicesPath = ir.DocumentPath + ".Services"
)

// sha256HexLen is the length of a SHA-256 digest spelled in hex.
const sha256HexLen = 64

// checkDocument asserts the two tables every compiled document carries
// (ir-design §2): at least one service, and a source table naming each input
// with the format it was read as and the digest of its bytes.
//
// A compile reads at least one file and lowers it to at least one service, so
// an empty table names a producer that assembled a document from nothing. The
// format and hash say which bytes were read as what; a refused compile's table,
// which sets Path alone, belongs to no Document.
func checkDocument(doc *ir.Document) []Violation {
	var vs []Violation
	if len(doc.Services) == 0 {
		vs = append(vs, Violation{
			Code:    "ir/services-absent",
			Message: "document declares no service; a compiled document holds at least one",
			Path:    servicesPath,
		})
	}
	if len(doc.Sources) == 0 {
		vs = append(vs, Violation{
			Code:    "ir/sources-absent",
			Message: "document names no source; a compiled document was read from at least one",
			Path:    sourcesPath,
		})
	}
	for i, src := range doc.Sources {
		if !isFormat(src.Format) {
			vs = append(vs, Violation{
				Code:    "ir/source-format-malformed",
				Message: "source format " + strconv.Quote(src.Format) + " is not a name@version pair",
				Path:    sourcePath(i, ".Format"),
			})
		}
		if !isSHA256Hex(src.Hash) {
			vs = append(vs, Violation{
				Code:    "ir/source-hash-malformed",
				Message: "source hash " + strconv.Quote(src.Hash) + " is not a lowercase hex SHA-256 digest",
				Path:    sourcePath(i, ".Hash"),
			})
		}
	}
	return vs
}

// sourcePath spells where field of source i sits, for a violation to be placed
// there. It is spelled only then, since strconv.Itoa builds the string for an
// index of 100 or more: spelled for every source, it would cost a large source
// table an allocation per source with nothing wrong.
func sourcePath(i int, field string) string {
	return sourcesPath + "[" + strconv.Itoa(i) + "]" + field
}

// isFormat reports whether format reads as name@version, each part non-empty
// and the name free of '@', as compilers.SourceFormat.String spells one.
func isFormat(format string) bool {
	name, version, _ := strings.Cut(format, "@")
	return name != "" && version != ""
}

// isSHA256Hex reports whether hash is a SHA-256 digest in lowercase hex, the
// spelling encoding/hex writes.
func isSHA256Hex(hash string) bool {
	if len(hash) != sha256HexLen {
		return false
	}
	for i := range len(hash) {
		c := hash[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}
