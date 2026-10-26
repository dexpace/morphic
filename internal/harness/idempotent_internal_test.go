package harness

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
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

// stamping is a transform that breaks its contract on every run: it appends a
// mark to the service of the document it is handed, which every copy of that
// document shares, and returns that document.
type stamping struct{}

func (stamping) Name() string { return "stamping" }

func (stamping) Run(doc *ir.Document) (*ir.Document, []ir.Diagnostic) {
	doc.Services[0].Version += "!"
	return doc, nil
}

// faulting is a transform that marks the document it returns, and does what
// first says on its first run, and rerun says over a document it marked. A
// nil fault behaves.
type faulting struct {
	first, rerun func(own *ir.Document) *ir.Document
}

func (faulting) Name() string { return "faulting" }

func (f faulting) Run(doc *ir.Document) (*ir.Document, []ir.Diagnostic) {
	own := *doc
	fault := f.first
	if own.Version == "seen" {
		fault = f.rerun
	}
	own.Version = "seen"
	if fault == nil {
		return &own, nil
	}
	return fault(&own), nil
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

// tallying is a transform whose document turns on the findings it is handed:
// it stamps how many there are, and reports one. Over the document a run of it
// returned, which carries that finding, it stamps another count.
type tallying struct{}

func (tallying) Name() string { return "tallying" }

func (tallying) Run(doc *ir.Document) (*ir.Document, []ir.Diagnostic) {
	own := *doc
	own.Version = strconv.Itoa(len(doc.Diagnostics))
	return &own, []ir.Diagnostic{ir.NewDiagnostic(ir.SeverityWarning, "stub/tallied", "tallied",
		ir.Provenance{Source: ir.NoSource})}
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
// the default passes, an idempotent transform and one reporting only over its
// own output are OK; a transform that changes its own output is
// non-idempotent, named, with what its second run changed, and so is one whose
// document turns on the findings a run of it wrote.
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
		{"a transform turning on the findings it is handed", []pass.Pass{tallying{}}, OutcomeNonIdempotent,
			[]string{`pass "tallying" changes its own output`, `"version": "0"`, `"version": "1"`}},
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

// TestCheck_APassWritingIntoItsInputIsAContractError pins that the sweep's
// verified run refuses a pass that writes into the document it is handed, on
// its first run, before any oracle compares what it returned.
func TestCheck_APassWritingIntoItsInputIsAContractError(t *testing.T) {
	withPasses(t, stamping{})

	r := Check(context.Background(), "spec.yaml", []byte(testspec.Tiny))

	assert.Equal(t, OutcomeError, r.Outcome, r.Detail)
	assert.Contains(t, r.Detail, `run pass "stamping": changed the document it was handed`)
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
// build, and one the verified run refuses on its first run or over its own
// output, for returning no document, one that does not encode, or writing
// into the one it is handed.
func TestIdempotent_Faults(t *testing.T) {
	noDocument := func(*ir.Document) *ir.Document { return nil }
	stampsItsInput := func(own *ir.Document) *ir.Document {
		own.Services[0].Version += "!" // the copy shares the input's services
		return own
	}
	for _, tc := range []struct {
		name   string
		passes []pass.Pass
		want   string
	}{
		{"a nil pass", []pass.Pass{nil}, "harness: engine: pass 0 is nil"},
		{"no document on the first run", []pass.Pass{faulting{first: noDocument}},
			`pass "faulting": engine: run pass "faulting": returned no document`},
		{"no document over its own output", []pass.Pass{faulting{rerun: noDocument}},
			`pass "faulting" over its own output: engine: run pass "faulting": returned no document`},
		{"an output that does not encode", []pass.Pass{faulting{first: unencodable}},
			`pass "faulting": engine: verify pass "faulting": the document breaks 2 invariants: ir/invalid-raw-value at "doc.Unmodeled[openapi:x]"; engine/unencodable at "doc"`},
		{"a rerun output that does not encode", []pass.Pass{faulting{rerun: unencodable}},
			`pass "faulting" over its own output: engine: verify pass "faulting": the document breaks 2 invariants: ir/invalid-raw-value at "doc.Unmodeled[openapi:x]"; engine/unencodable at "doc"`},
		{"a rerun writing into the document it is handed", []pass.Pass{faulting{rerun: stampsItsInput}},
			`pass "faulting" over its own output: engine: run pass "faulting": changed the document it was handed`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			withPasses(t, tc.passes...)

			detail, ok := idempotent(context.Background(), "spec.yaml", []byte(testspec.Tiny))

			assert.False(t, ok)
			assert.Contains(t, detail, tc.want)
		})
	}
}

// TestCheck_APassesPanicIsAPanicOutcome pins the first oracle for a pass,
// whose panic the engine recovers into an error: on its first run, and over
// its own output, a run only the idempotence oracle makes, each is still the
// panic outcome and not a finding of the oracle that ran it.
func TestCheck_APassesPanicIsAPanicOutcome(t *testing.T) {
	explode := func(*ir.Document) *ir.Document { panic("pass exploded") }
	for _, tc := range []struct {
		name string
		pass faulting
	}{
		{"on its first run", faulting{first: explode}},
		{"over its own output", faulting{rerun: explode}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			withPasses(t, tc.pass)

			r := Check(context.Background(), "spec.yaml", []byte(testspec.Tiny))

			assert.Equal(t, OutcomePanic, r.Outcome, r.Detail)
			assert.Contains(t, r.Detail, "pass exploded")
		})
	}
}

// TestIdempotent_AnEncodingThatFails pins the oracle's own encoding of a run's
// document, which a verified run has encoded already, so only the seam can
// fail it: the failure names the pass and the run.
func TestIdempotent_AnEncodingThatFails(t *testing.T) {
	orig := reserializeJSON
	t.Cleanup(func() { reserializeJSON = orig })
	reserializeJSON = func(any, ...json.Options) ([]byte, error) { return nil, errors.New("encode boom") }
	withPasses(t, lowering{})

	detail, ok := idempotent(context.Background(), "spec.yaml", []byte(testspec.Tiny))

	assert.False(t, ok)
	assert.Equal(t, `pass "lowering": encode its output: encode boom`, detail)
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
// threading: each pass is run over what the run of the one before it
// returned, findings and all, so a pass that is idempotent only over its
// predecessor's output is held to that.
func TestIdempotent_PassesSeeWhatTheOnesBeforeThemReturned(t *testing.T) {
	var seen []string
	withPasses(t, lowering{}, reporting{}, recording{seen: &seen})

	detail, ok := idempotent(context.Background(), "spec.yaml", []byte(testspec.Tiny))

	require.True(t, ok, detail)
	assert.Equal(t, []string{"tiny with 1 finding", "tiny with 1 finding"}, seen,
		"both runs see the name lowering wrote and the finding reporting's run wrote")
}

// recording records the name of each document it is handed, and how many
// findings that document carries.
type recording struct{ seen *[]string }

func (recording) Name() string { return "recording" }

func (r recording) Run(doc *ir.Document) (*ir.Document, []ir.Diagnostic) {
	*r.seen = append(*r.seen, fmt.Sprintf("%s with %d finding", doc.Name, len(doc.Diagnostics)))
	return doc, nil
}
