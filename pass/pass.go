package pass

import "github.com/dexpace/morphic/ir"

// Pass is one IR-to-IR step the engine schedules.
//
// Name is stable and unique among an engine's passes, since a caller disables a
// pass by it. Run never mutates doc and never returns nil: a transform returns
// a new document sharing what it left unchanged, and an analysis returns doc. A
// transform writes types through an ir.Editor, and may append to
// Document.Sources but keeps each entry at its index. Run reports only through
// its diagnostics, as the engine owns Document.Diagnostics. A Pass holds no
// mutable state, so one value serves concurrent runs.
//
// architecture.md §2.2 says why Run has no context or error.
type Pass interface {
	Name() string
	Run(doc *ir.Document) (*ir.Document, []ir.Diagnostic)
}
