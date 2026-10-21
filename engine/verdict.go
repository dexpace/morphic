package engine

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/dexpace/morphic/ir"
)

// FailOn is the lowest diagnostic severity that fails a run, spelled as that
// severity, or FailOnNone. The engine judges a run once its passes have
// reported and records the verdict as Result.Failed, so the CLI's exit status
// and any other driver's decision to go on rest on one rule rather than on
// each caller's reading of the diagnostics.
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

// severities lists the IR's severities, least severe first, so a severity's
// position is its rank and a threshold is a plain integer compare.
func severities() []ir.Severity {
	return []ir.Severity{ir.SeverityInfo, ir.SeverityWarning, ir.SeverityError}
}

// threshold is a FailOn that resolve accepted: the rank of the least severe
// diagnostic that fails a run. FailOnNone's lies past every severity's, so no
// diagnostic reaches it. Being a type of its own keeps an unresolved FailOn
// from judging a run, and its zero value fails a run on any diagnostic, so a
// threshold never resolved fails closed.
type threshold int

// resolve reads f as the threshold a run is judged at, the zero value as
// FailOnError, and refuses a value naming none.
func (f FailOn) resolve() (threshold, error) {
	if f == "" {
		f = FailOnError
	}
	known := severities()
	if f == FailOnNone {
		return threshold(len(known)), nil
	}
	if rank := slices.Index(known, ir.Severity(f)); rank >= 0 {
		return threshold(rank), nil
	}
	return 0, unknownFailOn(string(f))
}

// unknownFailOn reports s as naming no threshold, and names every one that
// exists, most severe first.
func unknownFailOn(s string) error {
	known := severities()
	names := make([]string, 0, len(known)+1)
	for _, sev := range slices.Backward(known) {
		names = append(names, string(sev))
	}
	names = append(names, string(FailOnNone))
	return fmt.Errorf("engine: fail-on %q (known: %s): %w", s, strings.Join(names, ", "), ErrUnknownFailOn)
}

// verdict reports whether res fails at t: it fails when it holds no document,
// and otherwise when a diagnostic's severity ranks at t or above. A severity
// the IR does not define is refused at every threshold, FailOnNone's included,
// because a stage that broke its contract is the caller's to hear about
// whatever gate they chose.
func (t threshold) verdict(res *Result) (bool, error) {
	known := severities()
	failed := res.Document == nil
	for _, d := range res.Diagnostics {
		rank := slices.Index(known, d.Severity)
		if rank < 0 {
			return false, fmt.Errorf("engine: diagnostic %q has severity %q: %w",
				d.Code, d.Severity, errUnknownSeverity)
		}
		failed = failed || threshold(rank) >= t
	}
	return failed, nil
}

// judge records res's verdict at t as Result.Failed and returns res, or the
// error the verdict refused it with.
func (t threshold) judge(res *Result) (*Result, error) {
	failed, err := t.verdict(res)
	if err != nil {
		return nil, err
	}
	res.Failed = failed
	return res, nil
}
