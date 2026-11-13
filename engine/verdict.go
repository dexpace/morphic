// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package engine

import (
	"errors"
	"fmt"
	"strings"

	"github.com/dexpace/morphic/engine/triage"
	"github.com/dexpace/morphic/ir"
)

// FailOn is the lowest severity a finding is rated at that fails a run,
// spelled as that severity, or FailOnNone. The engine judges a run once its
// passes have reported and its triage has rated their findings, and records
// the verdict as Result.Failed, so the CLI's exit status and any other driver's
// decision to go on rest on one rule rather than on each caller's reading of
// the diagnostics.
type FailOn string

// The thresholds a run takes, from the default through the stricter ones to
// FailOnNone, the most lenient. The zero value is FailOnError.
const (
	// FailOnError fails a run on an error: the document may say something the
	// spec does not, so nothing should be built on it unasked.
	FailOnError = FailOn(ir.SeverityError)
	// FailOnWarning fails a run on a warning too, for a stricter gate.
	FailOnWarning = FailOn(ir.SeverityWarning)
	// FailOnInfo fails a run on any diagnostic, for a gate that admits nothing
	// the compiler had to note, a construct it degraded among them.
	FailOnInfo = FailOn(ir.SeverityInfo)
	// FailOnNone fails only a run that produced no document, for a caller that
	// wants the document whatever its diagnostics say.
	FailOnNone FailOn = "none"
)

// ErrUnknownFailOn reports a RunOptions.FailOn naming no threshold. It is a Go
// error rather than a diagnostic for the reason ErrUnknownPass is: the run was
// misconfigured, and the spec is not at fault.
var ErrUnknownFailOn = errors.New("unknown fail-on threshold")

// errUnknownSeverity reports a stage that broke its contract by reporting a
// diagnostic of a severity the IR does not define. No threshold can rank it,
// and ranking it below them all would let a misspelt error through every gate.
var errUnknownSeverity = errors.New("unknown diagnostic severity")

// ParseFailOn reads a threshold spelled as its value, for a caller taking it
// as text. The empty string is refused here, unlike in RunOptions: a caller
// that read text was handed a value, and an empty one is a mistake to report
// rather than the default to assume.
func ParseFailOn(s string) (FailOn, error) {
	if s == "" {
		return "", unknownFailOn(s)
	}
	if _, err := FailOn(s).resolve(); err != nil {
		return "", err
	}
	return FailOn(s), nil
}

// threshold is a FailOn that resolve accepted: the rank of the least severe
// finding that fails a run. FailOnNone's lies past every severity's, so no
// finding reaches it. Being a type of its own keeps an unresolved FailOn
// from judging a run, and its zero value fails a run on every finding no rule
// accepted, so a threshold never resolved fails closed.
type threshold int

// resolve reads f as the threshold a run is judged at, the zero value as
// FailOnError, and refuses a value naming none.
func (f FailOn) resolve() (threshold, error) {
	if f == "" {
		f = FailOnError
	}
	if f == FailOnNone {
		return threshold(len(ir.Severities())), nil
	}
	if rank, known := ir.Severity(f).Rank(); known {
		return threshold(rank), nil
	}
	return 0, unknownFailOn(string(f))
}

// unknownFailOn reports s as naming no threshold, and names every one that
// exists, most severe first.
func unknownFailOn(s string) error {
	names := append(ir.SeverityNames(), string(FailOnNone))
	return fmt.Errorf("engine: fail-on %q (known: %s): %w", s, strings.Join(names, ", "), ErrUnknownFailOn)
}

// verdict reports whether a run that produced doc and findings fails at t: it
// fails when it holds no document, and otherwise when a finding no rule
// accepted is rated at t or above. A finding reported at a severity the IR does
// not declare is refused at every threshold, FailOnNone's included, whatever a
// rule rates it at, because a stage that broke its contract is the caller's to
// hear about whatever gate they chose.
func (t threshold) verdict(doc *ir.Document, findings []triage.Finding) (bool, error) {
	failed := doc == nil
	for _, f := range findings {
		d := f.Diagnostic
		if !d.Severity.Valid() {
			return false, fmt.Errorf("engine: diagnostic %q has severity %q: %w",
				d.Code, d.Severity, errUnknownSeverity)
		}
		rank, ranked := f.Severity.Rank()
		if !ranked {
			return false, fmt.Errorf("engine: diagnostic %q is rated at severity %q: %w",
				d.Code, f.Severity, errUnknownSeverity)
		}
		failed = failed || !f.Accepted && threshold(rank) >= t
	}
	return failed, nil
}

// judge rates res's findings by the plan's rules, records them and the rules
// none matched on res, and its verdict at the plan's threshold as
// Result.Failed. It returns res, or the error the verdict refused it with.
//
// A run that produced no document stopped short of most of the findings a
// rule is written for, so it lists no rule as unused: none it missed is shown
// to have outlived its finding.
func (p plan) judge(res *Result) (*Result, error) {
	findings, unused := p.rules.Rate(res.Diagnostics)
	failed, err := p.failOn.verdict(res.Document, findings)
	if err != nil {
		return nil, err
	}
	if res.Document == nil {
		unused = nil
	}
	res.Findings, res.UnusedRules, res.Failed = findings, unused, failed
	return res, nil
}
