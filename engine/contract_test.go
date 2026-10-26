package engine_test

import (
	"context"
	"errors"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/barrier"
	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/engine"
	"github.com/dexpace/morphic/internal/testspec"
	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/pass"
)

// faulty is a compiler each of whose answers a test can replace, so a test
// breaks one duty at a time. A method with no hook answers as a correct
// compiler would: it claims openapi 3.1, takes no options, and lowers the
// source to a document that keeps every duty.
type faulty struct {
	stubFront
	detect  func() (compilers.Recognition, []ir.Diagnostic, bool)
	decode  func() (any, error)
	table   func(sources []compilers.Source) []ir.SourceInfo
	compile func(sources []compilers.Source) (*ir.Document, []ir.Diagnostic, error)
	formats func() []compilers.SourceFormat
	limit   func() int
}

func (c faulty) Formats() []compilers.SourceFormat {
	if c.formats != nil {
		return c.formats()
	}
	return c.stubFront.Formats()
}

func (c faulty) Detect(ctx context.Context, src compilers.Source, opts compilers.Options) (compilers.Recognition, []ir.Diagnostic, bool) {
	if c.detect != nil {
		return c.detect()
	}
	return c.stubFront.Detect(ctx, src, opts)
}

func (c faulty) DecodeOptions(set compilers.OptionSet) (any, error) {
	if c.decode != nil {
		return c.decode()
	}
	return c.stubFront.DecodeOptions(set)
}

func (c faulty) SourceLimit(opts compilers.Options) int {
	if c.limit != nil {
		return c.limit()
	}
	return c.stubFront.SourceLimit(opts)
}

func (c faulty) SourceTable(sources []compilers.Source, opts compilers.Options) []ir.SourceInfo {
	if c.table != nil {
		return c.table(sources)
	}
	return c.stubFront.SourceTable(sources, opts)
}

func (c faulty) Compile(_ context.Context, sources []compilers.Source, _ compilers.Options) (*ir.Document, []ir.Diagnostic, error) {
	if c.compile != nil {
		return c.compile(sources)
	}
	return keptDocument(sources), nil, nil
}

// keptDocument is a document that keeps every duty the engine holds a compile
// to, built afresh for sources, with one finding of its own stored.
func keptDocument(sources []compilers.Source) *ir.Document {
	return &ir.Document{
		IRVersion:   ir.IRVersion,
		Name:        "Kept",
		Types:       ir.TypeRegistry{},
		Sources:     sourcesOf("openapi@3.1", sources),
		Diagnostics: []ir.Diagnostic{ir.NewDiagnostic(ir.SeverityInfo, "kept/note", "m", ir.Provenance{Source: ir.NoSource})},
	}
}

// funcPass is a pass defined by a function.
type funcPass struct {
	name string
	run  func(doc *ir.Document) (*ir.Document, []ir.Diagnostic)
}

func (p funcPass) Name() string { return p.name }

func (p funcPass) Run(doc *ir.Document) (*ir.Document, []ir.Diagnostic) { return p.run(doc) }

// entry is one way into the engine, run over the tiny spec with a compiler, the
// passes and the options a test gives it.
type entry struct {
	name string
	run  func(t *testing.T, eng *engine.Engine, opts engine.RunOptions) (*engine.Result, error)
}

// entries are the three ways a run can start. RunDocument compiles nothing, so
// a case about a compiler runs through the first two.
var (
	runFromAFile = entry{"Run", func(t *testing.T, eng *engine.Engine, opts engine.RunOptions) (*engine.Result, error) {
		t.Helper()
		return eng.Run(t.Context(), writeSpec(t, testspec.Tiny), opts)
	}}
	runFromMemory = entry{"RunSources", func(t *testing.T, eng *engine.Engine, opts engine.RunOptions) (*engine.Result, error) {
		t.Helper()
		return eng.RunSources(t.Context(), []compilers.Source{{Path: "spec.yaml", Data: []byte(testspec.Tiny)}}, opts)
	}}
	runOverADocument = entry{"RunDocument", func(t *testing.T, eng *engine.Engine, opts engine.RunOptions) (*engine.Result, error) {
		t.Helper()
		doc := keptDocument([]compilers.Source{{Path: "spec.yaml", Data: []byte(testspec.Tiny)}})
		return eng.RunDocument(t.Context(), doc, opts)
	}}
	compileEntries = []entry{runFromAFile, runFromMemory}
	passEntries    = []entry{runFromAFile, runFromMemory, runOverADocument}
)

