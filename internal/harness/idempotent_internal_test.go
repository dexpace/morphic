package harness

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/engine"
	"github.com/dexpace/morphic/internal/testspec"
	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/pass"
)

// lowering is an idempotent transform: it lowercases the document's name, so
// its first run changes the document and its second changes nothing.
type lowering struct{}

func (lowering) Name() string { return "lowering" }

func (lowering) Run(doc *ir.Document) (*ir.Document, []ir.Diagnostic) {
	own := *doc
	own.Name = strings.ToLower(own.Name)
	return &own, nil
}

// marking is a transform that is not idempotent: it appends a mark to the
// document's name on every run.
type marking struct{}

func (marking) Name() string { return "marking" }

func (marking) Run(doc *ir.Document) (*ir.Document, []ir.Diagnostic) {
	own := *doc
	own.Name += "!"
	return &own, nil
}

// stamping is a transform that is not idempotent and breaks its contract the
// same way on every run: it appends a mark to the service of the document it
// is handed, which every copy of that document shares, and returns that
// document. Its two outputs then hold one service.
type stamping struct{}

func (stamping) Name() string { return "stamping" }

func (stamping) Run(doc *ir.Document) (*ir.Document, []ir.Diagnostic) {
	doc.Services[0].Version += "!"
	return doc, nil
}

// rerunFault is a transform that behaves on its first run and, run again over
// its own output, does what fault says.
type rerunFault struct {
	fault func(own *ir.Document) *ir.Document
}

func (rerunFault) Name() string { return "rerun-fault" }

func (r rerunFault) Run(doc *ir.Document) (*ir.Document, []ir.Diagnostic) {
	own := *doc
	if own.Version == "seen" {
		return r.fault(&own), nil
	}
	own.Version = "seen"
	return &own, nil
}

// noting is an idempotent transform that reports only over its own output, as
// a filter whose pattern matched on the first run and matches nothing on the
// second would.
type noting struct{}

func (noting) Name() string { return "noting" }

func (noting) Run(doc *ir.Document) (*ir.Document, []ir.Diagnostic) {
	if doc.Version == "seen" {
		return doc, []ir.Diagnostic{ir.NewDiagnostic(ir.SeverityWarning, "stub/nothing-left", "matched nothing",
			ir.Provenance{Source: ir.NoSource})}
	}
	own := *doc
	own.Version = "seen"
	return &own, nil
}

// counting is a pass that is not a function of its input: it numbers the
// documents it returns, so two runs over one spec differ.
type counting struct{ n *atomic.Int32 }

func (counting) Name() string { return "counting" }

func (c counting) Run(doc *ir.Document) (*ir.Document, []ir.Diagnostic) {
	own := *doc
	own.Version = strconv.Itoa(int(c.n.Add(1)))
	return &own, nil
}

// unencodable returns doc's copy with an Unmodeled entry the encoder refuses.
func unencodable(own *ir.Document) *ir.Document {
	own.Unmodeled = ir.Unmodeled{"openapi:x": {Reason: ir.ReasonVendorExtension, Value: ir.RawValue("{invalid")}}
	return own
}

// TestCheck_IdempotenceOutcome pins what the oracle asks of a sweep's passes:
// an idempotent transform and the default passes are OK, a transform that
// changes its own output is non-idempotent, named, with what its second run
// changed, and so is one whose two outputs share what it writes into.
func TestCheck_IdempotenceOutcome(t *testing.T) {
	for _, tc := range []struct {
		name   string
		passes []pass.Pass
		want   Outcome
		detail []string
	}{
		{"the default passes", engine.Passes(), OutcomeOK, nil},
		{"an idempotent transform", []pass.Pass{lowering{}}, OutcomeOK, nil},
		{"an idempotent transform reporting only over its own output", []pass.Pass{noting{}}, OutcomeOK, nil},
		{"a transform changing its own output", []pass.Pass{lowering{}, marking{}}, OutcomeNonIdempotent,
			[]string{`pass "marking" changes its own output`, `"name": "tiny!"`, `"name": "tiny!!"`}},
		{"a transform writing into its input", []pass.Pass{stamping{}}, OutcomeNonIdempotent,
			[]string{`pass "stamping" changes its own output`, `"version": "!"`, `"version": "!!"`}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			withPasses(t, tc.passes...)

			r := Check(context.Background(), "spec.yaml", []byte(testspec.Tiny))

			assert.Equal(t, tc.want, r.Outcome, r.Detail)
			for _, want := range tc.detail {
				assert.Contains(t, r.Detail, want)
			}
		})
	}
}

