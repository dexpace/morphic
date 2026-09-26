package irverify

import (
	"reflect"
	"unicode/utf8"

	"github.com/dexpace/morphic/ir"
)

// checkUTF8 asserts every string the document holds is well-formed UTF-8.
//
// A Document refuses to encode a string that is not UTF-8 rather than rewrite it
// to U+FFFD (invariant #7), so one ill-formed string anywhere fails the whole
// artifact — and the encoder's error names a region of the output, not the
// string. Verify is the check a consumer runs before trusting a document, so it
// has to fail first and say which string it was (GitHub #507).
//
// Every string is reached through the walk — IDs, names, docs, values, map keys,
// diagnostics and their provenance — so a field added to the IR is held the
// moment it exists. Only byte sequences are not: the walk skips them, and the
// two kinds the IR holds are safe or held elsewhere. Value.Bytes encodes as
// base64, and an Unmodeled or RawConfig payload is held by checkRawPayloads,
// whose JSON-validity test rejects ill-formed UTF-8 inside the payload.
//
// The message quotes nothing: repeating the bytes would put them in the report
// too. The path names the string, and spells a map key as the walk does, which
// is the key's own bytes when the ill-formed string is a key.
func checkUTF8(doc *ir.Document, _ declarations) ([]Violation, bool) {
	var vs []Violation
	truncated := ir.WalkValues(doc, ir.DocumentPath, func(v reflect.Value, path string) bool {
		if v.Kind() == reflect.String && !utf8.ValidString(v.String()) {
			vs = append(vs, Violation{
				Code:    "ir/invalid-utf8",
				Message: "string is not valid UTF-8, so the document cannot be encoded",
				Path:    path,
			})
		}
		return true
	})
	return vs, truncated
}
