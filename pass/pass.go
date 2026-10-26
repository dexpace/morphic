package pass

import (
	"errors"

	"github.com/dexpace/morphic/ir"
)

// ErrContract reports a pass that broke its contract: a duty Pass's doc
// comment states, which the engine checked and found unmet. It names a bug in
// the pass, never in the spec, so it is a Go error rather than a diagnostic.
var ErrContract = errors.New("pass broke its contract")

// Pass is one IR-to-IR step the engine schedules.
//
// Name is stable and unique among an engine's passes: a caller disables one by
// it. Run never panics, mutates doc or returns nil: a transform returns a new
// document sharing what it left unchanged, and an analysis returns doc. Either
// keeps the IR version and each Sources entry at its index, though it may
// append, reports through diagnostics alone, as the engine owns
// Document.Diagnostics, and is idempotent over a run's result, Diagnostics
// aside. A Pass holds no mutable state.
//
// A pass breaking one fails the run, wrapping ErrContract. architecture.md §2.2
// explains Run's signature.
type Pass interface {
	Name() string
	Run(doc *ir.Document) (*ir.Document, []ir.Diagnostic)
}
