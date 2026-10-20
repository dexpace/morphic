package engine

import (
	"errors"
	"fmt"
	"slices"

	"github.com/dexpace/morphic/ir"
)

// FailOn is the lowest diagnostic severity that fails a run. The engine reads
// it once a run's every stage has reported, and records the verdict as
// Result.Failed, so the CLI's exit status and any other driver's decision to go
// on rest on one rule rather than each caller's reading of the diagnostics.
type FailOn string

// The thresholds a run takes. The zero value is FailOnError.
const (
	// FailOnError fails a run on any error diagnostic: the document may say
	// something the spec does not, so nothing should be built on it unasked.
	FailOnError FailOn = "error"
	// FailOnWarning fails a run on a warning too, for a stricter gate.
	FailOnWarning FailOn = "warning"
	// FailOnNone fails only a run that produced no document, for a caller that
	// wants the document whatever its diagnostics say.
	FailOnNone FailOn = "none"
)

// ErrUnknownFailOn reports a RunOptions.FailOn naming no threshold. It is a Go
// error rather than a diagnostic for the reason ErrUnknownPass is: the run was
// misconfigured, and the spec is not at fault.
var ErrUnknownFailOn = errors.New("unknown fail-on threshold")

// failOnValues lists every threshold in the order a message names them.
var failOnValues = []FailOn{FailOnError, FailOnWarning, FailOnNone}

// ParseFailOn reads a threshold spelled as its value, for a caller taking it
// as text. The empty string is refused here, unlike in RunOptions: a caller
// that read text has a value, and an empty one is a mistake to report rather
// than the default to assume.
func ParseFailOn(s string) (FailOn, error) {
	f := FailOn(s)
	if !slices.Contains(failOnValues, f) {
		return "", fmt.Errorf("engine: fail-on %q (want %s, %s or %s): %w",
			s, FailOnError, FailOnWarning, FailOnNone, ErrUnknownFailOn)
	}
	return f, nil
}

// threshold resolves the zero value to FailOnError and refuses a value naming
// no threshold.
func (f FailOn) threshold() (FailOn, error) {
	if f == "" {
		return FailOnError, nil
	}
	return ParseFailOn(string(f))
}

// fails reports whether a run with a document and diags fails at threshold f,
// which threshold has already resolved. FailOnError and FailOnWarning are
// spelled as the severities they name, and FailOnNone as none, so it ranks
// with the severities the IR does not define: below every diagnostic.
func (f FailOn) fails(diags []ir.Diagnostic) bool {
	least := severityRank(ir.Severity(f))
	if least == 0 {
		return false // FailOnNone: a document is enough.
	}
	return slices.ContainsFunc(diags, func(d ir.Diagnostic) bool {
		return severityRank(d.Severity) >= least
	})
}

// severityRank orders severities so a threshold comparison is a plain integer
// compare: error > warning > info > anything else.
func severityRank(s ir.Severity) int {
	switch s {
	case ir.SeverityError:
		return 3
	case ir.SeverityWarning:
		return 2
	case ir.SeverityInfo:
		return 1
	default:
		return 0
	}
}
