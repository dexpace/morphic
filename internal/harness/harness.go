// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package harness

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/dexpace/morphic/barrier"
	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/engine"
	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/ir/irverify"
	"github.com/dexpace/morphic/pass"
)

// Outcome classifies how a spec fared under the oracles.
type Outcome string

// Oracle outcomes, in the order Check applies them. OutcomeOK is the only
// non-finding value; every other outcome marks a spec the harness flags.
// OutcomeUnrecognized is a source no registered compiler takes: its Detail names
// the format when one was recognized and none serves it.
const (
	OutcomeOK                     Outcome = "ok"
	OutcomePanic                  Outcome = "panic"
	OutcomeError                  Outcome = "error"
	OutcomeUnrecognized           Outcome = "unrecognized"
	OutcomeViolations             Outcome = "violations"
	OutcomeErrorDiag              Outcome = "error-diagnostic"
	OutcomeRoundtrip              Outcome = "roundtrip-mismatch"
	OutcomeNondeterministic       Outcome = "nondeterministic"
	OutcomeNonIdempotent          Outcome = "non-idempotent"
	OutcomeOrderDependent         Outcome = "order-dependent"
	OutcomeSequenceOrderDependent Outcome = "sequence-order-dependent"
)

// Result is one spec's outcome plus human-readable detail, and every oracle's
// verdict on it.
type Result struct {
	// Spec identifies the input (its corpus path or logical name).
	Spec string
	// Outcome is the first oracle's finding, in Check's order, the fault that
	// ended the run (OutcomeError, OutcomeUnrecognized), or OutcomeOK.
	Outcome Outcome
	// Detail is human-readable context for a non-OK outcome.
	Detail string
	// Verdicts holds one Verdict per oracle, in the order Oracles lists them:
	// what each found, or why it asked nothing.
	Verdicts []Verdict
}

// Check runs data through the engine as `morphic compile --verify` runs a
// spec, its passes included, behind barrier.Catch, and applies every oracle in
// order. An error diagnostic stops none of them: each oracle with something to
// compare compares it, and one with nothing declines, saying why. A panic
// anywhere is OutcomePanic, and the oracles not yet run decline as not reached.
// A caller mistake (nil ctx, empty spec identifier) is OutcomeError with a
// harness-prefixed Detail, so it is never misattributed to the spec.
func Check(ctx context.Context, spec string, data []byte) Result {
	if ctx == nil {
		return ended(spec, OutcomeError, "harness: nil context", nil)
	}
	if spec == "" {
		return ended(spec, OutcomeError, "harness: empty spec identifier", nil)
	}

	var verdicts []Verdict
	var fault error
	if err := barrier.Catch(func() {
		fault = check(ctx, spec, data, func(v Verdict) { verdicts = append(verdicts, v) })
	}); err != nil {
		return judge(spec, append([]Verdict{found(OracleNoPanic, panicDetail(err))}, verdicts...))
	}
	verdicts = append([]Verdict{passed(OracleNoPanic)}, verdicts...)
	if errors.Is(fault, errUnrecognized) {
		return ended(spec, OutcomeUnrecognized, fault.Error(), verdicts)
	}
	if fault != nil {
		return ended(spec, OutcomeError, fault.Error(), verdicts)
	}
	return judge(spec, verdicts)
}

// ended is the Result of a run a fault ended before any oracle found anything,
// outcome and detail saying which fault: every oracle that gave no verdict
// declines as not reached.
func ended(spec string, outcome Outcome, detail string, verdicts []Verdict) Result {
	res := judge(spec, verdicts)
	res.Outcome, res.Detail = outcome, detail
	return res
}

// judge is the Result verdicts make: completed with a not-reached verdict from
// each oracle that gave none, and the first finding as its outcome.
func judge(spec string, verdicts []Verdict) Result {
	answered := make(map[Oracle]Verdict, len(verdicts))
	for _, v := range verdicts {
		answered[v.Oracle] = v
	}
	res := Result{Spec: spec, Outcome: OutcomeOK}
	for _, o := range Oracles() {
		v, ok := answered[o]
		if !ok {
			v = declined(o, DeclineNotReached)
		}
		if v.Found() && res.Outcome == OutcomeOK {
			res.Outcome, res.Detail = v.Outcome, v.Detail
		}
		res.Verdicts = append(res.Verdicts, v)
	}
	return res
}

