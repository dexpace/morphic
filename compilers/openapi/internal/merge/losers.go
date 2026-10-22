package merge

import (
	"errors"

	"github.com/dexpace/morphic/compilers/openapi/internal/annotation"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/ir"
)

// losingDeclarationKey prefixes the Unmodeled entry a redeclaration the merge could not
// fold whole is kept under. The "openapi:" namespace is what keeps two source
// formats' keys from colliding on one node (ir-design §12); the redeclaration's
// own pointer completes it, the way an allOf branch's index completes the
// composition's keys.
const losingDeclarationKey = "openapi:conflicting-redeclaration"

// errNoSource is why a declaration that must be kept verbatim cannot be: no
// Source was recorded for it.
var errNoSource = errors.New("no source was recorded for the declaration")

// keepLosingDeclaration keeps the declaration at loser verbatim under
// Unmodeled beside the merged property, so a document consumer sees what the
// merge did not carry (GitHub #424).
//
// The value is the construct source renders, not the dropped IR values (GitHub
// #445): Unmodeled holds what the document wrote (ir-design §12), and a TypeID
// in a byte slice is invisible to irverify. The key ends in loser's pointer, so
// each declaration keeps its own entry.
//
// A declaration with no pointer is skipped, since PreserveInto overwrites a
// collapsed key; no production path produces one. A source that will not
// render is reported as an error (GitHub #144).
func (g *Merger) keepLosingDeclaration(dst *ir.Property, loser ir.Provenance, source Source) {
	pointer := loser.Pointer
	if pointer == "" {
		return
	}
	key := losingDeclarationKey + string(pointer)
	raw, err := render(source)
	if err != nil {
		g.Report(ir.SeverityError, diag.UnpreservableConstruct, pointer,
			"%s could not be kept verbatim under Unmodeled and is represented in the IR "+
				"in no form at all: %s", key, err.Error())
		return
	}
	// Degraded lowering (ir-design §4.8,
	// docs/compilers/openapi.md#allof-redeclared-property): the IR has no
	// combinator for two shapes or two defaults at one position.
	annotation.PreserveInto(&dst.Unmodeled, key, raw,
		ir.ReasonDegradedLowering, loser)
}

// render calls source, or fails with errNoSource when there is none.
func render(source Source) (ir.RawValue, error) {
	if source == nil {
		return nil, errNoSource
	}
	return source()
}
