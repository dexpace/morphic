// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package engine_test

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"errors"
	"go/types"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/barrier"
	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi"
	"github.com/dexpace/morphic/engine"
	"github.com/dexpace/morphic/engine/cache"
	"github.com/dexpace/morphic/engine/reader"
	"github.com/dexpace/morphic/internal/decls"
	"github.com/dexpace/morphic/internal/testspec"
	"github.com/dexpace/morphic/ir"
)

// refSpec is a spec reaching one document beside it.
const refSpec = `openapi: 3.1.0
info: {title: t, version: "1"}
paths:
  /pets:
    get:
      responses:
        "200":
          description: ok
          content:
            application/json:
              schema: {$ref: "other.yaml#/components/schemas/Pet"}
`

// otherDoc returns the document refSpec reaches, its one schema's property
// named name.
func otherDoc(name string) string {
	return `openapi: 3.1.0
info: {title: o, version: "1"}
paths: {}
components:
  schemas:
    Pet: {type: object, properties: {` + name + `: {type: string}}}
`
}

// memoryCache is a Cache in memory, which counts its calls and fails as a test
// says.
type memoryCache struct {
	mu       sync.Mutex
	entries  map[cache.Key][]byte
	loads    int
	stores   int
	loadErr  error
	storeErr error
	onLoad   func()
}

func newMemoryCache() *memoryCache { return &memoryCache{entries: map[cache.Key][]byte{}} }

func (c *memoryCache) Load(_ context.Context, key cache.Key) ([]byte, bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.loads++
	if c.onLoad != nil {
		c.onLoad()
	}
	if c.loadErr != nil {
		return nil, false, c.loadErr
	}
	entry, ok := c.entries[key]
	return entry, ok, nil
}

func (c *memoryCache) Store(_ context.Context, key cache.Key, entry []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.stores++
	if c.storeErr != nil {
		return c.storeErr
	}
	c.entries[key] = slices.Clone(entry)
	return nil
}

// only returns the one entry c holds and its key.
func (c *memoryCache) only(t *testing.T) (cache.Key, []byte) {
	t.Helper()
	c.mu.Lock()
	defer c.mu.Unlock()
	require.Len(t, c.entries, 1)
	for key, entry := range c.entries {
		return key, entry
	}
	return cache.Key{}, nil
}

// countingCompiler is a compiler that counts what it was asked, so a test tells
// a hit from a miss by the work done rather than by anything the engine reports.
type countingCompiler struct {
	compilers.Compiler
	detects  atomic.Int32
	compiles atomic.Int32
}

func (c *countingCompiler) Detect(ctx context.Context, src compilers.Source,
	opts compilers.Options,
) (compilers.Recognition, []ir.Diagnostic, bool) {
	c.detects.Add(1)
	return c.Compiler.Detect(ctx, src, opts)
}

func (c *countingCompiler) Compile(ctx context.Context, sources []compilers.Source,
	opts compilers.Options,
) (*ir.Document, []ir.Diagnostic, error) {
	c.compiles.Add(1)
	return c.Compiler.Compile(ctx, sources, opts)
}

// cachedEngine returns an engine over store under build, and what counts its
// compiles.
func cachedEngine(t *testing.T, store engine.Cache, build string, opts ...engine.Option) (*engine.Engine, *countingCompiler) {
	t.Helper()
	counter := &countingCompiler{Compiler: openapi.New()}
	opts = append([]engine.Option{engine.WithCompilers(counter), engine.WithCache(store, build)}, opts...)
	eng, err := engine.New(opts...)
	require.NoError(t, err)
	return eng, counter
}

// sameRun compares two Results as a consumer reads them: every field but
// CacheErr, which is the one a hit may differ in, and the document by its
// encoding, which is the whole of what it says. Comparing the whole struct is
// what holds a field Result gains to a hit as well.
var sameRun = cmp.Options{
	cmpopts.IgnoreFields(engine.Result{}, "CacheErr"),
	cmpopts.EquateEmpty(),
	cmp.Transformer("encoded", func(doc *ir.Document) string {
		data, err := json.Marshal(doc)
		if err != nil {
			return "will not encode: " + err.Error()
		}
		return string(data)
	}),
}

// assertSameRun fails unless the two results tell a consumer the same thing.
func assertSameRun(t *testing.T, want, got *engine.Result) {
	t.Helper()
	if diff := cmp.Diff(want, got, sameRun); diff != "" {
		t.Errorf("a hit differs from the compile it stands for (-compiled +cached):\n%s", diff)
	}
}

// TestAssertSameRun_SeesEachField plants a difference in each field of a
// Result, so a comparison that skipped one would show here, not in a hit.
func TestAssertSameRun_SeesEachField(t *testing.T) {
	t.Parallel()
	base := engine.Result{
		Document:    &ir.Document{IRVersion: ir.IRVersion, Name: "a"},
		Diagnostics: []ir.Diagnostic{{Severity: ir.SeverityInfo, Code: "c"}},
		Sources:     []ir.SourceInfo{{Path: "p"}}, Format: compilers.SourceFormat{Name: "n", Version: "1"},
	}
	rows := map[string]func(*engine.Result){
		"Document":    func(r *engine.Result) { r.Document = &ir.Document{IRVersion: ir.IRVersion, Name: "b"} },
		"Diagnostics": func(r *engine.Result) { r.Diagnostics = nil },
		"Sources":     func(r *engine.Result) { r.Sources = []ir.SourceInfo{{Path: "q"}} },
		"Format":      func(r *engine.Result) { r.Format.Version = "2" },
		"Failed":      func(r *engine.Result) { r.Failed = true },
	}
	for field, change := range rows {
		changed := base
		change(&changed)
		assert.NotEmpty(t, cmp.Diff(&base, &changed, sameRun), "a difference in %s goes unseen", field)
	}
	withErr := base
	withErr.CacheErr = errors.New("cache failed")
	assert.Empty(t, cmp.Diff(&base, &withErr, sameRun), "CacheErr is the one field a hit may differ in")

	var declared []string
	for field := range reflect.TypeFor[engine.Result]().Fields() {
		declared = append(declared, field.Name)
	}
	assert.Empty(t, cmp.Diff(slices.Sorted(maps.Keys(rows)), slices.Sorted(slices.Values(slices.DeleteFunc(declared,
		func(name string) bool { return name == "CacheErr" })))), "a field of Result has no row (-rows +declared)")
}