// panicDetail is what err, a panic Check's barrier caught, says. A compiler's
// or a pass's panic that reraise raised again says what it said where it was
// first raised, rather than naming reraise as its site.
func panicDetail(err error) string {
	if raised, ok := errors.AsType[*barrier.PanicError](errors.Unwrap(err)); ok {
		return raised.Error()
	}
	return err.Error()
}

// check runs data through the oracles in order, handing each verdict to give,
// and returns the fault that ends the run before any oracle can ask: a source
// no compiler takes (errUnrecognized), or a Go error from the compile. A
// verified run the engine refused is the invariants oracle's finding, and
// nothing follows it, since the engine returns no document.
func check(ctx context.Context, spec string, data []byte, give func(Verdict)) error {
	doc, diags, err := compile(ctx, spec, data, passes())
	if verr, ok := errors.AsType[*engine.VerifyError](err); ok {
		give(found(OracleInvariants, fmt.Sprintf("%s: %+v", verr.Stage, verr.Violations)))
		return nil
	}
	if err != nil {
		return err
	}

	give(invariants(doc, diags))
	give(errorFree(diags))
	if doc == nil {
		give(declined(OracleRoundTrip, DeclineNoDocument))
	} else {
		give(OracleRoundTrip.judge(roundTrip(doc)))
	}
	// Determinism comes before the oracles that run the source again another
	// way, so a compiler that is not even same-input-same-output is reported
	// as that first, rather than as a pass changing its own output or as
	// order-dependence.
	give(OracleDeterminism.judge(deterministic(ctx, spec, data, doc, diags)))
	if doc == nil {
		give(declined(OracleIdempotence, DeclineNoDocument))
	} else {
		give(OracleIdempotence.judge(idempotent(ctx, spec, data, doc)))
	}
	give(orderInvariant(ctx, spec, data, doc != nil))
	give(sequenceOrderInvariant(ctx, spec, data, doc != nil))
	return nil
}

// invariants holds what the run returned to irverify's invariants: a document
// breaking one is a compiler's or a pass's bug whatever the spec says. Only a
// refusal, an error and no document, has nothing to hold. The run's
// verification holds each stage's document and findings (#1121); this hold is
// Check's own, of whatever compile returns, no document beside no error
// included.
func invariants(doc *ir.Document, diags []ir.Diagnostic) Verdict {
	if _, failed := ir.FirstError(diags); doc == nil && failed {
		return declined(OracleInvariants, DeclineNoDocument)
	}
	if vs := irverify.Verify(doc); len(vs) > 0 {
		return found(OracleInvariants, fmt.Sprintf("%+v", vs))
	}
	return passed(OracleInvariants)
}

// errorFree finds the first error diagnostic the run reported, the spec's own
// account of what is wrong with it.
func errorFree(diags []ir.Diagnostic) Verdict {
	if first, failed := ir.FirstError(diags); failed {
		return found(OracleNoErrorDiagnostic, first.Code+": "+first.Message)
	}
	return passed(OracleNoErrorDiagnostic)
}

// errUnrecognized marks a source no registered compiler takes. Check reports it
// as OutcomeUnrecognized rather than as a fault of the harness.
var errUnrecognized = errors.New("unrecognized source")

// registered is the compiler set a sweep compiles with: the engine's built-ins,
// so a compiler registered there is swept with no change here. It is a seam, as
// compile is, for tests that need a compiler the engine does not carry.
var registered = engine.Compilers

// passes is the pass list a sweep runs: the engine's default, so a pass that
// joins it is swept, and held to the idempotence oracle, with no change here.
// It is a seam, as registered is, for tests that need a pass the default list
// does not hold.
var passes = engine.Passes

