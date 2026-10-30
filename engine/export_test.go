// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package engine

import (
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

// Severities exposes the ranked severity list, which the external tests hold
// to what package ir declares.
func Severities() []ir.Severity { return severities() }

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