// engineOf builds an engine over comp and passes, failing the test if it cannot.
func engineOf(t *testing.T, comp compilers.Compiler, passes ...pass.Pass) *engine.Engine {
	t.Helper()
	eng, err := engine.New(engine.WithCompilers(comp), engine.WithPasses(passes...))
	require.NoError(t, err)
	return eng
}

// requireBreach requires err to be a stage's contract breach that records a
// panic of value, and to read as one line naming where it happened.
func requireBreach(t *testing.T, err, sentinel error, value any, names ...string) {
	t.Helper()
	require.ErrorIs(t, err, sentinel)
	perr, ok := errors.AsType[*barrier.PanicError](err)
	require.True(t, ok, "the breach should carry the panic: %v", err)
	assert.Equal(t, value, perr.Value())
	assert.NotContains(t, err.Error(), "\n", "an error is one line")
	for _, name := range names {
		assert.Contains(t, err.Error(), name)
	}
}

// TestEngine_RunReportsAPanicInACompilerAsAContractError holds each call the
// engine makes of a compiler while a run is under way to the contract Run
// states: a compiler breaking its own is a Go error, never a panic through the
// caller, with no result beside it.
func TestEngine_RunReportsAPanicInACompilerAsAContractError(t *testing.T) {
	t.Parallel()
	boom := func() { panic("boom") }
	tests := []struct {
		name    string
		comp    faulty
		opts    engine.RunOptions
		message string
	}{
		{"Compile", faulty{compile: func([]compilers.Source) (*ir.Document, []ir.Diagnostic, error) { boom(); return nil, nil, nil }},
			engine.RunOptions{}, "engine: compile"},
		{"Detect", faulty{detect: func() (compilers.Recognition, []ir.Diagnostic, bool) {
			boom()
			return compilers.Recognition{}, nil, false
		}},
			engine.RunOptions{}, "engine: detect"},
		{"SourceTable", faulty{table: func([]compilers.Source) []ir.SourceInfo { boom(); return nil }},
			engine.RunOptions{}, "SourceTable: panic: boom"},
		{"DecodeOptions", faulty{decode: func() (any, error) { boom(); return nil, nil }},
			engine.RunOptions{CompilerOptions: map[string]string{"setting": "value"}}, "configure: panic: boom"},
		{"SourceLimit", faulty{limit: func() int { boom(); return 0 }},
			engine.RunOptions{}, "SourceLimit: panic: boom"},
	}
	for _, tt := range tests {
		for _, via := range compileEntries {
			t.Run(tt.name+"/"+via.name, func(t *testing.T) {
				t.Parallel()
				res, err := via.run(t, engineOf(t, tt.comp), tt.opts)

				requireBreach(t, err, compilers.ErrContract, "boom", tt.message)
				assert.Nil(t, res)
			})
		}
	}
}

func TestNew_RefusesACompilerWhoseFormatsPanic(t *testing.T) {
	t.Parallel()

	eng, err := engine.New(engine.WithCompilers(faulty{formats: func() []compilers.SourceFormat { panic("boom") }}))

	requireBreach(t, err, compilers.ErrContract, "boom", "engine: register compiler 0", "Formats")
	assert.Nil(t, eng)
}

// TestEngine_RunStaysUsableAfterACompilerPanics pins that a breach is one run's:
// the engine holds nothing the failed run left behind, so the next run over the
// same engine is a run like any other.
func TestEngine_RunStaysUsableAfterACompilerPanics(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	comp := faulty{compile: func(sources []compilers.Source) (*ir.Document, []ir.Diagnostic, error) {
		if calls.Add(1) == 1 {
			panic("first run only")
		}
		return keptDocument(sources), nil, nil
	}}
	for _, via := range compileEntries {
		t.Run(via.name, func(t *testing.T) {
			calls.Store(0)
			eng := engineOf(t, comp)

			_, err := via.run(t, eng, engine.RunOptions{})
			require.ErrorIs(t, err, compilers.ErrContract)

			res, err := via.run(t, eng, engine.RunOptions{})
			require.NoError(t, err)
			require.NotNil(t, res.Document)
		})
	}
}

// TestEngine_RunReportsAPanicInAPassAsAContractError holds the passes to the
// same contract, however a run starts, and stops the run at the pass that broke
// it.
func TestEngine_RunReportsAPanicInAPassAsAContractError(t *testing.T) {
	t.Parallel()
	for _, via := range passEntries {
		t.Run(via.name, func(t *testing.T) {
			t.Parallel()
			boomer := funcPass{"boomer", func(*ir.Document) (*ir.Document, []ir.Diagnostic) { panic("boom") }}
			eng := engineOf(t, faulty{}, notePass("before"), boomer, unreachedPass{t: t, name: "after"})

			res, err := via.run(t, eng, engine.RunOptions{})

			requireBreach(t, err, pass.ErrContract, "boom", `engine: run pass "boomer": Run: panic: boom`)
			assert.Nil(t, res)
		})
	}
}

