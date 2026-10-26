package pass

import "github.com/dexpace/morphic/ir"

// Pass is one IR-to-IR step the engine schedules.
//
// Name is stable and unique among an engine's passes: a caller disables a pass
// by it. Run never mutates doc and never returns nil: a transform returns a new
// document sharing what it left unchanged, and an analysis returns doc. A
// transform writes types through an ir.Editor, and may append to
// Document.Sources but keeps each entry at its index. Run reports only through
// its diagnostics, as the engine owns Document.Diagnostics, and is idempotent,
// returning its own output unchanged. One Pass value, holding no mutable state,
// serves concurrent runs.
//
// architecture.md §2.2 gives the reasons.
type Pass interface {
	Name() string
	Run(doc *ir.Document) (*ir.Document, []ir.Diagnostic)
}