// countingPass is a pass that counts its runs and changes nothing.
type countingPass struct {
	runs *atomic.Int32
}

func (countingPass) Name() string { return "counting" }

func (p countingPass) Run(_ context.Context, doc *ir.Document) (*ir.Document, []ir.Diagnostic) {
	p.runs.Add(1)
	return doc, nil
}

func TestWithCache_SecondRunSkipsEveryStage(t *testing.T) {
	t.Parallel()
	store := newMemoryCache()
	var passRuns atomic.Int32
	eng, counter := cachedEngine(t, store, "b1", engine.WithPasses(countingPass{runs: &passRuns}))
	spec := writeSpec(t, testspec.Tiny)

	first, err := eng.Run(t.Context(), spec, engine.RunOptions{})
	require.NoError(t, err)
	require.NotNil(t, first.Document)
	second, err := eng.Run(t.Context(), spec, engine.RunOptions{})
	require.NoError(t, err)

	assert.EqualValues(t, 1, counter.compiles.Load(), "the second run compiled")
	assert.EqualValues(t, 1, counter.detects.Load(), "a hit skips detection, which parses the spec")
	assert.EqualValues(t, 1, passRuns.Load(), "the second run ran the passes")
	assertSameRun(t, first, second)
	assert.NotSame(t, first.Document, second.Document, "each run owns its document")
	assert.NoError(t, second.CacheErr)
}

// TestWithCache_EachEntryPointReadsTheCache runs each way into the pipeline
// twice, since each composes its own reader.
func TestWithCache_EachEntryPointReadsTheCache(t *testing.T) {
	t.Parallel()
	src := compilers.Source{Path: "spec.yaml", Data: []byte(testspec.Tiny)}
	onDisk := writeSpec(t, testspec.Tiny)
	for name, run := range map[string]func(*engine.Engine, context.Context) (*engine.Result, error){
		"Run": func(e *engine.Engine, ctx context.Context) (*engine.Result, error) {
			return e.Run(ctx, onDisk, engine.RunOptions{})
		},
		"RunSpec": func(e *engine.Engine, ctx context.Context) (*engine.Result, error) {
			return e.RunSpec(ctx, src, engine.RunOptions{})
		},
		"RunSources": func(e *engine.Engine, ctx context.Context) (*engine.Result, error) {
			return e.RunSources(ctx, []compilers.Source{src}, engine.RunOptions{})
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			store := newMemoryCache()
			eng, counter := cachedEngine(t, store, "b1")
			for range 2 {
				_, err := run(eng, t.Context())
				require.NoError(t, err)
			}
			assert.Equal(t, 1, store.stores, "the compile is kept once")
			assert.EqualValues(t, 1, counter.compiles.Load(), "the second run compiled")
		})
	}
}

// TestWithCache_RunDocumentTakesNoPart is here because it compiles nothing: a
// cache that kept its output would keep passes over a caller's own document.
func TestWithCache_RunDocumentTakesNoPart(t *testing.T) {
	t.Parallel()
	store := newMemoryCache()
	eng, _ := cachedEngine(t, store, "b1")
	res, err := eng.Run(t.Context(), writeSpec(t, testspec.Tiny), engine.RunOptions{})
	require.NoError(t, err)
	stored := store.stores

	_, err = eng.RunDocument(t.Context(), res.Document, engine.RunOptions{})
	require.NoError(t, err)
	assert.Equal(t, stored, store.stores)
}

// TestWithCache_AChangedInputMisses changes one input at a time and requires a
// second compile, with an unchanged run beside it as the control: without it a
// cache that never hit would pass every row.
func TestWithCache_AChangedInputMisses(t *testing.T) {
	t.Parallel()
	base := engine.RunOptions{CompilerOptions: map[string]string{"grouping": "tags"}}
	tiny := writeSpec(t, testspec.Tiny)
	for _, tc := range []struct {
		name  string
		spec  string
		opts  engine.RunOptions
		built string
		hits  bool
	}{
		{name: "nothing changed", spec: tiny, opts: base, built: "b1", hits: true},
		{name: "another threshold, which only the verdict reads", spec: tiny, opts: engine.RunOptions{
			CompilerOptions: base.CompilerOptions, FailOn: engine.FailOnNone,
		}, built: "b1", hits: true},
		{name: "the spec's path", spec: writeNamed(t, "elsewhere.yaml", testspec.Tiny), opts: base, built: "b1"},
		{name: "a setting's value", spec: tiny, opts: engine.RunOptions{
			CompilerOptions: map[string]string{"grouping": "path-prefix"},
		}, built: "b1"},
		{name: "a setting removed", spec: tiny, opts: engine.RunOptions{}, built: "b1"},
		{name: "a pass turned off", spec: tiny, opts: engine.RunOptions{
			CompilerOptions: base.CompilerOptions, DisablePasses: []string{engine.ValidatePass},
		}, built: "b1"},
		{name: "the build", spec: tiny, opts: base, built: "b2"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			store := newMemoryCache()
			eng, counter := cachedEngine(t, store, "b1")
			_, err := eng.Run(t.Context(), tiny, base)
			require.NoError(t, err)

			later, laterCount := cachedEngine(t, store, tc.built)
			_, err = later.Run(t.Context(), tc.spec, tc.opts)
			require.NoError(t, err)

			want := 1
			if tc.hits {
				want = 0
			}
			assert.EqualValues(t, want, laterCount.compiles.Load())
			assert.EqualValues(t, 1, counter.compiles.Load())
		})
	}
}

// TestWithCache_TheSpecsBytesAreAnInput edits the file in place, so its bytes are
// all that changed: a spec elsewhere would miss on its path.
func TestWithCache_TheSpecsBytesAreAnInput(t *testing.T) {
	t.Parallel()
	store := newMemoryCache()
	eng, counter := cachedEngine(t, store, "b1")
	spec := writeSpec(t, testspec.Tiny)
	_, err := eng.Run(t.Context(), spec, engine.RunOptions{})
	require.NoError(t, err)

	edited := strings.ReplaceAll(testspec.Tiny, "title: Tiny", "title: Edited")
	require.NoError(t, os.WriteFile(spec, []byte(edited), 0o644))
	res, err := eng.Run(t.Context(), spec, engine.RunOptions{})
	require.NoError(t, err)
	assert.EqualValues(t, 2, counter.compiles.Load())
	assert.Equal(t, "Edited", res.Document.Name)
}