func TestNew_RefusesAPassWhoseNamePanics(t *testing.T) {
	t.Parallel()
	namer := panickingName{}

	eng, err := engine.New(engine.WithCompilers(faulty{}), engine.WithPasses(notePass("fine"), namer))

	requireBreach(t, err, pass.ErrContract, "name exploded", "engine: pass 1: Name: panic")
	assert.Nil(t, eng)
}

// panickingName is a pass whose Name panics.
type panickingName struct{}

func (panickingName) Name() string { panic("name exploded") }

func (panickingName) Run(doc *ir.Document) (*ir.Document, []ir.Diagnostic) { return doc, nil }

// countedName is a pass that counts how often it is asked for its name.
type countedName struct{ asked *atomic.Int32 }

func (p countedName) Name() string {
	p.asked.Add(1)
	return "counted"
}

func (p countedName) Run(doc *ir.Document) (*ir.Document, []ir.Diagnostic) { return doc, nil }

// TestNew_AsksAPassForItsNameOnce pins that the name a pass answered to when
// the engine was built is the one every run uses, however many runs, refusals
// and disabled passes follow: no later call into the pass can panic outside New
// or answer another name than the one a caller disabled it by.
func TestNew_AsksAPassForItsNameOnce(t *testing.T) {
	t.Parallel()
	var asked atomic.Int32
	eng := engineOf(t, faulty{}, countedName{&asked}, notePass("other"))

	_, err := runFromMemory.run(t, eng, engine.RunOptions{})
	require.NoError(t, err)
	_, err = runFromMemory.run(t, eng, engine.RunOptions{DisablePasses: []string{"counted"}})
	require.NoError(t, err)
	_, err = runFromMemory.run(t, eng, engine.RunOptions{DisablePasses: []string{"missing"}})
	require.ErrorIs(t, err, engine.ErrUnknownPass)
	_, err = runOverADocument.run(t, eng, engine.RunOptions{})
	require.NoError(t, err)

	assert.EqualValues(t, 1, asked.Load())
}

// changed returns a pass named p that edits a copy of its input and returns it.
func changed(edit func(*ir.Document)) funcPass {
	return funcPass{"p", func(doc *ir.Document) (*ir.Document, []ir.Diagnostic) {
		next := *doc
		edit(&next)
		return &next, nil
	}}
}

