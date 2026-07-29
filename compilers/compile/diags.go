package compile

import (
	"strconv"
	"strings"

	"github.com/dexpace/morphic/ir"
)

// Diags accumulates a compile's diagnostics, dropping any whose full identity —
// severity, code, message and provenance — repeats one already recorded.
//
// Dedup is what makes lowering a referenced component once at its declaration
// safe to report on: every use site then produces an identical diagnostic, and
// the second copy tells a reader nothing the first did not. Because identity
// includes provenance, two positions that genuinely differ still both surface;
// this collapses repeats, never distinct findings.
//
// Suppression that is broader than identity — silencing a whole pointer once any
// diagnostic lands there — is compiler policy rather than a framework guarantee,
// and stays with the compiler that wants it.
//
// The zero value is ready to use. It is single-compile state and is not safe for
// concurrent use.
type Diags struct {
	list    []ir.Diagnostic
	emitted map[string]bool
}

// Append records d unless one identical to it was already recorded.
func (d *Diags) Append(x ir.Diagnostic) {
	key := identity(x)
	if d.emitted[key] {
		return
	}
	if d.emitted == nil {
		d.emitted = make(map[string]bool)
	}
	d.emitted[key] = true
	d.list = append(d.list, x)
}

// AppendAll records each of xs in order, deduping as Append does. It is the
// entry point for a pure reader that returns its findings as a slice.
func (d *Diags) AppendAll(xs []ir.Diagnostic) {
	for _, x := range xs {
		d.Append(x)
	}
}

// List returns the recorded diagnostics in the order they were first appended.
//
// The slice is the live backing array, not a copy: it is handed to the compiler
// that owns this Diags, on its way out of Compile. Callers must not retain or
// mutate it across further Append calls.
func (d *Diags) List() []ir.Diagnostic { return d.list }

// Len reports how many distinct diagnostics were recorded.
func (d *Diags) Len() int { return len(d.list) }

// identity renders the full identity of d as a map key. NUL separates the
// fields: it cannot occur in a severity, code, pointer, or coerced message, so
// no two distinct diagnostics can collide by concatenation.
func identity(d ir.Diagnostic) string {
	return strings.Join([]string{
		string(d.Severity), d.Code, d.Message,
		strconv.Itoa(d.Provenance.Source), d.Provenance.Pointer, d.Provenance.Inferred,
	}, "\x00")
}
