package harness

import (
	"bytes"
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
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
// first run's document, or what the second run changed. The first is encoded
// before the second run, so a pass writing into the document it is handed
// cannot hide a change by making it to both.
func twice(ctx context.Context, p pass.Pass, doc *ir.Document) (*ir.Document, string, bool) {
	e, err := newEngine([]pass.Pass{p})
	if err != nil {
		return nil, err.Error(), false
	}
	once, err := e.RunDocument(ctx, doc, engine.RunOptions{})
	if err != nil {
		return nil, fmt.Sprintf("pass %q: %v", p.Name(), err), false
	}
	first, err := passOutput(once.Document)
	if err != nil {
		return nil, fmt.Sprintf("pass %q: encode its output: %v", p.Name(), err), false
	}
	again, err := e.RunDocument(ctx, once.Document, engine.RunOptions{})
	if err != nil {
		return nil, fmt.Sprintf("pass %q over its own output: %v", p.Name(), err), false
	}
	second, err := passOutput(again.Document)
	if err != nil {
		return nil, fmt.Sprintf("pass %q over its own output: encode: %v", p.Name(), err), false
	}
	if !bytes.Equal(first, second) {
		return nil, fmt.Sprintf("pass %q changes its own output (-once +twice):\n%s",
			p.Name(), cmp.Diff(string(first), string(second))), false
	}
	return once.Document, "", true
}

// passOutput encodes what a pass returned, indented so that a difference reads
// line by line, and without Document.Diagnostics: the engine writes those, and
// a rerun may report what the first run did not.
func passOutput(doc *ir.Document) ([]byte, error) {
	own := *doc
	own.Diagnostics = nil
	return json.Marshal(&own, jsontext.WithIndent("  "))
}
