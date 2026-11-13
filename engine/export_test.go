// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package engine

import (
	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/engine/cache"
	"github.com/dexpace/morphic/engine/triage"
	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/ir/irverify"
)

// ErrNoDocument exposes the error a pass returning no document fails a run with.
var ErrNoDocument = errNoDocument

// ErrSourcesMoved exposes the error a pass that does not keep each source at its
// index fails a run with.
var ErrSourcesMoved = errSourcesMoved

// ErrSourceOutOfRange exposes the error a pass reporting a diagnostic that
// indexes no source fails a run with.
var ErrSourceOutOfRange = errSourceOutOfRange

// ErrUnknownSeverity exposes the error a stage reporting a diagnostic of a
// severity the IR does not define fails a run with.
var ErrUnknownSeverity = errUnknownSeverity

// MaxTriageBytes exposes the bound on a triage file.
const MaxTriageBytes = maxTriageBytes

// ErrVersionChanged exposes the error a pass returning a document stamped with
// another IR version fails a run with.
var ErrVersionChanged = errVersionChanged

// ErrDiagnosticsChanged exposes the error a pass that puts findings on
// Document.Diagnostics fails a run with.
var ErrDiagnosticsChanged = errDiagnosticsChanged

// ErrInputChanged exposes the error a pass that changes the document it was
// handed fails a verified run with.
var ErrInputChanged = errInputChanged

// OnVerify makes e call f each time a verified run is about to hold a document
// to the IR's invariants, whatever irverify then says of it.
func OnVerify(e *Engine, f func()) {
	inner := e.verifier
	e.verifier = func(doc *ir.Document) []irverify.Violation {
		f()
		return inner(doc)
	}
}

// KeyInputs is what a run's key covers, for a test that changes one input a run
// cannot, such as the IR version.
type KeyInputs = keyInputs

// DeriveKey exposes deriveKey.
func DeriveKey(in KeyInputs) cache.Key { return deriveKey(in) }

// FailLocate makes e unable to place a spec's Path, as where the working
// directory cannot be named, which a test cannot rely on a system to refuse:
// removing it leaves some still naming it.
func FailLocate(e *Engine, err error) {
	e.locate = func(string) (string, error) { return "", err }
}

// Recorder exposes the reader a cached compile reads through.
type Recorder = recorder

// NewRecorder returns a Recorder reading through docs.
func NewRecorder(docs compilers.DocumentReader) *Recorder {
	return newRecorder(docs)
}

// SettledLocations returns where r read, in the order an entry lists them, and
// whether r could vouch for them.
func SettledLocations(r *Recorder) ([]string, bool) {
	reads, sound := r.settled()
	locations := make([]string, 0, len(reads))
	for _, read := range reads {
		locations = append(locations, read.Request.Location)
	}
	return locations, sound
}

// SettledDirectories returns each directory r placed and where, in the order
// an entry lists them.
func SettledDirectories(r *Recorder) [][2]string {
	dirs := r.directories()
	out := make([][2]string, 0, len(dirs))
	for _, d := range dirs {
		out = append(out, [2]string{d.Directory, d.Located})
	}
	return out
}

// SealedEntry returns an entry for key holding head and body, sealed as a store
// would hand it back intact: what a test writes to reach what follows the seal.
func SealedEntry(key cache.Key, head, body string) []byte {
	return sealed(key, []byte(head), []byte(body))
}

// Verdict exposes the verdict at failOn over findings rated by hand, for a test
// that rates one at a severity no rule could set.
func Verdict(failOn FailOn, doc *ir.Document, findings []triage.Finding) (bool, error) {
	t, err := failOn.resolve()
	if err != nil {
		return false, err
	}
	return t.verdict(doc, findings)
}
