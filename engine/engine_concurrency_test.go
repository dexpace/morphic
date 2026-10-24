package engine_test

import (
	"context"
	"encoding/json/v2"
	"fmt"
	"path/filepath"
	"sync"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/engine"
	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/ir/irtest"
)

// concurrencyCorpus is the capability corpus the compiler's own conformance
// suite drives. Reusing it here means the concurrent run covers every construct
// the compiler can lower rather than one hand-picked spec, so shared state
// hiding in a single lowering still gets exercised.
const concurrencyCorpus = "../testdata/conformance/openapi"

// concurrentWorkers is the bounded fan-out: enough goroutines to overlap on any
// machine the suite runs on, few enough that a whole-corpus sweep per worker
// stays well inside the gate's per-package timeout even under -race.
const concurrentWorkers = 8

// TestEngine_ConcurrentRunSharesOneEngine drives the conformance corpus through
// one *engine.Engine from several goroutines at once and requires every
// document to be byte-identical to the one an unshared Engine produced, which
// also re-checks determinism (CLAUDE.md invariant 7). The Engine is built
// outside the goroutines; one per goroutine would share nothing.
//
// Two properties are pinned: no data race (run under -race) and identical
// documents. A cache keyed on the wrong thing can be perfectly synchronized and
// still hand one caller another's answer. The baseline comes from a fresh
// Engine per spec, since one drawn from the shared Engine would carry the same
// corruption.
func TestEngine_ConcurrentRunSharesOneEngine(t *testing.T) {
	t.Parallel()

	specs := corpusSpecs(t)
	ctx := t.Context()

	// The baseline: each spec compiled sequentially through an Engine of its own,
	// so nothing at all is shared and nothing can carry over between compiles.
	want := make([]string, len(specs))
	for i, spec := range specs {
		fresh, err := engine.New()
		require.NoError(t, err)
		doc, err := compileJSON(ctx, fresh, spec)
		require.NoError(t, err)
		require.NotEmpty(t, doc, "baseline compile of %s produced nothing", spec)
		want[i] = doc
	}

	// A comparison against a degenerate baseline would pass for the wrong reason,
	// so require the corpus to have produced a distinct document per spec.
	distinct := make(map[string]struct{}, len(want))
	for _, doc := range want {
		distinct[doc] = struct{}{}
	}
	require.Len(t, specs, len(distinct), "baseline documents must all differ")

	eng, err := engine.New()
	require.NoError(t, err)

	got := make([]workerRun, concurrentWorkers)
	var wg sync.WaitGroup
	for w := range concurrentWorkers {
		wg.Go(func() {
			got[w] = runCorpus(ctx, eng, specs, w)
		})
	}
	wg.Wait()

	for w, run := range got {
		require.NoError(t, run.err, "worker %d", w)
		// Bounds for the indexing below. It cannot fail while runCorpus returns
		// either an error or a full-length slice, and it is here so that a later
		// change to that shape is a failed assertion rather than a panic.
		require.Len(t, run.docs, len(specs), "worker %d returned %d docs", w, len(run.docs))
		for i, spec := range specs {
			// A worker that stopped early leaves zero values behind, which the
			// diff below would report as a document compiled differently. Say
			// what actually happened instead.
			if run.docs[i] == "" {
				t.Errorf("worker %d never compiled %s", w, filepath.Base(spec))
				continue
			}
			if diff := cmp.Diff(want[i], run.docs[i]); diff != "" {
				t.Errorf("worker %d compiled %s differently (-unshared +shared):\n%s",
					w, filepath.Base(spec), diff)
			}
		}
	}
}

