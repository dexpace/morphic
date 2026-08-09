package harness

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi"
	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/ir/irverify"
)

// Outcome classifies how a spec fared under the oracles.
type Outcome string

// Oracle outcomes, in the order Check applies them. OutcomeOK is the only
// non-finding value; every other outcome marks a spec the harness flags.
const (
	OutcomeOK               Outcome = "ok"
	OutcomePanic            Outcome = "panic"
	OutcomeError            Outcome = "error"
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

// Check compiles data under recover() and applies every oracle in order,
// returning the first failure or OutcomeOK. A panic anywhere in compilation or
// the oracles is captured as OutcomePanic rather than escaping to the caller. A
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

	doc, diags, err := compile(ctx, spec, data)
	if err != nil {
		return Result{Spec: spec, Outcome: OutcomeError, Detail: err.Error()}
	}
	if d, ok := ir.FirstError(diags); ok {
		return Result{Spec: spec, Outcome: OutcomeErrorDiag, Detail: d.Code + ": " + d.Message}
	}
	if vs := irverify.Verify(doc); len(vs) > 0 {
		return Result{Spec: spec, Outcome: OutcomeViolations, Detail: fmt.Sprintf("%+v", vs)}
	}
	if detail, ok := roundTrips(doc); !ok {
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

// compile runs the OpenAPI compiler on one in-memory source with default
// options. It is a package-level seam: production always uses the real compiler
// wired here, while tests replace it to drive the oracle-failure paths a correct
// compiler never produces — a panic, a document with structural violations, a
// non-round-tripping document, or a nondeterministic recompile.
var compile = func(ctx context.Context, spec string, data []byte) (*ir.Document, []ir.Diagnostic, error) {
	return openapi.New().Compile(ctx,
		[]compilers.Source{{Path: spec, Data: data}}, compilers.Options{})
}

// reserializeJSON re-encodes a value that has already survived a marshal or a
// compile: the decoded document in roundTrips and the recompiled document in
// deterministic. It is a package-level seam over json.Marshal that defaults to
// json.Marshal in production, where such a value can never fail to re-marshal;
// tests replace it to exercise that otherwise-unreachable defensive error path.
var reserializeJSON = json.Marshal

// roundTrips marshals doc, unmarshals into a fresh Document, re-marshals, and
// compares the two encodings byte for byte. Comparing serialized JSON — not
// the in-memory structs — is deliberate: it catches a null-vs-[] flip on the
// IR's deliberately non-omitempty Value collections that an in-memory
// EquateEmpty compare would miss, while still ignoring the harmless
// omitempty empty-vs-nil distinction.
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

// Report renders results sorted by spec name into a stable multi-line summary,
// one aligned line per spec. Column widths are measured from the results being
// rendered, so a spec path never runs into its outcome. It copies its input, so
// the caller's slice order is preserved.
func Report(results []Result) string {
	sorted := make([]Result, len(results))
	copy(sorted, results)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Spec < sorted[j].Spec })

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
