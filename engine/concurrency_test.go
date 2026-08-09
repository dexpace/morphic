package engine_test

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sync"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/engine"
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

// TestEngine_ConcurrentRunSharesOneEngine drives the whole conformance corpus
// through a single *engine.Engine from several goroutines at once and requires
// every document to be byte-identical to the one an unshared Engine produced.
// One Engine, built outside the goroutines, is the whole point: a test that
// constructs an Engine per goroutine shares nothing and proves nothing.
//
// Two properties are pinned, and only the first is the detector's. A data race
// is not the only way concurrency corrupts output — a cache keyed on the wrong
// thing can be perfectly synchronized and still hand one caller another's
// answer — so the documents are compared, not merely produced, and the baseline
// is built with a *fresh* Engine per spec: a baseline drawn from the shared
// Engine would carry the same corruption and cancel it out. Matching it also
// re-checks determinism (CLAUDE.md invariant 7). Run under -race for the first
// property; the comparison holds either way.
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
	require.Equal(t, len(specs), len(distinct), "baseline documents must all differ")

	eng, err := engine.New()
	require.NoError(t, err)

	got := make([]workerRun, concurrentWorkers)
	var wg sync.WaitGroup
	for w := range concurrentWorkers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got[w] = runCorpus(ctx, eng, specs, w)
		}()
	}
	wg.Wait()

	for w, run := range got {
		require.NoError(t, run.err, "worker %d", w)
		require.Len(t, run.docs, len(specs), "worker %d did not finish the corpus", w)
		for i, spec := range specs {
			if diff := cmp.Diff(want[i], run.docs[i]); diff != "" {
				t.Errorf("worker %d compiled %s differently (-unshared +shared):\n%s",
					w, filepath.Base(spec), diff)
			}
		}
	}
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
