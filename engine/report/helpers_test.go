// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package report_test

import (
	"errors"
	"io"
	"strings"

	"github.com/dexpace/morphic/engine/report"
	"github.com/dexpace/morphic/engine/triage"
	"github.com/dexpace/morphic/ir"
)

// errDisk is the failure the refusing writers return.
var errDisk = errors.New("disk gone")

// failingWriter accepts writes until the failAt-th, which fails, and counts
// every attempt it is asked for, including any after the failure, so a renderer
// that does not stop shows in attempts.
type failingWriter struct {
	failAt   int
	attempts int
	text     strings.Builder
}

func (w *failingWriter) Write(p []byte) (int, error) {
	w.attempts++
	if w.attempts == w.failAt {
		return 0, errDisk
	}
	return w.text.Write(p)
}

// diag returns an error diagnostic with message "m" at prov.
func diag(code string, prov ir.Provenance) ir.Diagnostic {
	return ir.Diagnostic{Severity: ir.SeverityError, Code: code, Message: "m", Provenance: prov}
}

// rated returns diags as findings no rule met, each at its reported severity.
func rated(diags []ir.Diagnostic) []triage.Finding {
	findings, _ := triage.Ruleset{}.Rate(diags)
	return findings
}

// writeDiagnostics writes diags, as findings no rule met, the way WriteText
// writes a run that withholds nothing: one line each.
func writeDiagnostics(w io.Writer, diags []ir.Diagnostic, sources []ir.SourceInfo) error {
	return report.WriteText(w, report.Report{Findings: rated(diags), Sources: sources}, report.TextOptions{})
}
