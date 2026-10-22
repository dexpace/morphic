package engine

import (
	"context"
	"errors"
	"fmt"
	"reflect"
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

// errSourcesMoved reports a pass that broke its contract by dropping, moving or
// replacing an entry of Document.Sources. The findings the engine carries over,
// and the IR's own provenance, index that table, so the Result would send them
// to the wrong file.
var errSourcesMoved = errors.New("moved, replaced or dropped an entry of Document.Sources")

// errSourceOutOfRange reports a pass that broke its contract with a diagnostic
// indexing no entry of Document.Sources, which the Result promises every one it
// carries does (Result.Sources).
var errSourceOutOfRange = errors.New("reported a diagnostic indexing no source")

// Passes returns the default passes in the order a run takes them, in a list
// of the caller's own: the list New runs, and the one an embedder composes its
// own from (WithPasses). No other code holds that order. An analysis that
// judges the document, validate among them, goes after every transform, so it
// checks what the run returns; architecture.md §2.2 gives the reasons.
func Passes() []pass.Pass {
	return []pass.Pass{pass.Validator{}}
}

// checkPasses refuses a pass list a run could not address by name: a nil pass,
// one with no name, and two sharing a name, which turning one off would turn
// off together. Each is a Go error naming the position, as a refused compiler
// is.
func checkPasses(ps []pass.Pass) error {
	at := make(map[string]int, len(ps))
	for i, p := range ps {
		if isNilPass(p) {
			return fmt.Errorf("engine: pass %d is nil", i)
		}
		name := p.Name()
		if name == "" {
			return fmt.Errorf("engine: pass %d has no name, which a run turns it off by", i)
		}
		if first, taken := at[name]; taken {
			return fmt.Errorf("engine: passes %d and %d are both named %q; turning one off would turn off both",
				first, i, name)
		}
		at[name] = i
	}
	return nil
}

// isNilPass reports whether p is unsafe to call: an untyped nil interface or a
// typed nil pointer, which a method with a value receiver dereferences.
func isNilPass(p pass.Pass) bool {
	if p == nil {
		return true
	}
	rv := reflect.ValueOf(p)
	return rv.Kind() == reflect.Pointer && rv.IsNil()
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
// A pass takes no context, so ctx is checked before each one: a cancelled run
// stops at the next pass rather than running the rest.
func runPasses(ctx context.Context, doc *ir.Document, passes []pass.Pass) (*ir.Document, []ir.Diagnostic, error) {
	var found []ir.Diagnostic
	for _, p := range passes {
		if err := ctx.Err(); err != nil {
			return nil, nil, fmt.Errorf("engine: stop before pass %q: %w", p.Name(), err)
		}
		next, diags := p.Run(doc)
		if next == nil {
			return nil, nil, fmt.Errorf("engine: run pass %q: %w", p.Name(), errNoDocument)
		}
		if !keepsSources(doc.Sources, next.Sources) {
			return nil, nil, fmt.Errorf("engine: run pass %q: %w", p.Name(), errSourcesMoved)
		}
		if d, outside := ir.FirstOutsideTable(diags, len(next.Sources)); outside {
			return nil, nil, fmt.Errorf("engine: run pass %q: %s at source %d of %d: %w",
				p.Name(), d.Code, d.Provenance.Source, len(next.Sources), errSourceOutOfRange)
		}
		doc, found = next, append(found, diags...)
	}
	return doc, found, nil
}

// keepsSources reports whether next holds every entry of prev at the index prev
// gives it, which is all a Provenance.Source already made needs.
func keepsSources(prev, next []ir.SourceInfo) bool {
	return len(next) >= len(prev) && slices.Equal(prev, next[:len(prev)])
}