// TestWithCache_APassTurnedOffDoesNotSeeItsDiagnostics is the failure a key
// without the passes would have: a run with validate off served the findings
// of a run with it on.
func TestWithCache_APassTurnedOffDoesNotSeeItsDiagnostics(t *testing.T) {
	t.Parallel()
	store := newMemoryCache()
	eng, _ := cachedEngine(t, store, "b1", engine.WithCompilers(danglingCompiler{}))
	spec := writeSpec(t, testspec.Tiny)

	on, err := eng.Run(t.Context(), spec, engine.RunOptions{})
	require.NoError(t, err)
	require.NotEmpty(t, on.Diagnostics, "a fixture whose only findings are the pass's")
	off, err := eng.Run(t.Context(), spec, engine.RunOptions{DisablePasses: []string{engine.ValidatePass}})
	require.NoError(t, err)
	assert.Empty(t, off.Diagnostics)
}

// TestKeyFor_CoversEveryInput changes each input of the derivation, which is
// the only place one a run cannot vary, such as the IR version, can be seen.
func TestKeyFor_CoversEveryInput(t *testing.T) {
	t.Parallel()
	passes, settings := []string{"validate"}, map[string]string{"grouping": "tags"}
	base := engine.KeyFor("build", "1.0.0", "/dir", "spec.yaml", "hash", passes, settings)
	for name, key := range map[string]cache.Key{
		"build":      engine.KeyFor("other", "1.0.0", "/dir", "spec.yaml", "hash", passes, settings),
		"IR version": engine.KeyFor("build", "1.0.1", "/dir", "spec.yaml", "hash", passes, settings),
		"directory":  engine.KeyFor("build", "1.0.0", "/other", "spec.yaml", "hash", passes, settings),
		"path":       engine.KeyFor("build", "1.0.0", "/dir", "other.yaml", "hash", passes, settings),
		"spec":       engine.KeyFor("build", "1.0.0", "/dir", "spec.yaml", "other", passes, settings),
		"passes":     engine.KeyFor("build", "1.0.0", "/dir", "spec.yaml", "hash", nil, settings),
		"settings":   engine.KeyFor("build", "1.0.0", "/dir", "spec.yaml", "hash", passes, nil),
		"a value":    engine.KeyFor("build", "1.0.0", "/dir", "spec.yaml", "hash", passes, map[string]string{"grouping": "x"}),
		"a name":     engine.KeyFor("build", "1.0.0", "/dir", "spec.yaml", "hash", passes, map[string]string{"other": "tags"}),
	} {
		assert.NotEqual(t, base, key, "%s changed and the key did not", name)
	}
}

// TestKeyFor_ThePassesEndWhereTheirCountSays spells two runs alike but for it:
// with no count, the passes a, k and 0 and the pass a with the setting k=0 are
// one sequence of fields.
func TestKeyFor_ThePassesEndWhereTheirCountSays(t *testing.T) {
	t.Parallel()
	assert.NotEqual(t,
		engine.KeyFor("b", "v", "d", "p", "h", []string{"a", "k", "0"}, nil),
		engine.KeyFor("b", "v", "d", "p", "h", []string{"a"}, map[string]string{"k": "0"}))
	assert.NotEqual(t,
		engine.KeyFor("b", "v", "d", "p", "h", []string{"a", "b"}, nil),
		engine.KeyFor("b", "v", "d", "p", "h", []string{"b", "a"}, nil),
		"each pass reads what the one before it returned")
	assert.Equal(t,
		engine.KeyFor("b", "v", "d", "p", "h", nil, map[string]string{"a": "1", "b": "2"}),
		engine.KeyFor("b", "v", "d", "p", "h", nil, map[string]string{"b": "2", "a": "1"}),
		"a map has no order to depend on")
}

// TestWithCache_WorkingDirectoryIsAnInput is the case no hash of the inputs a
// compile is handed covers. One spec, by one relative path, reaching one
// absolute file reads the same bytes from two directories and lowers to two sets
// of type IDs, so a key without the directory serves one directory's IDs to the
// other. Not parallel: it changes directory.
func TestWithCache_WorkingDirectoryIsAnInput(t *testing.T) {
	root := t.TempDir()
	shared := filepath.Join(root, "shared", "other.yaml")
	require.NoError(t, os.MkdirAll(filepath.Dir(shared), 0o755))
	require.NoError(t, os.WriteFile(shared, []byte(otherDoc("name")), 0o644))
	spec := strings.ReplaceAll(refSpec, "other.yaml#", shared+"#")
	opts := engine.RunOptions{CompilerOptions: map[string]string{"allow-external-refs": "true"}}

	store := newMemoryCache()
	eng, _ := cachedEngine(t, store, "b1")
	plain, err := engine.New()
	require.NoError(t, err)

	for _, dir := range []string{filepath.Join(root, "near"), filepath.Join(root, "much", "further", "down")} {
		require.NoError(t, os.MkdirAll(filepath.Join(dir, "spec"), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "spec", "api.yaml"), []byte(spec), 0o644))
		t.Chdir(dir)

		want, err := plain.Run(t.Context(), "spec/api.yaml", opts)
		require.NoError(t, err)
		require.NotNil(t, want.Document)
		got, err := eng.Run(t.Context(), "spec/api.yaml", opts)
		require.NoError(t, err)
		assertSameRun(t, want, got)
	}
	assert.Len(t, store.entries, 2, "the two directories lower to two documents")
}

// splitSpec writes refSpec and the document it reaches, and returns the spec's
// path and the other's.
func splitSpec(t *testing.T) (spec, other string) {
	t.Helper()
	dir := t.TempDir()
	spec, other = filepath.Join(dir, "api.yaml"), filepath.Join(dir, "other.yaml")
	require.NoError(t, os.WriteFile(spec, []byte(refSpec), 0o644))
	require.NoError(t, os.WriteFile(other, []byte(otherDoc("name")), 0o644))
	return spec, other
}