// TestEngine_ConcurrentRunDocumentSharesOneDocument runs the passes over each
// corpus document from several goroutines at once, all handed the same decoded
// document, and requires every result to match a lone run's and the shared
// document to come out as it went in. Under -race it is what holds
// RunDocument's claim that it reads the document and never writes it.
func TestEngine_ConcurrentRunDocumentSharesOneDocument(t *testing.T) {
	t.Parallel()
	eng, err := engine.New()
	require.NoError(t, err)
	ctx := t.Context()
	specs := corpusSpecs(t)

	shared := make([]*ir.Document, len(specs))
	before := make([]string, len(specs))
	want := make([]string, len(specs))
	for i, spec := range specs {
		res, err := eng.Run(ctx, spec, engine.RunOptions{})
		require.NoError(t, err)
		require.NotNil(t, res.Document, "%s lowered no document", spec)
		shared[i] = roundTrip(t, res.Document)
		before[i] = irtest.Encoded(t, shared[i])
		alone, err := eng.RunDocument(ctx, roundTrip(t, res.Document), engine.RunOptions{})
		require.NoError(t, err)
		want[i] = irtest.Encoded(t, alone.Document)
	}

	got := make([]workerRun, concurrentWorkers)
	var wg sync.WaitGroup
	for w := range concurrentWorkers {
		wg.Go(func() {
			got[w] = runDocuments(ctx, eng, shared, w)
		})
	}
	wg.Wait()

	for w, run := range got {
		require.NoError(t, run.err, "worker %d", w)
		require.Len(t, run.docs, len(specs), "worker %d", w)
		for i, spec := range specs {
			assert.Empty(t, cmp.Diff(want[i], run.docs[i]), "worker %d ran %s differently", w, filepath.Base(spec))
		}
	}
	for i, spec := range specs {
		assert.Empty(t, cmp.Diff(before[i], irtest.Encoded(t, shared[i])), "the shared %s changed", filepath.Base(spec))
	}
}

// runDocuments runs the passes over every document through eng, starting at
// offset so that workers are on different documents at any instant.
func runDocuments(ctx context.Context, eng *engine.Engine, docs []*ir.Document, offset int) workerRun {
	out := make([]string, len(docs))
	for i := range docs {
		j := (offset + i) % len(docs)
		res, err := eng.RunDocument(ctx, docs[j], engine.RunOptions{})
		if err != nil {
			return workerRun{err: fmt.Errorf("document %d: %w", j, err)}
		}
		raw, err := json.Marshal(res.Document)
		if err != nil {
			return workerRun{err: fmt.Errorf("marshal document %d: %w", j, err)}
		}
		out[j] = string(raw)
	}
	return workerRun{docs: out}
}

// workerRun is one goroutine's sweep of the corpus: the marshalled document per
// spec, in the corpus order, or the first failure it hit.
type workerRun struct {
	docs []string
	err  error
}

// runCorpus compiles every spec through eng, starting at offset so that workers
// are on different specs at any instant and overlapping calls exercise
// different lowerings rather than all crowding one.
func runCorpus(ctx context.Context, eng *engine.Engine, specs []string, offset int) workerRun {
	docs := make([]string, len(specs))
	for i := range specs {
		j := (offset + i) % len(specs)
		doc, err := compileJSON(ctx, eng, specs[j])
		if err != nil {
			return workerRun{err: err}
		}
		docs[j] = doc
	}
	return workerRun{docs: docs}
}

// compileJSON runs one spec through eng and marshals the document. It returns
// errors rather than calling t.Fatal because it runs off the test goroutine,
// where require's FailNow is not legal.
func compileJSON(ctx context.Context, eng *engine.Engine, path string) (string, error) {
	res, err := eng.Run(ctx, path, engine.RunOptions{})
	if err != nil {
		return "", fmt.Errorf("run %s: %w", path, err)
	}
	if res.Document == nil {
		return "", fmt.Errorf("run %s: nil document", path)
	}
	raw, err := json.Marshal(res.Document)
	if err != nil {
		return "", fmt.Errorf("marshal %s: %w", path, err)
	}
	return string(raw), nil
}

func corpusSpecs(t *testing.T) []string {
	t.Helper()
	specs, err := filepath.Glob(filepath.Join(concurrencyCorpus, "*.yaml"))
	require.NoError(t, err)
	require.NotEmpty(t, specs, "no specs under %s", concurrencyCorpus)
	return specs
}