// runOptions is what every engine run a sweep makes runs under: verified, so
// each document a compiler or a pass returns, and the findings each stage
// reports, in every run an oracle makes, are held to irverify's invariants,
// and each pass to leaving the document it is handed as it was. The walk costs
// a run more than its compile does; the sweep pays it for the reach. A verify
// error on the first compile is OutcomeViolations; on a later run, the oracle
// making it reports it, with the engine's account in its detail.
func runOptions() engine.RunOptions { return engine.RunOptions{Verify: true} }

// newEngine builds the engine a sweep runs: the registered compilers, and ps
// as its passes, in order.
func newEngine(ps []pass.Pass) (*engine.Engine, error) {
	e, err := engine.New(engine.WithCompilers(registered()...), engine.WithPasses(ps...))
	if err != nil {
		return nil, fmt.Errorf("harness: %w", err)
	}
	return e, nil
}

// compile runs one in-memory source through the engine as `morphic compile
// --verify` runs a spec, with ps as the passes: data is held as the file at
// spec, and what its references name is read from disk beside it. It returns
// the document and every finding of the run, the compiler's and the passes', or
// errUnrecognized for a source no registered compiler takes.
//
// It is a package-level seam: production always runs the engine, while tests
// replace it to drive the oracle-failure paths a correct compiler never
// produces: a panic, structural violations, a non-round-tripping document, or
// a nondeterministic recompile.
var compile = func(ctx context.Context, spec string, data []byte, ps []pass.Pass) (*ir.Document, []ir.Diagnostic, error) {
	e, err := newEngine(ps)
	if err != nil {
		return nil, nil, err
	}
	res, err := e.RunSpec(ctx, compilers.Source{Path: spec, Data: data}, runOptions())
	if err != nil {
		return nil, nil, fmt.Errorf("harness: %w", reraise(err))
	}
	if d, ok := ir.FirstError(res.Diagnostics); ok && undetected(d.Code) {
		return nil, nil, fmt.Errorf("%s: %w", d.Message, errUnrecognized)
	}
	return res.Document, res.Diagnostics, nil
}

// undetected reports whether code is one the engine raises, with no document,
// for a source no registered compiler takes: one nobody recognized, or one in
// a format this build does not compile, as with Swagger 2.0. A compiler
// declining a source with its own diagnostics has said the source is its own
// and broken, which is a refusal like any other.
func undetected(code string) bool {
	return code == engine.CodeUnrecognizedFormat || code == engine.CodeNoCompilerForFormat
}

// reraise returns err, or raises again the panic it records. The engine
// recovers a compiler's or a pass's panic into an error, and the first oracle
// is that none occurs, in every run an oracle makes of a source, so the panic
// goes on to Check's barrier as it would have without the engine's.
func reraise(err error) error {
	if perr, ok := errors.AsType[*barrier.PanicError](err); ok {
		panic(perr)
	}
	return err
}

// reserializeJSON re-encodes a value that has already survived a marshal or a
// compile: the decoded document in roundTrips, the recompiled document in
// deterministic, and each document the idempotence oracle compares, which a
// verified run has encoded already. It is a package-level seam over
// json.Marshal, where such a value can never fail to re-marshal; tests replace
// it to exercise that otherwise-unreachable defensive error path.
var reserializeJSON = json.Marshal

// roundTrip is the round-trip oracle Check applies: a package-level seam over
// roundTrips for the same reason compile is one. A test swaps it to drive
// Check's OutcomeRoundtrip classification directly, so that test does not
// depend on which documents Verify lets through to this oracle.
var roundTrip = roundTrips

// roundTrips marshals doc, unmarshals into a fresh Document, re-marshals, and
// compares the two encodings byte for byte rather than doc against the decoded
// value. An empty payload decodes back as nil, which a struct comparison would
// report and the wire cannot express; what the bytes still catch is any decode
// that loses or alters something the document says.
func roundTrips(doc *ir.Document) (string, bool) {
	first, err := json.Marshal(doc)
	if err != nil {
		return "marshal: " + err.Error(), false
	}
	var back ir.Document
	if err := json.Unmarshal(first, &back); err != nil {
		return "unmarshal: " + err.Error(), false
	}
	second, err := reserializeJSON(&back)
	if err != nil {
		return "remarshal: " + err.Error(), false
	}
	if !bytes.Equal(first, second) {
		return "round-trip JSON differs:\n first: " + string(first) + "\nsecond: " + string(second), false
	}
	return "", true
}