// TestWithCache_ADocumentARunReadsIsAnInput changes only a document the spec
// reaches, whose bytes no key made before the compile can cover.
func TestWithCache_ADocumentARunReadsIsAnInput(t *testing.T) {
	t.Parallel()
	store := newMemoryCache()
	eng, counter := cachedEngine(t, store, "b1")
	plain, err := engine.New()
	require.NoError(t, err)
	spec, other := splitSpec(t)

	_, err = eng.Run(t.Context(), spec, engine.RunOptions{})
	require.NoError(t, err)
	_, err = eng.Run(t.Context(), spec, engine.RunOptions{})
	require.NoError(t, err)
	require.EqualValues(t, 1, counter.compiles.Load(), "an unchanged document hits")

	require.NoError(t, os.WriteFile(other, []byte(otherDoc("title")), 0o644))
	got, err := eng.Run(t.Context(), spec, engine.RunOptions{})
	require.NoError(t, err)
	require.EqualValues(t, 2, counter.compiles.Load(), "a changed document misses")
	want, err := plain.Run(t.Context(), spec, engine.RunOptions{})
	require.NoError(t, err)
	assertSameRun(t, want, got)

	again, err := eng.Run(t.Context(), spec, engine.RunOptions{})
	require.NoError(t, err)
	assert.EqualValues(t, 2, counter.compiles.Load(), "the new document was kept")
	assertSameRun(t, want, again)

	require.NoError(t, os.Remove(other))
	gone, err := eng.Run(t.Context(), spec, engine.RunOptions{})
	require.NoError(t, err)
	assert.EqualValues(t, 3, counter.compiles.Load(), "a document no longer there misses")
	assert.True(t, gone.Failed, "and the run reports what is missing, not the document it once read")
}

// TestWithCache_ADocumentADocumentReadsIsAnInput changes a document the spec
// never names, reached through the one it does.
func TestWithCache_ADocumentADocumentReadsIsAnInput(t *testing.T) {
	t.Parallel()
	store := newMemoryCache()
	eng, counter := cachedEngine(t, store, "b1")
	spec, other := splitSpec(t)
	third := filepath.Join(filepath.Dir(other), "third.yaml")
	reaching := strings.ReplaceAll(otherDoc("name"), "{type: string}", `{$ref: "third.yaml#/components/schemas/Name"}`)
	require.NoError(t, os.WriteFile(other, []byte(reaching), 0o644))
	named := func(typ string) []byte {
		return []byte(strings.ReplaceAll(otherDoc("x"),
			"Pet: {type: object, properties: {x: {type: string}}}", "Name: {type: "+typ+"}"))
	}
	require.NoError(t, os.WriteFile(third, named("string"), 0o644))

	first, err := eng.Run(t.Context(), spec, engine.RunOptions{})
	require.NoError(t, err)
	require.Len(t, first.Sources, 3, "a fixture whose third document is read")
	_, err = eng.Run(t.Context(), spec, engine.RunOptions{})
	require.NoError(t, err)
	require.EqualValues(t, 1, counter.compiles.Load())

	require.NoError(t, os.WriteFile(third, named("integer"), 0o644))
	_, err = eng.Run(t.Context(), spec, engine.RunOptions{})
	require.NoError(t, err)
	assert.EqualValues(t, 2, counter.compiles.Load())
}

// TestWithCache_ARemoteDocumentIsFetchedAgain holds a hit to its cost and to its
// reason: the server is asked each run, and new bytes there are a miss.
func TestWithCache_ARemoteDocumentIsFetchedAgain(t *testing.T) {
	t.Parallel()
	var fetches atomic.Int32
	var body atomic.Pointer[string]
	serve := func(doc string) { body.Store(&doc) }
	serve(otherDoc("name"))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fetches.Add(1)
		_, err := io.WriteString(w, *body.Load())
		assert.NoError(t, err)
	}))
	t.Cleanup(server.Close)

	spec := writeSpec(t, strings.ReplaceAll(refSpec, "other.yaml#", server.URL+"/other.yaml#"))
	opts := engine.RunOptions{CompilerOptions: map[string]string{"allow-external-refs": "true"}}
	store := newMemoryCache()
	eng, counter := cachedEngine(t, store, "b1")

	first, err := eng.Run(t.Context(), spec, opts)
	require.NoError(t, err)
	require.Len(t, first.Sources, 2, "a fixture that reaches the server")
	fetched := fetches.Load()
	require.Positive(t, fetched)

	_, err = eng.Run(t.Context(), spec, opts)
	require.NoError(t, err)
	assert.EqualValues(t, 1, counter.compiles.Load())
	assert.Greater(t, fetches.Load(), fetched, "a hit asks the server again")

	serve(otherDoc("title"))
	_, err = eng.Run(t.Context(), spec, opts)
	require.NoError(t, err)
	assert.EqualValues(t, 2, counter.compiles.Load())
}

// TestWithCache_EachRunRereadsThroughItsOwnReader shares a key between two runs
// that see different documents: Run reaches the one on disk, and RunSources,
// reading nothing but what it holds, does not. The entry must not answer it.
func TestWithCache_EachRunRereadsThroughItsOwnReader(t *testing.T) {
	t.Parallel()
	store := newMemoryCache()
	eng, counter := cachedEngine(t, store, "b1")
	spec, _ := splitSpec(t)
	_, err := eng.Run(t.Context(), spec, engine.RunOptions{})
	require.NoError(t, err)

	res, err := eng.RunSources(t.Context(), []compilers.Source{{Path: spec, Data: []byte(refSpec)}}, engine.RunOptions{})
	require.NoError(t, err)
	assert.EqualValues(t, 2, counter.compiles.Load())
	assert.True(t, res.Failed, "the document it does not hold is unresolved, whatever an earlier run read")
}

// TestWithCache_AnOverlayIsADocumentARunReads changes a file a setting names,
// the other way a compile reads beyond its spec.
func TestWithCache_AnOverlayIsADocumentARunReads(t *testing.T) {
	t.Parallel()
	store := newMemoryCache()
	eng, counter := cachedEngine(t, store, "b1")
	overlay := func(title string) string {
		return "overlay: 1.0.0\ninfo: {title: o, version: \"1\"}\nactions:\n" +
			"  - target: $.info\n    update: {title: " + title + "}\n"
	}
	spec := writeSpec(t, testspec.Tiny)
	file := writeNamed(t, "overlay.yaml", overlay("first"))
	opts := engine.RunOptions{CompilerOptions: map[string]string{"overlay": file}}

	first, err := eng.Run(t.Context(), spec, opts)
	require.NoError(t, err)
	require.NotNil(t, first.Document)
	_, err = eng.Run(t.Context(), spec, opts)
	require.NoError(t, err)
	require.EqualValues(t, 1, counter.compiles.Load())

	require.NoError(t, os.WriteFile(file, []byte(overlay("second")), 0o644))
	second, err := eng.Run(t.Context(), spec, opts)
	require.NoError(t, err)
	assert.EqualValues(t, 2, counter.compiles.Load())
	assert.Equal(t, "second", second.Document.Name)
}

