package compile

import "github.com/dexpace/morphic/ir"

// Diags accumulates a compile's diagnostics, dropping any whose full identity —
// severity, code, message and provenance — repeats one already recorded.
//
// Dedup is what makes lowering a referenced component once at its declaration
// safe to report on: every use site then produces an identical diagnostic, and
// the second copy tells a reader nothing the first did not. Because identity
// includes provenance, two positions that genuinely differ still both surface;
// this collapses repeats, never distinct findings. The key is the whole value,
// as it is in the engine's merge, so a field Diagnostic gains joins the
// identity without an edit here.
//
// Suppression that is broader than identity — silencing a whole pointer once any
// diagnostic lands there — is compiler policy rather than a framework guarantee,
// and stays with the compiler that wants it.
//
// The zero value is ready to use. It is single-compile state and is not safe for
// concurrent use.
type Diags struct {
	list    []ir.Diagnostic
	emitted map[ir.Diagnostic]bool
}

// Append records d unless one identical to it was already recorded.
func (d *Diags) Append(x ir.Diagnostic) {
	if d.emitted[x] {
		return
	}
	if d.emitted == nil {
		d.emitted = make(map[ir.Diagnostic]bool)
	}
	d.emitted[x] = true
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