// TestCheck_ANondeterministicPassIsNotReadAsNonIdempotent pins the oracle's
// place after determinism: a pass that numbers its outputs also changes its
// own output, and is reported as what it is first.
func TestCheck_ANondeterministicPassIsNotReadAsNonIdempotent(t *testing.T) {
	withPasses(t, counting{n: new(atomic.Int32)})

	r := Check(context.Background(), "spec.yaml", []byte(testspec.Tiny))

	assert.Equal(t, OutcomeNondeterministic, r.Outcome, r.Detail)
}

// TestIdempotent_Faults pins each way the oracle stops short of a comparison,
// naming the pass and the run that failed: a pass the engine refuses to
// build, one breaking its contract on its first run or over its own output,
// and an output that does not encode.
func TestIdempotent_Faults(t *testing.T) {
	noDocument := func(*ir.Document) *ir.Document { return nil }
	for _, tc := range []struct {
		name   string
		passes []pass.Pass
		want   string
	}{
		{"a nil pass", []pass.Pass{nil}, "harness: engine: pass 0 is nil"},
		{"no document on the first run", []pass.Pass{nilRun{}},
			`pass "nil-run": engine: run pass "nil-run": returned no document`},
		{"no document over its own output", []pass.Pass{rerunFault{fault: noDocument}},
			`pass "rerun-fault" over its own output: engine: run pass "rerun-fault": returned no document`},
		{"an output that does not encode", []pass.Pass{unencodablePass{}},
			`pass "unencodable": encode its output`},
		{"a rerun output that does not encode", []pass.Pass{rerunFault{fault: unencodable}},
			`pass "rerun-fault" over its own output: encode`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			withPasses(t, tc.passes...)

			detail, ok := idempotent(context.Background(), "spec.yaml", []byte(testspec.Tiny))

			assert.False(t, ok)
			assert.Contains(t, detail, tc.want)
		})
	}
}

// nilRun returns no document.
type nilRun struct{}

func (nilRun) Name() string { return "nil-run" }

func (nilRun) Run(*ir.Document) (*ir.Document, []ir.Diagnostic) { return nil, nil }

// unencodablePass returns a document the encoder refuses on its first run.
type unencodablePass struct{}

func (unencodablePass) Name() string { return "unencodable" }

func (unencodablePass) Run(doc *ir.Document) (*ir.Document, []ir.Diagnostic) {
	own := *doc
	return unencodable(&own), nil
}

// TestIdempotent_ACompileWithoutThePassesThatFails pins the oracle's own
// compile, which runs no pass: a fault or no document there is reported, not
// taken for a pass's.
func TestIdempotent_ACompileWithoutThePassesThatFails(t *testing.T) {
	for _, tc := range []struct {
		name string
		doc  *ir.Document
		err  error
		want string
	}{
		{"a fault", nil, errors.New("boom"), "compile without the passes: boom"},
		{"no document", nil, nil, "compile without the passes produced no document"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			orig := compile
			t.Cleanup(func() { compile = orig })
			compile = func(context.Context, string, []byte, []pass.Pass) (*ir.Document, []ir.Diagnostic, error) {
				return tc.doc, nil, tc.err
			}

			detail, ok := idempotent(context.Background(), "spec.yaml", nil)

			assert.False(t, ok)
			assert.Equal(t, tc.want, detail)
		})
	}
}

// TestIdempotent_PassesSeeWhatTheOnesBeforeThemReturned pins the oracle's
// threading: each pass is run over the document the one before it returned,
// as a run hands it, so a pass that is idempotent only over its
// predecessor's output is held to that.
func TestIdempotent_PassesSeeWhatTheOnesBeforeThemReturned(t *testing.T) {
	var seen []string
	withPasses(t, lowering{}, recording{seen: &seen})

	detail, ok := idempotent(context.Background(), "spec.yaml", []byte(testspec.Tiny))

	require.True(t, ok, detail)
	assert.Equal(t, []string{"tiny", "tiny"}, seen, "both runs see the lowered name")
}

// recording records the name of each document it is handed.
type recording struct{ seen *[]string }

func (recording) Name() string { return "recording" }

func (r recording) Run(doc *ir.Document) (*ir.Document, []ir.Diagnostic) {
	*r.seen = append(*r.seen, doc.Name)
	return doc, nil
}