// TestWithCache_AFailedReadLeavesNoEntry is what a list of reads cannot see: a
// document that was missing has no bytes to compare, so an entry for that run
// would go on answering once the document exists.
func TestWithCache_AFailedReadLeavesNoEntry(t *testing.T) {
	t.Parallel()
	store := newMemoryCache()
	eng, counter := cachedEngine(t, store, "b1")
	spec, other := splitSpec(t)
	require.NoError(t, os.Remove(other))

	first, err := eng.Run(t.Context(), spec, engine.RunOptions{FailOn: engine.FailOnNone})
	require.NoError(t, err)
	require.NotNil(t, first.Document, "a fixture that lowers, or the entry is refused for having no document")
	assert.Zero(t, store.stores, "the run could not read a document it names")

	require.NoError(t, os.WriteFile(other, []byte(otherDoc("name")), 0o644))
	second, err := eng.Run(t.Context(), spec, engine.RunOptions{})
	require.NoError(t, err)
	assert.EqualValues(t, 2, counter.compiles.Load())
	assert.False(t, second.Failed)
}

func TestRecorder_ListsEveryReadInOneOrder(t *testing.T) {
	t.Parallel()
	docs, err := reader.NewMemory([]compilers.Source{
		{Path: "b.yaml", Data: []byte("b")}, {Path: "a.yaml", Data: []byte("a")},
	})
	require.NoError(t, err)
	rec := engine.NewRecorder(docs)
	for _, req := range []compilers.DocumentRequest{
		{Location: "b.yaml"}, {Location: "a.yaml"}, {Location: "a.yaml", Limit: 9}, {Location: "a.yaml"},
	} {
		_, err := rec.ReadDocument(t.Context(), req)
		require.NoError(t, err)
	}
	locations, sound := engine.SettledLocations(rec)
	assert.True(t, sound)
	assert.Equal(t, []string{"a.yaml", "a.yaml", "b.yaml"}, locations, "one entry per request, in sorted order")
}

// fickleReader answers each read from the next of its answers.
type fickleReader struct {
	answers []string
	failing bool
}

func (r *fickleReader) ReadDocument(context.Context, compilers.DocumentRequest) ([]byte, error) {
	if r.failing {
		return nil, errors.New("unreadable")
	}
	answer := r.answers[0]
	r.answers = r.answers[1:]
	return []byte(answer), nil
}

func TestRecorder_ARunItCannotVouchForIsUnsound(t *testing.T) {
	t.Parallel()
	req := compilers.DocumentRequest{Location: "a.yaml"}

	t.Run("a repeat answering with the same bytes is sound", func(t *testing.T) {
		t.Parallel()
		rec := engine.NewRecorder(&fickleReader{answers: []string{"one", "one"}})
		_, _ = rec.ReadDocument(t.Context(), req)
		_, _ = rec.ReadDocument(t.Context(), req)
		_, sound := engine.SettledLocations(rec)
		assert.True(t, sound)
	})
	t.Run("a repeat answering with other bytes is not", func(t *testing.T) {
		t.Parallel()
		rec := engine.NewRecorder(&fickleReader{answers: []string{"one", "two"}})
		_, _ = rec.ReadDocument(t.Context(), req)
		_, _ = rec.ReadDocument(t.Context(), req)
		_, sound := engine.SettledLocations(rec)
		assert.False(t, sound)
	})
	t.Run("a third read does not restore it", func(t *testing.T) {
		t.Parallel()
		rec := engine.NewRecorder(&fickleReader{answers: []string{"one", "two", "two"}})
		for range 3 {
			_, _ = rec.ReadDocument(t.Context(), req)
		}
		_, sound := engine.SettledLocations(rec)
		assert.False(t, sound)
	})
	t.Run("a failed read is not, and the failure reaches the compile", func(t *testing.T) {
		t.Parallel()
		rec := engine.NewRecorder(&fickleReader{failing: true})
		_, err := rec.ReadDocument(t.Context(), req)
		require.Error(t, err)
		_, sound := engine.SettledLocations(rec)
		assert.False(t, sound)
	})
}

func TestWithCache_AVerifiedRunRunsItsStages(t *testing.T) {
	t.Parallel()
	store := newMemoryCache()
	eng, counter := cachedEngine(t, store, "b1")
	spec := writeSpec(t, testspec.Tiny)
	_, err := eng.Run(t.Context(), spec, engine.RunOptions{})
	require.NoError(t, err)
	loads, stores := store.loads, store.stores

	_, err = eng.Run(t.Context(), spec, engine.RunOptions{Verify: true})
	require.NoError(t, err)
	assert.EqualValues(t, 2, counter.compiles.Load(), "an entry would leave nothing for it to verify")
	assert.Equal(t, loads, store.loads)
	assert.Equal(t, stores, store.stores)
}

func TestWithCache_FormatOptionsTakeNoPart(t *testing.T) {
	t.Parallel()
	store := newMemoryCache()
	eng, counter := cachedEngine(t, store, "b1")
	spec := writeSpec(t, testspec.Tiny)
	opts := engine.RunOptions{FormatOptions: openapi.Options{Grouping: openapi.GroupByPathPrefix}}
	for range 2 {
		_, err := eng.Run(t.Context(), spec, opts)
		require.NoError(t, err)
	}
	assert.EqualValues(t, 2, counter.compiles.Load())
	assert.Zero(t, store.loads)
	assert.Zero(t, store.stores)
}

// warningCompiler lowers anything to an empty document holding one warning.
type warningCompiler struct {
	stubFront
}

func (warningCompiler) Compile(_ context.Context, sources []compilers.Source, _ compilers.Options) (*ir.Document, []ir.Diagnostic, error) {
	warning := ir.Diagnostic{Severity: ir.SeverityWarning, Code: "test/warning", Message: "w"}
	return &ir.Document{IRVersion: ir.IRVersion, Sources: sourcesOf("openapi@3.1", sources)}, []ir.Diagnostic{warning}, nil
}