// deterministic marshals what the first compile returned, recompiles the same
// bytes, and asserts the two marshaled forms are byte-identical (invariants #5,
// #7). What a compile returned is its document, or for a refusal its findings.
func deterministic(ctx context.Context, spec string, data []byte, doc *ir.Document, diags []ir.Diagnostic) (string, bool) {
	first, err := json.Marshal(outputOf(doc, diags))
	if err != nil {
		return "marshal first: " + err.Error(), false
	}
	recompiled, again, err := compile(ctx, spec, data, passes())
	if err != nil {
		return "recompile: " + err.Error(), false
	}
	second, err := reserializeJSON(outputOf(recompiled, again))
	if err != nil {
		return "marshal second: " + err.Error(), false
	}
	if !bytes.Equal(first, second) {
		return "IR JSON differs across two compiles", false
	}
	return "", true
}

// outputOf is what a compile returned: its document, which holds every
// finding, or the findings of a refusal, which returns none.
func outputOf(doc *ir.Document, diags []ir.Diagnostic) any {
	if doc == nil {
		return diags
	}
	return doc
}

// Report renders results sorted by spec name into a stable multi-line summary:
// one line per finding a result's verdicts hold, or one for its outcome when
// they hold none, plus one more for every newline a Detail carries, as the
// round-trip oracle's does. Columns are sized from the lines rendered, so a
// spec path never runs into its outcome, and line up where each line begins.
//
// It copies its input, so the caller's order is preserved. The sort is stable
// so that results named alike keep the order the caller gave them, not an
// unspecified one.
func Report(results []Result) string {
	sorted := slices.Clone(results)
	slices.SortStableFunc(sorted, func(a, b Result) int { return cmp.Compare(a.Spec, b.Spec) })

	lines := make([]reportLine, 0, len(sorted)) // a line per result at least
	for _, r := range sorted {
		lines = append(lines, linesOf(r)...)
	}
	specWidth, outcomeWidth := columnWidths(lines)

	var b strings.Builder
	for _, l := range lines {
		// strings.Builder.Write never returns an error; the discard is explicit
		// so no write in this codebase is dropped silently.
		if l.detail == "" {
			_, _ = fmt.Fprintf(&b, "%-*s %s\n", specWidth, l.spec, l.outcome)
			continue
		}
		_, _ = fmt.Fprintf(&b, "%-*s %-*s %s\n", specWidth, l.spec, outcomeWidth, l.outcome, l.detail)
	}
	return b.String()
}

// reportLine is one line Report renders.
type reportLine struct {
	spec, detail string
	outcome      Outcome
}

// linesOf is what Report renders for r: a line per finding among its verdicts,
// in Check's order, or its outcome alone.
func linesOf(r Result) []reportLine {
	var lines []reportLine
	for _, v := range r.Verdicts {
		if v.Found() {
			lines = append(lines, reportLine{spec: r.Spec, outcome: v.Outcome, detail: v.Detail})
		}
	}
	if len(lines) == 0 {
		lines = append(lines, reportLine{spec: r.Spec, outcome: r.Outcome, detail: r.Detail})
	}
	return lines
}

// columnWidths returns the widths Report pads its first two columns to: the
// longest spec, and the longest outcome among the lines that carry a detail. A
// line with no detail has nothing to the right of its outcome to line up, so
// it is not what the outcome column is sized against and stops at the outcome
// rather than padding out to one.
//
// Widths are counted in runes because that is the unit fmt's %-*s pads in.
func columnWidths(lines []reportLine) (spec, outcome int) {
	for _, l := range lines {
		spec = max(spec, utf8.RuneCountInString(l.spec))
		if l.detail == "" {
			continue
		}
		outcome = max(outcome, utf8.RuneCountInString(string(l.outcome)))
	}
	return spec, outcome
}
