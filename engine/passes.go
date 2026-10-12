package engine

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/pass"
)

// ValidatePass names the validate pass, for a caller that does not import pass.
const ValidatePass = pass.ValidateName

// ErrUnknownPass reports a RunOptions.DisablePasses name that none of the
// engine's passes carries. It is a Go error rather than a diagnostic because it
// names no defect in the spec: the run was misconfigured.
var ErrUnknownPass = errors.New("unknown pass")

// errNoDocument reports a pass that broke its contract by returning no
// document. Keeping the previous one would hide the bug, and stopping without
// one would report the spec as refused.
var errNoDocument = errors.New("returned no document")

// defaultPasses returns the passes every engine runs, in the order it runs
// them. This is the one place that order is written down.
func defaultPasses() []pass.Pass {
	return []pass.Pass{pass.Validator{}}
}

// enabledPasses returns, in run order, those of e's passes that disabled does
// not name, and refuses a name none of them carries, for the reason
// RunOptions.DisablePasses gives.
func (e *Engine) enabledPasses(disabled []string) ([]pass.Pass, error) {
	names := make([]string, 0, len(e.passes))
	for _, p := range e.passes {
		names = append(names, p.Name())
	}
	for _, name := range disabled {
		if !slices.Contains(names, name) {
			return nil, fmt.Errorf("engine: disable %q (known: %s): %w",
				name, strings.Join(names, ", "), ErrUnknownPass)
		}
	}

	return slices.DeleteFunc(slices.Clone(e.passes), func(p pass.Pass) bool {
		return slices.Contains(disabled, p.Name())
	}), nil
}

// runPasses runs passes in order, each over the document the one before it
// returned, and returns the last document with every diagnostic they reported.
func runPasses(doc *ir.Document, passes []pass.Pass) (*ir.Document, []ir.Diagnostic, error) {
	var found []ir.Diagnostic
	for _, p := range passes {
		next, diags := p.Run(doc)
		if next == nil {
			return nil, nil, fmt.Errorf("engine: run pass %q: %w", p.Name(), errNoDocument)
		}
		doc, found = next, append(found, diags...)
	}
	return doc, found, nil
}