// TestWithCache_AHitIsJudgedAtTheRunsOwnThreshold shares one entry between two
// thresholds, which the key leaves out for this reason.
func TestWithCache_AHitIsJudgedAtTheRunsOwnThreshold(t *testing.T) {
	t.Parallel()
	store := newMemoryCache()
	counter := &countingCompiler{Compiler: warningCompiler{}}
	eng, err := engine.New(engine.WithCompilers(counter), engine.WithCache(store, "b1"), engine.WithPasses())
	require.NoError(t, err)
	spec := writeSpec(t, testspec.Tiny)

	lenient, err := eng.Run(t.Context(), spec, engine.RunOptions{})
	require.NoError(t, err)
	assert.False(t, lenient.Failed)
	strict, err := eng.Run(t.Context(), spec, engine.RunOptions{FailOn: engine.FailOnWarning})
	require.NoError(t, err)
	assert.True(t, strict.Failed, "the warning reaches this run's threshold")
	assert.EqualValues(t, 1, counter.compiles.Load())
	assert.Equal(t, compilers.SourceFormat{Name: "openapi", Version: "3.1"}, strict.Format,
		"the format a hit did not detect")
}

// TestWithCache_NothingCompiledLeavesNoEntry covers each way a run ends with no
// document: an entry holds one, and a refusal costs little to repeat.
func TestWithCache_NothingCompiledLeavesNoEntry(t *testing.T) {
	t.Parallel()
	for name, spec := range map[string]string{
		"a source no compiler claims": "not a spec",
		"a compile the spec refuses":  "openapi: 3.1.0\ninfo: {title: t, version: '1'}\npaths: {}\nx-a: &a [*a]\n",
		"an empty source":             "",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			store := newMemoryCache()
			eng, _ := cachedEngine(t, store, "b1")
			res, err := eng.Run(t.Context(), writeSpec(t, spec), engine.RunOptions{})
			require.NoError(t, err)
			require.Nil(t, res.Document)
			assert.Zero(t, store.stores)
			assert.NoError(t, res.CacheErr)
		})
	}
}

// TestWithCache_ADamagedEntryCostsACompile damages an entry each way a store
// could return it: broken anywhere the seal covers, or sealed intact around a
// head or a document that does not decode. The run must come back whole, say
// what it found, and leave a sound entry in its place.
func TestWithCache_ADamagedEntryCostsACompile(t *testing.T) {
	t.Parallel()
	const head = `{"format":{"Name":"openapi","Version":"3.1"}}`
	flip := func(e []byte, at int) []byte {
		flipped := slices.Clone(e)
		flipped[at] ^= 1
		return flipped
	}
	for name, damage := range map[string]func(key cache.Key, entry []byte) []byte{
		"emptied":             func(cache.Key, []byte) []byte { return nil },
		"cut in its seal":     func(_ cache.Key, e []byte) []byte { return e[:5] },
		"cut in its document": func(_ cache.Key, e []byte) []byte { return e[:len(e)-1] },
		"its seal edited":     func(_ cache.Key, e []byte) []byte { return flip(e, 0) },
		"its head edited": func(_ cache.Key, e []byte) []byte {
			return bytes.Replace(e, []byte(`"Version":"3.1"`), []byte(`"Version":"3.0"`), 1)
		},
		"a document edited, still well formed": func(_ cache.Key, e []byte) []byte {
			return bytes.Replace(e, []byte(`"name":"Tiny"`), []byte(`"name":"Tiny, edited"`), 1)
		},
		"sealed around a head of no JSON": func(key cache.Key, _ []byte) []byte {
			return engine.SealedEntry(key, "garbage", `{}`)
		},
		"sealed around a document that decodes to nothing": func(key cache.Key, _ []byte) []byte {
			return engine.SealedEntry(key, head, `{}`)
		},
		"sealed around a document of another generation": func(key cache.Key, _ []byte) []byte {
			return engine.SealedEntry(key, head, `{"irVersion":"0.0.0"}`)
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			store := newMemoryCache()
			eng, counter := cachedEngine(t, store, "b1")
			spec := writeSpec(t, testspec.Tiny)
			want, err := eng.Run(t.Context(), spec, engine.RunOptions{})
			require.NoError(t, err)

			key, entry := store.only(t)
			damaged := damage(key, entry)
			require.NotEqual(t, entry, damaged, "a fixture that damages nothing")
			store.entries[key] = damaged

			got, err := eng.Run(t.Context(), spec, engine.RunOptions{})
			require.NoError(t, err)
			assert.EqualValues(t, 2, counter.compiles.Load())
			assertSameRun(t, want, got)
			require.ErrorContains(t, got.CacheErr, key.String(), "the failure names the entry")

			healed, err := eng.Run(t.Context(), spec, engine.RunOptions{})
			require.NoError(t, err)
			assert.EqualValues(t, 2, counter.compiles.Load(), "the entry was replaced")
			assert.NoError(t, healed.CacheErr)
		})
	}
}

// TestWithCache_AnEntryForAnotherKeyIsRefused hands one run's intact entry back
// for another's key, as a store mixing up two files would. A hash of the bytes
// alone passes it, and the run is served the other spec's document.
func TestWithCache_AnEntryForAnotherKeyIsRefused(t *testing.T) {
	t.Parallel()
	store := newMemoryCache()
	eng, counter := cachedEngine(t, store, "b1")
	alpha := writeNamed(t, "alpha.yaml", strings.ReplaceAll(testspec.Tiny, "title: Tiny", "title: Alpha"))
	beta := writeNamed(t, "beta.yaml", strings.ReplaceAll(testspec.Tiny, "title: Tiny", "title: Beta"))
	_, err := eng.Run(t.Context(), alpha, engine.RunOptions{})
	require.NoError(t, err)
	alphaKey, alphaEntry := store.only(t)
	_, err = eng.Run(t.Context(), beta, engine.RunOptions{})
	require.NoError(t, err)
	require.Len(t, store.entries, 2)
	for key := range store.entries {
		if key != alphaKey {
			store.entries[key] = alphaEntry
		}
	}

	res, err := eng.Run(t.Context(), beta, engine.RunOptions{})
	require.NoError(t, err)
	assert.Equal(t, "Beta", res.Document.Name)
	assert.EqualValues(t, 3, counter.compiles.Load())
	require.ErrorContains(t, res.CacheErr, "seal")
}

