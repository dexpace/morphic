package pass

import "github.com/dexpace/morphic/ir"

// Pass is one IR-to-IR step the engine schedules.
//
// Name is stable and unique among an engine's passes: a caller disables one by
// it. Run never mutates doc and never returns nil: a transform returns a new
// document sharing what it left unchanged, and an analysis returns doc. A
// transform writes types through an ir.Editor and may only append to
// Document.Sources. Run reports only through its diagnostics, as the engine
// owns Document.Diagnostics, and is idempotent: handed its own output, it
// returns that unchanged. One value serves concurrent runs, holding no mutable
// state.
//
// architecture.md §2.2 says why Run has no context or error.
type Pass interface {
	Name() string
	Run(doc *ir.Document) (*ir.Document, []ir.Diagnostic)
}