// TestEngine_RunHoldsAPassToWhatItsOutputMustKeep breaks, one at a time, each
// duty the engine can read off a pass's output and its input together, and
// requires the breach to name the pass and to wrap pass.ErrContract besides
// the duty it broke. The accepted rows are the controls: a check that refused
// them would be refusing a legal transform.
func TestEngine_RunHoldsAPassToWhatItsOutputMustKeep(t *testing.T) {
	t.Parallel()
	own := ir.Diagnostic{Severity: ir.SeverityWarning, Code: "pass/own"}
	tests := []struct {
		name string
		pass pass.Pass
		want error
	}{
		{"no document", funcPass{"p", func(*ir.Document) (*ir.Document, []ir.Diagnostic) { return nil, nil }}, engine.ErrNoDocument},
		{"another IR version", changed(func(d *ir.Document) { d.IRVersion = "9.9.9" }), engine.ErrVersionChanged},
		{"no IR version", changed(func(d *ir.Document) { d.IRVersion = "" }), engine.ErrVersionChanged},
		{"a source table with an entry dropped", changed(func(d *ir.Document) { d.Sources = nil }), engine.ErrSourcesMoved},
		{"a source table with an entry replaced", changed(func(d *ir.Document) {
			d.Sources = slices.Clone(d.Sources)
			d.Sources[0].Path = "elsewhere"
		}), engine.ErrSourcesMoved},
		{"findings put on the document", changed(func(d *ir.Document) {
			d.Diagnostics = append(slices.Clone(d.Diagnostics), own)
		}), engine.ErrDiagnosticsChanged},
		{"findings replacing the document's", changed(func(d *ir.Document) {
			d.Diagnostics = []ir.Diagnostic{own}
		}), engine.ErrDiagnosticsChanged},
		{"a finding indexing no source", funcPass{"p", func(doc *ir.Document) (*ir.Document, []ir.Diagnostic) {
			return doc, []ir.Diagnostic{ir.NewDiagnostic(ir.SeverityInfo, "pass/far", "m", ir.Provenance{Source: 7})}
		}}, engine.ErrSourceOutOfRange},

		{"a document built afresh, leaving the findings to the engine", funcPass{"p", func(doc *ir.Document) (*ir.Document, []ir.Diagnostic) {
			return &ir.Document{IRVersion: doc.IRVersion, Sources: doc.Sources}, nil
		}}, nil},
		{"a copy carrying the findings it was given", changed(func(*ir.Document) {}), nil},
		{"a source appended to the table", changed(func(d *ir.Document) {
			d.Sources = append(slices.Clone(d.Sources), ir.SourceInfo{Path: "more"})
		}), nil},
		{"a finding on a source the pass appended", funcPass{"p", func(doc *ir.Document) (*ir.Document, []ir.Diagnostic) {
			next := *doc
			next.Sources = append(slices.Clone(doc.Sources), ir.SourceInfo{Path: "more"})
			return &next, []ir.Diagnostic{ir.NewDiagnostic(ir.SeverityInfo, "pass/new", "m", ir.Provenance{Source: len(doc.Sources)})}
		}}, nil},
		{"a finding on the document itself", funcPass{"p", func(doc *ir.Document) (*ir.Document, []ir.Diagnostic) {
			return doc, []ir.Diagnostic{ir.NewDiagnostic(ir.SeverityInfo, "pass/near", "m", ir.Provenance{Source: ir.NoSource})}
		}}, nil},
	}
	for _, tt := range tests {
		for _, via := range passEntries {
			t.Run(tt.name+"/"+via.name, func(t *testing.T) {
				t.Parallel()
				// The pass after the one under test must not run after a breach.
				var after pass.Pass = notePass("after")
				if tt.want != nil {
					after = unreachedPass{t: t, name: "after"}
				}
				eng := engineOf(t, faulty{}, tt.pass, after)

				res, err := via.run(t, eng, engine.RunOptions{})

				if tt.want == nil {
					require.NoError(t, err)
					require.NotNil(t, res.Document)
					return
				}
				require.ErrorIs(t, err, tt.want)
				require.ErrorIs(t, err, pass.ErrContract)
				assert.Contains(t, err.Error(), `engine: run pass "p"`)
				assert.NotContains(t, err.Error(), "\n")
				assert.Nil(t, res)
			})
		}
	}
}

// TestEngine_RunRefusesACompilerStampingAnotherIRVersion is the compile side of
// the same duty: no pass has run when the document is first read.
func TestEngine_RunRefusesACompilerStampingAnotherIRVersion(t *testing.T) {
	t.Parallel()
	for _, stamp := range []string{"", "0.0.1", ir.IRVersion + "-rc.1"} {
		for _, via := range compileEntries {
			t.Run(stamp+"/"+via.name, func(t *testing.T) {
				t.Parallel()
				comp := faulty{compile: func(sources []compilers.Source) (*ir.Document, []ir.Diagnostic, error) {
					doc := keptDocument(sources)
					doc.IRVersion = stamp
					return doc, nil, nil
				}}

				res, err := via.run(t, engineOf(t, comp), engine.RunOptions{})

				require.ErrorIs(t, err, compilers.ErrContract)
				assert.Contains(t, err.Error(), "irVersion")
				assert.Nil(t, res)
			})
		}
	}
}

// TestEngine_RunRefusesWhatCompilersAlreadyHeldBreachesOf keeps the duties
// Detection.Compile checked before this engine held stages to the rest, as rows
// of the same contract: a refusal that gives no reason, and a finding that
// indexes a source the table does not hold.
func TestEngine_RunRefusesWhatCompilersAlreadyHeldBreachesOf(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		comp faulty
		want string
	}{
		{"a refusal saying nothing", faulty{compile: func([]compilers.Source) (*ir.Document, []ir.Diagnostic, error) {
			return nil, nil, nil
		}}, "no error diagnostic"},
		{"a finding past the source table", faulty{compile: func(sources []compilers.Source) (*ir.Document, []ir.Diagnostic, error) {
			return keptDocument(sources), []ir.Diagnostic{ir.NewDiagnostic(ir.SeverityWarning, "x/y", "m", ir.Provenance{Source: 9})}, nil
		}}, "indexes source 9 of 1"},
	}
	for _, tt := range tests {
		for _, via := range compileEntries {
			t.Run(tt.name+"/"+via.name, func(t *testing.T) {
				t.Parallel()
				res, err := via.run(t, engineOf(t, tt.comp), engine.RunOptions{})

				require.ErrorIs(t, err, compilers.ErrContract)
				assert.Contains(t, err.Error(), tt.want)
				assert.Nil(t, res)
			})
		}
	}
}