// TestWithCache_EveryRunOptionIsAnswered lists what a cached run does with each
// field of RunOptions. A field added there is either in the key or keeps its run
// out, and leaving it unanswered would serve one setting's document to another.
func TestWithCache_EveryRunOptionIsAnswered(t *testing.T) {
	t.Parallel()
	answered := map[string]string{
		"CompilerOptions": "in the key: TestWithCache_AChangedInputMisses",
		"DisablePasses":   "in the key, as the passes left on: TestWithCache_APassTurnedOffDoesNotSeeItsDiagnostics",
		"FailOn":          "judged afresh on a hit: TestWithCache_AHitIsJudgedAtTheRunsOwnThreshold",
		"FormatOptions":   "keeps its run out: TestWithCache_FormatOptionsTakeNoPart",
		"Verify":          "keeps its run out: TestWithCache_AVerifiedRunRunsItsStages",
	}
	p, err := decls.Load(reflect.TypeFor[engine.RunOptions]().PkgPath())
	require.NoError(t, err)
	named, err := p.Named("RunOptions")
	require.NoError(t, err)
	fields, ok := named.Underlying().(*types.Struct)
	require.True(t, ok, "RunOptions is a struct")
	declared := make([]string, 0, fields.NumFields())
	for field := range fields.Fields() {
		declared = append(declared, field.Name())
	}
	require.NotEmpty(t, declared, "a pin over no fields holds nothing")
	assert.Empty(t, cmp.Diff(slices.Sorted(maps.Keys(answered)), slices.Sorted(slices.Values(declared))),
		"say how a cached run treats each field (-answered +declared)")
}

func TestWithCache_AFailingStoreCostsOnlyTheWork(t *testing.T) {
	t.Parallel()
	loadErr, storeErr := errors.New("load failed"), errors.New("store failed")
	for name, tc := range map[string]struct{ load, store error }{
		"Load":         {load: loadErr},
		"Store":        {store: storeErr},
		"both of them": {load: loadErr, store: storeErr},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			store := newMemoryCache()
			store.loadErr, store.storeErr = tc.load, tc.store
			eng, counter := cachedEngine(t, store, "b1")
			spec := writeSpec(t, testspec.Tiny)

			res, err := eng.Run(t.Context(), spec, engine.RunOptions{})
			require.NoError(t, err, "a cache never fails a run")
			require.NotNil(t, res.Document)
			assert.False(t, res.Failed)
			assert.EqualValues(t, 1, counter.compiles.Load())
			for _, want := range []error{tc.load, tc.store} {
				if want != nil {
					require.ErrorIs(t, res.CacheErr, want)
				}
			}
			assert.Contains(t, res.CacheErr.Error(), "engine: cache: ", "a store's error says what the run was doing")
			assert.Equal(t, 1, store.stores, "a failed lookup still tries to keep the compile")
		})
	}
}

// panickyCache panics in the one step a test names, so a row reports that step's
// panic and no other's.
type panickyCache struct {
	onLoad bool
}

func (c panickyCache) Load(context.Context, cache.Key) ([]byte, bool, error) {
	if c.onLoad {
		panic("load")
	}
	return nil, false, nil
}

func (c panickyCache) Store(context.Context, cache.Key, []byte) error {
	if !c.onLoad {
		panic("store")
	}
	return nil
}

// TestWithCache_APanicIsACacheFailure holds a Cache as the engine holds any code
// it does not own, and the nil pointer in an interface among the ways to panic,
// which a check of the interface at New cannot see.
func TestWithCache_APanicIsACacheFailure(t *testing.T) {
	t.Parallel()
	var nilDir *cache.Dir
	for name, store := range map[string]engine.Cache{
		"in Load":          panickyCache{onLoad: true},
		"in Store":         panickyCache{},
		"a nil *cache.Dir": nilDir,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			eng, _ := cachedEngine(t, store, "b1")
			res, err := eng.Run(t.Context(), writeSpec(t, testspec.Tiny), engine.RunOptions{})
			require.NoError(t, err)
			require.NotNil(t, res.Document)
			var perr *barrier.PanicError
			require.ErrorAs(t, res.CacheErr, &perr)
		})
	}
}

// unencodableCompiler returns a document that will not encode, its name not
// being UTF-8.
type unencodableCompiler struct {
	stubFront
}

func (unencodableCompiler) Compile(_ context.Context, sources []compilers.Source, _ compilers.Options) (*ir.Document, []ir.Diagnostic, error) {
	return &ir.Document{IRVersion: ir.IRVersion, Name: "\xff", Sources: sourcesOf("openapi@3.1", sources)}, nil, nil
}

// readingCompiler reads one document and lowers to an empty one.
type readingCompiler struct {
	stubFront
	location string
}

func (c readingCompiler) Compile(ctx context.Context, sources []compilers.Source, opts compilers.Options) (*ir.Document, []ir.Diagnostic, error) {
	if _, err := opts.Reader.ReadDocument(ctx, compilers.DocumentRequest{Location: c.location}); err != nil {
		return nil, nil, err
	}
	return &ir.Document{IRVersion: ir.IRVersion, Sources: sourcesOf("openapi@3.1", sources)}, nil, nil
}

// TestWithCache_AnEntryThatWillNotEncodeIsNotKept covers both halves of an
// entry. A document that will not encode is the caller's to meet at output, and
// a name that is not UTF-8 is legal on a file system; neither may end the run.
// Only the read's name is not UTF-8 in the second row, so the document encodes
// and the head is what fails.
func TestWithCache_AnEntryThatWillNotEncodeIsNotKept(t *testing.T) {
	t.Parallel()
	spec := compilers.Source{Path: "spec.yaml", Data: []byte("x")}
	beyondUTF8 := compilers.Source{Path: "\xff.yaml", Data: []byte("x")}
	for name, tc := range map[string]struct {
		compiler compilers.Compiler
		want     string
	}{
		"its document": {unencodableCompiler{}, "encode document"},
		"its head":     {readingCompiler{location: beyondUTF8.Path}, "encode head"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			store := newMemoryCache()
			eng, err := engine.New(engine.WithCompilers(tc.compiler), engine.WithCache(store, "b1"), engine.WithPasses())
			require.NoError(t, err)
			res, err := eng.RunSources(t.Context(), []compilers.Source{spec, beyondUTF8}, engine.RunOptions{})
			require.NoError(t, err)
			require.NotNil(t, res.Document)
			require.ErrorContains(t, res.CacheErr, tc.want)
			assert.Zero(t, store.stores)
		})
	}
}

