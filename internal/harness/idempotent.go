package harness

import (
	"bytes"
	"context"
	"encoding/json/jsontext"
	"fmt"

	"github.com/google/go-cmp/cmp"

	"github.com/dexpace/morphic/engine"
	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/pass"
)

// idempotent compiles data with no pass, then runs the sweep's passes in order
// through the engine's RunDocument, each over what the run of the one before
// it returned, and each twice: the second time over what its first run
// returned, findings and all, which it must return again (pass.Pass). Then it
// holds result, what the sweep's run of every pass returned, to being settled.
// No run that turns passes off is swept on its own.
//
// RunDocument holds the pass to the rest of its contract on every run.
// Diagnostics are not compared (encodeOwn).
func idempotent(ctx context.Context, spec string, data []byte, result *ir.Document) (string, bool) {
	alone, err := eachAlone(passes())
	if err != nil {
		return err.Error(), false
	}
	doc, _, err := compile(ctx, spec, data, nil)
	if err != nil {
		return "compile without the passes: " + err.Error(), false
	}
	if doc == nil {
		return "compile without the passes produced no document", false
	}
	for _, p := range alone {
		once, detail, ok := p.twice(ctx, doc)
		if !ok {
			return detail, false
		}
		doc = once
	}
	return settled(ctx, alone, result)
}

// solo is one of the sweep's passes, by the name it answered to, and an engine
// running it alone.
type solo struct {
	name   string
	engine *engine.Engine
}

// eachAlone returns, for each of ps in order, an engine running it alone.
func eachAlone(ps []pass.Pass) ([]solo, error) {
	alone := make([]solo, 0, len(ps))
	for _, p := range ps {
		e, err := newEngine([]pass.Pass{p})
		if err != nil {
			return nil, err
		}
		alone = append(alone, solo{name: p.Name(), engine: e})
	}
	return alone, nil
}

// twice runs p over doc, then over what that run returned, and returns the
// first run's document, or what the second run changed. A verified run refuses
// a pass that writes into the document it is handed, so the first run's
// document is as it was returned when the second has run.
func (p solo) twice(ctx context.Context, doc *ir.Document) (*ir.Document, string, bool) {
	once, first, err := runEncoded(ctx, p.engine, doc)
	if err != nil {
		return nil, fmt.Sprintf("pass %q: %v", p.name, err), false
	}
	_, second, err := runEncoded(ctx, p.engine, once)
	if err != nil {
		return nil, fmt.Sprintf("pass %q over its own output: %v", p.name, err), false
	}
	if !bytes.Equal(first, second) {
		return nil, fmt.Sprintf("pass %q changes its own output (-once +twice):\n%s",
			p.name, cmp.Diff(string(first), string(second))), false
	}
	return once, "", true
}

// settled runs each pass of alone over result, what a run of them all
// returned, and reports the first that changes it. Each pass being idempotent
// does not settle the list: one acting on what a later one returns changes the
// result of a second run of it.
func settled(ctx context.Context, alone []solo, result *ir.Document) (string, bool) {
	want, err := encodeOwn(result)
	if err != nil {
		return "encode the run's result: " + err.Error(), false
	}
	for _, p := range alone {
		_, got, err := runEncoded(ctx, p.engine, result)
		if err != nil {
			return fmt.Sprintf("pass %q over the run's result: %v", p.name, err), false
		}
		if !bytes.Equal(want, got) {
			return fmt.Sprintf("pass %q changes the run's result (-run +rerun):\n%s",
				p.name, cmp.Diff(string(want), string(got))), false
		}
	}
	return "", true
}

// runEncoded runs e's passes over doc and returns the document the run
// returned, beside its encoding (encodeOwn).
func runEncoded(ctx context.Context, e *engine.Engine, doc *ir.Document) (*ir.Document, []byte, error) {
	res, err := e.RunDocument(ctx, doc, runOptions())
	if err != nil {
		return nil, nil, reraise(err)
	}
	encoded, err := encodeOwn(res.Document)
	if err != nil {
		return nil, nil, fmt.Errorf("encode its output: %w", err)
	}
	return res.Document, encoded, nil
}

// encodeOwn encodes what doc says of its own: indented, so that a difference
// reads line by line, and without Document.Diagnostics, which the engine
// writes and a rerun may report differently.
func encodeOwn(doc *ir.Document) ([]byte, error) {
	own := *doc
	own.Diagnostics = nil
	return reserializeJSON(&own, jsontext.WithIndent("  "))
}