// TestEngine_RunLeavesTheDocumentACompilerReturnedAlone pins that the engine
// writes the one field it owns into a copy: a compiler that returns the same
// document to every run, as a caching one would, is neither changed by a run
// nor raced by two. Under -race the concurrent half is what catches an engine
// that writes through the pointer it was handed.
func TestEngine_RunLeavesTheDocumentACompilerReturnedAlone(t *testing.T) {
	t.Parallel()
	shared := keptDocument([]compilers.Source{{Path: "spec.yaml", Data: []byte(testspec.Tiny)}})
	stored := ir.Diagnostic{Severity: ir.SeverityInfo, Code: "stub/stored"}
	shared.Diagnostics = []ir.Diagnostic{stored}
	returned := ir.NewDiagnostic(ir.SeverityWarning, "stub/returned", "m", ir.Provenance{Source: 0})
	comp := faulty{compile: func([]compilers.Source) (*ir.Document, []ir.Diagnostic, error) {
		return shared, []ir.Diagnostic{returned}, nil
	}}
	eng := engineOf(t, comp, notePass("note"))

	const runs = 8
	results := make([]*engine.Result, runs)
	errs := make([]error, runs)
	var wg sync.WaitGroup
	for i := range runs {
		wg.Go(func() { results[i], errs[i] = runFromMemory.run(t, eng, engine.RunOptions{}) })
	}
	wg.Wait()

	assert.Equal(t, []ir.Diagnostic{stored}, shared.Diagnostics, "the compiler's document is as it left it")
	for i := range runs {
		require.NoError(t, errs[i])
		assert.NotSame(t, shared, results[i].Document, "each run owns the document it returns")
		assert.Equal(t, []string{"stub/stored", "stub/returned", "test/note"}, codesOf(results[i]))
		assert.Equal(t, results[i].Diagnostics, results[i].Document.Diagnostics)
	}
}

// blocker is a compiler that ignores its context: it says it has started, then
// waits to be let go, and keeps lowering as if nothing had happened.
type blocker struct {
	stubFront
	started chan<- struct{}
	release <-chan struct{}
}

func (b blocker) Compile(_ context.Context, sources []compilers.Source, _ compilers.Options) (*ir.Document, []ir.Diagnostic, error) {
	b.started <- struct{}{}
	<-b.release
	return keptDocument(sources), nil, nil
}

// TestEngine_RunWaitsForACompilerThatIgnoresItsContext pins the outcome Run
// documents for a stage that does not stop when asked: the engine cannot stop a
// call it did not start on a goroutine of its own, and abandoning one would
// leave it reading caller-owned bytes after Run returned. So Run waits, then
// fails with the context's error, the compile's output unjudged: a stage that
// returns once its context is done may have stopped short, and nothing tells a
// whole answer from one cut, so with a pass still to run or none.
func TestEngine_RunWaitsForACompilerThatIgnoresItsContext(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		disabled []string
	}{
		{"with a pass still to run", nil},
		{"with every pass off", []string{"note"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			started, release := make(chan struct{}), make(chan struct{})
			eng := engineOf(t, blocker{started: started, release: release}, notePass("note"))
			ctx, cancel := context.WithCancel(t.Context())
			t.Cleanup(cancel)

			var (
				res *engine.Result
				err error
			)
			done := make(chan struct{})
			go func() {
				defer close(done)
				res, err = eng.RunSources(ctx, []compilers.Source{{Path: "spec.yaml", Data: []byte(testspec.Tiny)}},
					engine.RunOptions{DisablePasses: tt.disabled})
			}()
			<-started
			cancel()

			assert.Never(t, func() bool { return isClosed(done) }, 100*time.Millisecond, 5*time.Millisecond,
				"Run returned while the compiler was still running")
			close(release)
			<-done
			require.ErrorIs(t, err, context.Canceled)
			assert.Contains(t, err.Error(), `engine: compile "spec.yaml"`, "the compile is what the cancel ended")
			assert.Nil(t, res)
		})
	}
}

// isClosed reports whether ch is closed, without blocking.
func isClosed(ch <-chan struct{}) bool {
	select {
	case <-ch:
		return true
	default:
		return false
	}
}
