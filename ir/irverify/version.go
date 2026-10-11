package irverify

import (
	"github.com/dexpace/morphic/ir"
)

// versionPath is where a violation about the schema stamp is reported, spelled
// as ir.WalkValues would reach the field.
const versionPath = ir.DocumentPath + ".IRVersion"

// checkVersion asserts the document declares the IR schema generation it was
// written against, and that this build reads it (ir-design §2.1).
//
// Every other check holds a document against itself; this one holds it against
// the schema it claims to conform to, the one claim a consumer cannot recompute
// from the contents.
//
// Two codes, because the failures name different writers. Absence is a producer
// in this tree that never stamped the document, a failure omitempty hides. An
// incompatible stamp is another generation's document reaching a consumer that
// cannot read it.
func checkVersion(doc *ir.Document) []Violation {
	if doc.IRVersion == "" {
		return []Violation{{
			Code:    "ir/ir-version-absent",
			Message: "document declares no irVersion; this build writes and reads " + ir.IRVersion,
			Path:    versionPath,
		}}
	}
	if !ir.CompatibleVersion(doc.IRVersion) {
		return []Violation{{
			Code:    "ir/ir-version-incompatible",
			Message: "document declares irVersion " + doc.IRVersion + "; this build reads only " + ir.IRVersion,
			Path:    versionPath,
		}}
	}
	return nil
}
