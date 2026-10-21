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

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/engine"
	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/ir/irverify"
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
	OutcomeErrorDiag        Outcome = "error-diagnostic"
	OutcomeViolations       Outcome = "violations"
	OutcomeRoundtrip        Outcome = "roundtrip-mismatch"
	OutcomeNondeterministic Outcome = "nondeterministic"
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

// Check compiles data through the registered compilers under recover() and
// applies every oracle in order, returning the first failure or OutcomeOK. A
// panic anywhere in compilation or the oracles is captured as OutcomePanic
// rather than escaping to the caller. A caller mistake (nil ctx, empty spec
// identifier) is reported as OutcomeError with a harness-prefixed Detail, so it
// is never misattributed to the spec as a compiler panic.
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

	doc, diags, err := compile(ctx, spec, data)
	if errors.Is(err, errUnrecognized) {
		return Result{Spec: spec, Outcome: OutcomeUnrecognized, Detail: err.Error()}
	}
	if err != nil {
		return Result{Spec: spec, Outcome: OutcomeError, Detail: err.Error()}
	}
	if d, ok := ir.FirstError(diags); ok {
		return Result{Spec: spec, Outcome: OutcomeErrorDiag, Detail: d.Code + ": " + d.Message}
	}
	if vs := irverify.Verify(doc); len(vs) > 0 {
		return Result{Spec: spec, Outcome: OutcomeViolations, Detail: fmt.Sprintf("%+v", vs)}
	}
	if detail, ok := roundTrip(doc); !ok {
		return Result{Spec: spec, Outcome: OutcomeRoundtrip, Detail: detail}
	}
	if detail, ok := deterministic(ctx, spec, data, doc); !ok {
		return Result{Spec: spec, Outcome: OutcomeNondeterministic, Detail: detail}
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

// newRegistry registers the compilers into a registry of its own. Each call
// builds a fresh one, so no sweep shares a registry with another. An empty set
// is refused: every source would then read as unrecognized, blaming the spec
// for a misconfigured harness.
func newRegistry() (*compilers.Registry, error) {
	cs := registered()
	if len(cs) == 0 {
		return nil, errors.New("harness: no compilers registered")
	}

	reg := compilers.NewRegistry()
	for i, c := range cs {
		if err := reg.Register(c); err != nil {
			return nil, fmt.Errorf("harness: register compiler %d: %w", i, err)
		}
	}
	return reg, nil
}

// compile lowers one in-memory source with the compiler that claims it, under
// default options and without the passes, and holds the outcome to the compiler
// contract. It returns the diagnostics Compile returned; findings a compiler
// stores on the Document alone are not read. It is a package-level seam:
// production always uses the registry wired here, while tests replace it to
// drive the oracle-failure paths a correct compiler never produces — a panic, a
// document with structural violations, a non-round-tripping document, or a
// nondeterministic recompile.
var compile = func(ctx context.Context, spec string, data []byte) (*ir.Document, []ir.Diagnostic, error) {
	reg, err := newRegistry()
	if err != nil {
		return nil, nil, err
	}

	src := compilers.Source{Path: spec, Data: data}
	det, ok, err := reg.Detect(ctx, src, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("harness: detect: %w", err)
	}
	if !ok {
		diags, err := unrecognized(det)
		return nil, diags, err
	}

	out, err := det.Compile(ctx, src)
	if err != nil {
		return nil, nil, fmt.Errorf("harness: compile: %w", err)
	}
	return out.Document, out.Diagnostics, nil
}

// unrecognized answers for a source no registered compiler takes. A compiler
// that declined it with an error has said why in its own words, so those
// diagnostics are returned for Check to classify like any refusal. Otherwise the
// source is unrecognized, naming the format a compiler recognized and none
// serves, as with Swagger 2.0.
func unrecognized(det compilers.Detection) ([]ir.Diagnostic, error) {
	if _, stated := ir.FirstError(det.Declined); stated {
		return det.Declined, nil
	}
	if format := det.Recognition.Format; format.Name != "" {
		return nil, fmt.Errorf("no registered compiler serves %s: %w", format, errUnrecognized)
	}
	return nil, fmt.Errorf("no registered compiler recognizes the source: %w", errUnrecognized)
}

// reserializeJSON re-encodes a value that has already survived a marshal or a
// compile: the decoded document in roundTrips and the recompiled document in
// deterministic. It is a package-level seam over json.Marshal that defaults to
// json.Marshal in production, where such a value can never fail to re-marshal;
// tests replace it to exercise that otherwise-unreachable defensive error path.
var reserializeJSON = func(v any) ([]byte, error) { return json.Marshal(v) }

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
	recompiled, _, err := compile(ctx, spec, data)
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
