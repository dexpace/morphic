package engine

import (
	"slices"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/pass"
)

// ErrNoDocument exposes the error a pass returning no document fails a run with.
var ErrNoDocument = errNoDocument

// ErrSourcesMoved exposes the error a pass that does not keep each source at its
// index fails a run with.
var ErrSourcesMoved = errSourcesMoved

// ErrUnknownSeverity exposes the error a stage reporting a diagnostic of a
// severity the IR does not define fails a run with.
var ErrUnknownSeverity = errUnknownSeverity

// DefaultPasses exposes the default pass list to this package's external tests,
// which pin its order without a production caller needing to read it.
func DefaultPasses() []pass.Pass { return defaultPasses() }

// NewWithPasses builds an engine over the compilers cs that runs passes instead of the
// default list. The external tests compose passes the default list does not
// hold, beside the stub compilers declared there, to show threading, ordering
// and disabling. Exporting it from a _test.go file keeps it out of the
// production API.
func NewWithPasses(passes []pass.Pass, cs ...compilers.Compiler) (*Engine, error) {
	eng, err := NewWith(cs...)
	if err != nil {
		return nil, err
	}
	eng.passes = slices.Clone(passes)
	return eng, nil
}
