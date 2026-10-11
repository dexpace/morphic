package irverify

import (
	"reflect"
	"unicode/utf8"

	"github.com/dexpace/morphic/ir"
)

// checkUTF8 asserts every string the document holds is well-formed UTF-8.
//
// A Document refuses to encode a non-UTF-8 string rather than rewrite it to
// U+FFFD (invariant #7), and the encoder's error names an output region, not
// the string, so Verify fails first and says which string (GitHub #507).
//
// A violation quotes no string, but its Path spells a map key raw, so an
// ill-formed key's bytes appear there.
//
// The walk reaches every string, so a new IR field is held at once. Byte
// sequences it skips are safe or held elsewhere: Value.Bytes encodes as base64,
// and checkRawPayloads checks Unmodeled and RawConfig payloads.
func checkUTF8(doc *ir.Document, _ declarations) ([]Violation, bool) {
	var vs []Violation
	truncated := ir.WalkValues(doc, ir.DocumentPath, func(v reflect.Value, path string) bool {
		if v.Kind() == reflect.String && !utf8.ValidString(v.String()) {
			// The message quotes nothing: the bytes would be repeated into the
			// report.
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
