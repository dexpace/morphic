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
	OutcomeOK               Outcome = "ok"
	OutcomePanic            Outcome = "panic"
	OutcomeError            Outcome = "error"
	OutcomeUnrecognized     Outcome = "unrecognized"
	OutcomeViolations       Outcome = "violations"
	OutcomeErrorDiag        Outcome = "error-diagnostic"
	OutcomeRoundtrip        Outcome = "roundtrip-mismatch"
	OutcomeNondeterministic Outcome = "nondeterministic"
	OutcomeNonIdempotent    Outcome = "non-idempotent"
	OutcomeOrderDependent   Outcome = "order-dependent"
)

// Result is one spec's outcome plus human-readable detail. Detail is empty when
// Outcome is OutcomeOK and otherwise describes the first oracle that fired.
type Result struct {
	// Spec identifies the input (its corpus path or logical name).
	Spec string
	// Outcome is the first failing oracle, or OutcomeOK when all passed.
	Outcome Outcome
	// Detail is human-readable context for a non-OK outcome.
	Detail string
}

// Check runs data through the engine as `morphic compile --verify` runs a
// spec, its passes included, under recover() and applies every oracle in order,
// returning the first failure or OutcomeOK. A panic anywhere in the run or the
// oracles is captured as OutcomePanic rather than escaping to the caller. A
// caller mistake (nil ctx, empty spec identifier) is reported as OutcomeError
// with a harness-prefixed Detail, so it is never misattributed to the spec as a
// compiler panic.
func Check(ctx context.Context, spec string, data []byte) (res Result) {
	res = Result{Spec: spec, Outcome: OutcomeOK}
	defer func() {
		if r := recover(); r != nil {
			res = Result{Spec: spec, Outcome: OutcomePanic, Detail: fmt.Sprint(r)}
		}
	}()

	if ctx == nil {
		return Result{Spec: spec, Outcome: OutcomeError, Detail: "harness: nil context"}
	}
	if spec == "" {
		return Result{Spec: spec, Outcome: OutcomeError, Detail: "harness: empty spec identifier"}
	}

	doc, diags, err := compile(ctx, spec, data, passes())
	if errors.Is(err, errUnrecognized) {
		return Result{Spec: spec, Outcome: OutcomeUnrecognized, Detail: err.Error()}
	}
	if verr, ok := errors.AsType[*engine.VerifyError](err); ok {
		return Result{Spec: spec, Outcome: OutcomeViolations, Detail: fmt.Sprintf("%s: %+v", verr.Stage, verr.Violations)}
	}
	if err != nil {
		return Result{Spec: spec, Outcome: OutcomeError, Detail: err.Error()}
	}
	// Before the spec's own errors: a document breaking the invariants is a
	// compiler's or a pass's bug whatever the spec says, and the run returns it.
	// Only a refusal, an error and no document, has nothing to hold. The run's
	// verification does not reach the findings the engine writes onto the
	// document after its last stage (#1121), so this hold stays beside it.
	first, failed := ir.FirstError(diags)
	if doc != nil || !failed {
		if vs := irverify.Verify(doc); len(vs) > 0 {
			return Result{Spec: spec, Outcome: OutcomeViolations, Detail: fmt.Sprintf("%+v", vs)}
		}
	}
	if failed {
		return Result{Spec: spec, Outcome: OutcomeErrorDiag, Detail: first.Code + ": " + first.Message}
	}
	if detail, ok := roundTrip(doc); !ok {
		return Result{Spec: spec, Outcome: OutcomeRoundtrip, Detail: detail}
	}
	if detail, ok := deterministic(ctx, spec, data, doc); !ok {
		return Result{Spec: spec, Outcome: OutcomeNondeterministic, Detail: detail}
	}
	// After deterministic: a pass that is not even same-input-same-output would
	// read as one that changes its own output.
	if detail, ok := idempotent(ctx, spec, data, doc); !ok {
		return Result{Spec: spec, Outcome: OutcomeNonIdempotent, Detail: detail}
	}
	// Last, and after deterministic: this one permutes the input, so a compiler
	// that is not even same-input-same-output should be reported as that rather
	// than as order-dependence.
	if detail, ok := orderInvariant(ctx, spec, data); !ok {
		return Result{Spec: spec, Outcome: OutcomeOrderDependent, Detail: detail}
	}
	return res
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
// each document a compiler or a pass returns, in every run an oracle makes, is
// held to irverify's invariants, and each pass to leaving the document it is
// handed as it was. The walk costs a run more than its compile does; the sweep
// pays it for the reach. A verify error on the first compile is
// OutcomeViolations; on a later run, the oracle making it reports it, with the
// engine's account in its detail.
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
// goes on to Check's recover as it would have without the engine's.
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

// deterministic marshals the first compile's IR, recompiles the same bytes, and
// asserts the two marshaled forms are byte-identical (invariants #5, #7).
func deterministic(ctx context.Context, spec string, data []byte, doc *ir.Document) (string, bool) {
	first, err := json.Marshal(doc)
	if err != nil {
		return "marshal first: " + err.Error(), false
	}
	recompiled, _, err := compile(ctx, spec, data, passes())
	if err != nil {
		return "recompile: " + err.Error(), false
	}
	second, err := reserializeJSON(recompiled)
	if err != nil {
		return "marshal second: " + err.Error(), false
	}
	if !bytes.Equal(first, second) {
		return "IR JSON differs across two compiles", false
	}
	return "", true
}

// Report renders results sorted by spec name into a stable multi-line summary:
// one line per spec, plus one more for every newline a Detail carries, as the
// round-trip oracle's does. Columns are sized from the results rendered, so a
// spec path never runs into its outcome, and line up where each result begins.
//
// It copies its input, so the caller's order is preserved. The sort is stable
// so that results named alike keep the order the caller gave them, not an
// unspecified one.
func Report(results []Result) string {
	sorted := slices.Clone(results)
	slices.SortStableFunc(sorted, func(a, b Result) int { return cmp.Compare(a.Spec, b.Spec) })

	specWidth, outcomeWidth := columnWidths(sorted)

	var b strings.Builder
	for _, r := range sorted {
		// strings.Builder.Write never returns an error; the discard is explicit
		// so no write in this codebase is dropped silently.
		if r.Detail == "" {
			_, _ = fmt.Fprintf(&b, "%-*s %s\n", specWidth, r.Spec, r.Outcome)
			continue
		}
		_, _ = fmt.Fprintf(&b, "%-*s %-*s %s\n", specWidth, r.Spec, outcomeWidth, r.Outcome, r.Detail)
	}
	return b.String()
}

// columnWidths returns the widths Report pads its first two columns to: the
// longest spec, and the longest outcome among the results that carry a Detail.
// A result with no Detail has nothing to the right of its outcome to line up, so
// it is not what the outcome column is sized against and its own line stops at
// the outcome rather than padding out to one.
//
// Widths are counted in runes because that is the unit fmt's %-*s pads in.
func columnWidths(results []Result) (spec, outcome int) {
	for _, r := range results {
		spec = max(spec, utf8.RuneCountInString(r.Spec))
		if r.Detail == "" {
			continue
		}
		outcome = max(outcome, utf8.RuneCountInString(string(r.Outcome)))
	}
	return spec, outcome
}
