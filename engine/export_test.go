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

// OnVerify makes compileEngine call f each time a verified run is about to hold
// a document to the IR's invariants, whatever irverify then says of it.
func OnVerify(compileEngine *Engine, f func()) {
	inner := compileEngine.verifier
	compileEngine.verifier = func(doc *ir.Document) []irverify.Violation {
		f()
		return inner(doc)
	}
}

// KeyInputs is what a run's key covers, for a test that changes one input a run
// cannot, such as the IR version.
type KeyInputs = keyInputs

// DeriveKey exposes deriveKey.
func DeriveKey(in KeyInputs) cache.Key { return deriveKey(in) }

// FailLocate makes compileEngine unable to place a spec's Path, as where the
// working directory cannot be named, which a test cannot rely on a system to
// refuse: removing it leaves some still naming it.
func FailLocate(compileEngine *Engine, err error) {
	compileEngine.locate = func(string) (string, error) { return "", err }
}

// Recorder exposes the reader a cached compile reads through.
type Recorder = recorder

// NewRecorder returns a Recorder reading through docs.
func NewRecorder(docs compilers.DocumentReader) *Recorder {
	return newRecorder(docs)
}

// SettledLocations returns where recorder read, in the order an entry lists
// them, and whether recorder could vouch for them.
func SettledLocations(recorder *Recorder) ([]string, bool) {
	reads, sound := recorder.settled()
	locations := make([]string, 0, len(reads))
	for _, read := range reads {
		locations = append(locations, read.Request.Location)
	}
	return locations, sound
}

// SettledRequests returns what recorder was asked, in the order an entry lists
// it.
func SettledRequests(recorder *Recorder) []compilers.DocumentRequest {
	reads, _ := recorder.settled()
	requests := make([]compilers.DocumentRequest, 0, len(reads))
	for _, read := range reads {
		requests = append(requests, read.Request)
	}
	return requests
}

// SettledDirectories returns each directory recorder placed and where, in the
// order an entry lists them.
func SettledDirectories(recorder *Recorder) [][2]string {
	dirs := recorder.directories()
	out := make([][2]string, 0, len(dirs))
	for _, locatedDirectory := range dirs {
		out = append(out, [2]string{locatedDirectory.Directory, locatedDirectory.Located})
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
	threshold, err := failOn.resolve()
	if err != nil {
		return false, err
	}
	return threshold.verdict(doc, findings)
}

// ReadUnder makes compileEngine read every document but a run's spec through
// documentReader, under the record a cached run keeps of its reads.
func ReadUnder(compileEngine *Engine, documentReader compilers.DocumentReader) {
	compileEngine.under = documentReader
}