// TestWithCache_AWorkingDirectoryNoOneCanNameCostsTheWork fails to name it: with
// no way to derive a key, an entry could answer the wrong directory, so nothing
// is looked up or kept, even where an entry for another run exists.
func TestWithCache_AWorkingDirectoryNoOneCanNameCostsTheWork(t *testing.T) {
	t.Parallel()
	boom := errors.New("no working directory")
	store := newMemoryCache()
	eng, counter := cachedEngine(t, store, "b1")
	spec := writeSpec(t, testspec.Tiny)
	_, err := eng.Run(t.Context(), spec, engine.RunOptions{})
	require.NoError(t, err)
	loads, stores := store.loads, store.stores

	engine.FailWorkingDirectory(eng, boom)
	res, err := eng.Run(t.Context(), spec, engine.RunOptions{})
	require.NoError(t, err)
	require.NotNil(t, res.Document)
	require.ErrorIs(t, res.CacheErr, boom)
	assert.EqualValues(t, 2, counter.compiles.Load())
	assert.Equal(t, loads, store.loads)
	assert.Equal(t, stores, store.stores)
}

// TestWithCache_ACancelledRunReturnsNoDocument cancels inside the lookup of an
// entry that would answer: a stop returns no document on a hit as on a compile.
func TestWithCache_ACancelledRunReturnsNoDocument(t *testing.T) {
	t.Parallel()
	store := newMemoryCache()
	eng, _ := cachedEngine(t, store, "b1")
	spec := writeSpec(t, testspec.Tiny)
	_, err := eng.Run(t.Context(), spec, engine.RunOptions{})
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(t.Context())
	store.onLoad = cancel
	res, err := eng.Run(ctx, spec, engine.RunOptions{})
	require.ErrorIs(t, err, context.Canceled)
	assert.Nil(t, res)
}

// TestWithCache_ACancelledRecheckFallsToTheCompile cancels while the lookup is
// rereading what the compile read, which reads as a miss: the compile it falls
// to is what reports the stop.
func TestWithCache_ACancelledRecheckFallsToTheCompile(t *testing.T) {
	t.Parallel()
	store := newMemoryCache()
	eng, _ := cachedEngine(t, store, "b1")
	spec, _ := splitSpec(t)
	_, err := eng.Run(t.Context(), spec, engine.RunOptions{})
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(t.Context())
	store.onLoad = cancel
	res, err := eng.Run(ctx, spec, engine.RunOptions{})
	require.ErrorIs(t, err, context.Canceled)
	assert.Nil(t, res)
}

func TestNew_RefusesACacheNoRunCouldUse(t *testing.T) {
	t.Parallel()
	_, err := engine.New(engine.WithCache(nil, "b1"))
	require.ErrorContains(t, err, "cache is nil")
	_, err = engine.New(engine.WithCache(newMemoryCache(), ""))
	require.ErrorContains(t, err, "no build")
}

// TestWithCache_ADirectoryServesARun goes through the store a binary uses, which
// is what a run reaches that memoryCache does not: bytes that crossed a file.
func TestWithCache_ADirectoryServesARun(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(t.TempDir(), "cache")
	spec, _ := splitSpec(t)

	first, counter := cachedEngine(t, cache.NewDir(dir), "b1")
	want, err := first.Run(t.Context(), spec, engine.RunOptions{})
	require.NoError(t, err)
	require.NoError(t, want.CacheErr)

	second, later := cachedEngine(t, cache.NewDir(dir), "b1")
	got, err := second.Run(t.Context(), spec, engine.RunOptions{})
	require.NoError(t, err)
	assert.EqualValues(t, 1, counter.compiles.Load())
	assert.Zero(t, later.compiles.Load(), "another engine, as another process would be, finds the entry")
	assertSameRun(t, want, got)
}

// TestWithCache_OverlappingRunsAgree is for -race, which the coverage gate uses:
// an entry is shared by runs starting and storing at once.
func TestWithCache_OverlappingRunsAgree(t *testing.T) {
	t.Parallel()
	store := newMemoryCache()
	eng, _ := cachedEngine(t, store, "b1")
	spec, _ := splitSpec(t)
	want, err := eng.Run(t.Context(), spec, engine.RunOptions{})
	require.NoError(t, err)

	const runs = 8
	results := make([]*engine.Result, runs)
	var wg sync.WaitGroup
	for i := range runs {
		wg.Go(func() {
			res, err := eng.Run(t.Context(), spec, engine.RunOptions{})
			assert.NoError(t, err)
			results[i] = res
		})
	}
	wg.Wait()
	for _, res := range results {
		assertSameRun(t, want, res)
	}
}

// TestWithCache_AHitIsTheCompileOverTheCorpus holds what the cache promises to
// every spec under testdata: the second run tells a consumer exactly what the
// first did. It is the differential a hand-picked spec cannot be, since an IR
// field the encoding lost would show only where a spec sets it.
func TestWithCache_AHitIsTheCompileOverTheCorpus(t *testing.T) {
	t.Parallel()
	var hits atomic.Int32
	t.Run("specs", func(t *testing.T) {
		for _, path := range corpusSpecFiles(t, "../testdata") {
			t.Run(filepath.Base(path), func(t *testing.T) {
				t.Parallel()
				store := newMemoryCache()
				eng, counter := cachedEngine(t, store, "b1")
				opts := engine.RunOptions{FailOn: engine.FailOnNone}
				first, err := eng.Run(t.Context(), path, opts)
				require.NoError(t, err)
				require.NoError(t, first.CacheErr)
				second, err := eng.Run(t.Context(), path, opts)
				require.NoError(t, err)
				assertSameRun(t, first, second)
				if len(store.entries) > 0 {
					assert.EqualValues(t, 1, counter.compiles.Load(), "a kept document answers the second run")
					hits.Add(1)
				}
			})
		}
	})
	assert.Positive(t, hits.Load(), "a sweep with no hit compared nothing")
}
