package harness

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"

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
// the oracles is captured as OutcomePanic rather than escaping to the caller.
func Check(ctx context.Context, spec string, data []byte) (res Result) {
	res = Result{Spec: spec, Outcome: OutcomeOK}
	defer func() {
		if r := recover(); r != nil {
			res = Result{Spec: spec, Outcome: OutcomePanic, Detail: fmt.Sprint(r)}
		}
	}()

	doc, diags, err := compile(ctx, spec, data)
	if err != nil {
		return Result{Spec: spec, Outcome: OutcomeError, Detail: err.Error()}
	}
	if d, ok := firstErrorDiag(diags); ok {
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
	return res
}

// compile runs the OpenAPI compiler on one in-memory source with default
// options.
func compile(ctx context.Context, spec string, data []byte) (*ir.Document, []ir.Diagnostic, error) {
	return openapi.New().Compile(ctx,
		[]compilers.Source{{Path: spec, Data: data}}, compilers.Options{})
}

// firstErrorDiag returns the first error-severity diagnostic, if any.
func firstErrorDiag(diags []ir.Diagnostic) (ir.Diagnostic, bool) {
	for _, d := range diags {
		if d.Severity == ir.SeverityError {
			return d, true
		}
	}
	return ir.Diagnostic{}, false
}

// roundTrips marshals doc, unmarshals it back into a fresh Document, and
// compares. It reports the cmp diff on mismatch. cmpopts.EquateEmpty treats a
// nil and an empty collection as equal: the IR's omitempty JSON tags collapse
// empty non-nil registries and slices to absent, so they return as nil — a
// normalization, not the data loss this oracle exists to catch.
func roundTrips(doc *ir.Document) (string, bool) {
	data, err := json.Marshal(doc)
	if err != nil {
		return "marshal: " + err.Error(), false
	}
	var back ir.Document
	if err := json.Unmarshal(data, &back); err != nil {
		return "unmarshal: " + err.Error(), false
	}
	if diff := cmp.Diff(doc, &back, cmpopts.EquateEmpty()); diff != "" {
		return diff, false
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
	second, err := json.Marshal(recompiled)
	if err != nil {
		return "marshal second: " + err.Error(), false
	}
	if !bytes.Equal(first, second) {
		return "IR JSON differs across two compiles", false
	}
	return "", true
}

// Report renders results sorted by spec name into a stable multi-line summary,
// one aligned line per spec. It copies its input, so the caller's slice order is
// preserved.
func Report(results []Result) string {
	sorted := make([]Result, len(results))
	copy(sorted, results)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Spec < sorted[j].Spec })

	var b strings.Builder
	for _, r := range sorted {
		fmt.Fprintf(&b, "%-40s %-20s %s\n", r.Spec, r.Outcome, r.Detail)
	}
	return b.String()
}
