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
// returned, findings and all, which it must return again (pass.Pass).
//
// RunDocument holds the pass to the rest of its contract on both runs.
// Diagnostics are not compared, since the engine writes them and a rerun may
// report differently.
func idempotent(ctx context.Context, spec string, data []byte) (string, bool) {
	doc, _, err := compile(ctx, spec, data, nil)
	if err != nil {
		return "compile without the passes: " + err.Error(), false
	}
	if doc == nil {
		return "compile without the passes produced no document", false
	}
	for _, p := range passes() {
		once, detail, ok := twice(ctx, p, doc)
		if !ok {
			return detail, false
		}
		doc = once
	}
	return "", true
}

// twice runs p over doc, then over what that run returned, and returns the
// first run's document, or what the second run changed. A verified run refuses
// a pass that writes into the document it is handed, so the first run's
// document is as it was returned when the second has run.
func twice(ctx context.Context, p pass.Pass, doc *ir.Document) (*ir.Document, string, bool) {
	e, err := newEngine([]pass.Pass{p})
	if err != nil {
		return nil, err.Error(), false
	}
	once, first, err := runEncoded(ctx, e, doc)
	if err != nil {
		return nil, fmt.Sprintf("pass %q: %v", p.Name(), err), false
	}
	_, second, err := runEncoded(ctx, e, once)
	if err != nil {
		return nil, fmt.Sprintf("pass %q over its own output: %v", p.Name(), err), false
	}
	if !bytes.Equal(first, second) {
		return nil, fmt.Sprintf("pass %q changes its own output (-once +twice):\n%s",
			p.Name(), cmp.Diff(string(first), string(second))), false
	}
	return once, "", true
}

// runEncoded runs e's passes over doc and returns the document the run
// returned, beside its encoding: indented so that a difference reads line by
// line, and without Document.Diagnostics, which the engine writes and a rerun
// may report differently.
func runEncoded(ctx context.Context, e *engine.Engine, doc *ir.Document) (*ir.Document, []byte, error) {
	res, err := e.RunDocument(ctx, doc, runOptions())
	if err != nil {
		return nil, nil, reraise(err)
	}
	own := *res.Document
	own.Diagnostics = nil
	encoded, err := reserializeJSON(&own, jsontext.WithIndent("  "))
	if err != nil {
		return nil, nil, fmt.Errorf("encode its output: %w", err)
	}
	return res.Document, encoded, nil
}
