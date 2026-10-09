package engine

import (
	"errors"
	"fmt"

	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/pass"
)

// ValidatePass is the name of the validate pass, for callers that may not
// import pass.
const ValidatePass = pass.ValidateName

// ErrUnknownPass reports a RunOptions.DisablePasses name that no pass the
// engine composes carries. It is a Go error rather than a diagnostic: an
// unknown option names no defect in the spec and has no provenance to carry,
// so it is the caller's misuse.
var ErrUnknownPass = errors.New("engine: unknown pass")

// errNilPassDocument reports a pass that returned no document, which would
// otherwise silently keep or lose the previous one.
var errNilPassDocument = errors.New("engine: pass returned a nil document")

// DefaultPasses returns the passes a default engine runs, in run order. The
// order is explicit here, and nowhere else, because the engine is the only
// composer of passes.
func DefaultPasses() []pass.Pass {
	return []pass.Pass{pass.NewValidate()}
}

// disabledSet resolves names against passes, refusing any that no pass carries.
func disabledSet(passes []pass.Pass, names []string) (map[string]struct{}, error) {
	known := make(map[string]struct{}, len(passes))
	for _, p := range passes {
		known[p.Name()] = struct{}{}
	}

	set := make(map[string]struct{}, len(names))
	for _, name := range names {
		if _, ok := known[name]; !ok {
			return nil, fmt.Errorf("%w %q", ErrUnknownPass, name)
		}
		set[name] = struct{}{}
	}
	return set, nil
}

// runPasses runs every enabled pass in order, each over the previous one's
// output, and returns the final document with every diagnostic produced.
func runPasses(doc *ir.Document, passes []pass.Pass, disabled map[string]struct{}) (*ir.Document, []ir.Diagnostic, error) {
	cur := doc
	var produced []ir.Diagnostic
	for _, p := range passes {
		if _, off := disabled[p.Name()]; off {
			continue
		}
		next, diags := p.Run(cur)
		if next == nil {
			return nil, nil, fmt.Errorf("%w: %s", errNilPassDocument, p.Name())
		}
		produced = append(produced, diags...)
		cur = next
	}
	return cur, produced, nil
}
