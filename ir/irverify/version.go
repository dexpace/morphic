package irverify

import (
	"github.com/dexpace/morphic/ir"
)

// versionPath is where a violation about the schema stamp is reported, spelled
// as ir.WalkValues would reach the field.
const versionPath = ir.DocumentPath + ".IRVersion"

// checkVersion asserts the document declares the IR schema generation it was
// written against, and that it is one this build reads (ir-design §2.1).
//
// Every other check here holds a document against itself: keys against node IDs,
// references against registries, canonical names against the grammar that mints
// them. This one holds it against the schema it claims to conform to — the only
// claim in a document that cannot be recomputed from its contents, and so the
// only one a consumer has to be told. A document whose every internal invariant
// holds is still unreadable if the generation that wrote it spelled its keys
// differently, and nothing else in this package can see that.
//
// Two codes rather than one, because the failures name different writers.
// Absence is a producer that never stamped the document — ours, since the
// compilers in this tree are what stamp it — and it is the failure omitempty
// hides, a document without the key being byte-identical to one that never
// carried it. An incompatible stamp is another generation's document reaching a
// consumer that cannot read it, which is a fault in the pairing.
//
// It takes no declarations and runs no walk: the field is on Document itself,
// and a walk to read one field would report a truncation flag that says nothing
// about it.
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
