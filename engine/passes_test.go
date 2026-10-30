// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package engine_test

import (
	"context"
	"fmt"
	"slices"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi"
	"github.com/dexpace/morphic/engine"
	"github.com/dexpace/morphic/internal/testspec"
	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/pass"
)

// notePass is an analysis that reports one diagnostic coded after its own name,
// so a run's diagnostics say which passes ran and in what order.
type notePass string

func (p notePass) Name() string { return string(p) }

func (p notePass) Run(_ context.Context, doc *ir.Document) (*ir.Document, []ir.Diagnostic) {
	return doc, []ir.Diagnostic{{Severity: ir.SeverityInfo, Code: "test/" + string(p)}}
}

// danglePass is a transform: it returns a new document whose one operation
// references an undeclared type, and leaves its input alone.
type danglePass struct{}

func (danglePass) Name() string { return "dangle" }

func (danglePass) Run(_ context.Context, doc *ir.Document) (*ir.Document, []ir.Diagnostic) {
	next := *doc
	next.Services = []ir.Service{{
		ID: "s/x",
		Groups: []ir.OperationGroup{{
			Operations: []ir.Operation{{
				ID: "op/x",
				Errors: []ir.ErrorCase{{
					Payload: &ir.Payload{Contents: []ir.Content{{Type: ir.TypeRef{Target: "t/missing"}}}},
				}},
			}},
		}},
	}}
	return &next, []ir.Diagnostic{{Severity: ir.SeverityInfo, Code: "test/dangle"}}
}

// rebuildPass is a transform that builds its result afresh, carrying over the
// types and the source table and nothing else, Document.Diagnostics included.
type rebuildPass struct{}

func (rebuildPass) Name() string { return "rebuild" }

func (rebuildPass) Run(_ context.Context, doc *ir.Document) (*ir.Document, []ir.Diagnostic) {
	return &ir.Document{IRVersion: doc.IRVersion, Sources: doc.Sources, Types: doc.Types},
		[]ir.Diagnostic{{Severity: ir.SeverityInfo, Code: "test/rebuild"}}
}

// sourcesPass is a transform that hands its document's source table to edit
// and returns a document carrying whatever edit makes of it.
type sourcesPass struct {
	edit func([]ir.SourceInfo) []ir.SourceInfo
}

func (sourcesPass) Name() string { return "sources" }

func (p sourcesPass) Run(_ context.Context, doc *ir.Document) (*ir.Document, []ir.Diagnostic) {
	next := *doc
	next.Sources = p.edit(slices.Clone(doc.Sources))
	return &next, nil
}

// nilPass breaks the Pass contract by returning no document.
type nilPass struct{}

func (nilPass) Name() string { return "nil" }

func (nilPass) Run(context.Context, *ir.Document) (*ir.Document, []ir.Diagnostic) { return nil, nil }

// cancelPass is an analysis that cancels the run it is part of. A broken one
// returns no document as it goes, which would break the contract were it read.
type cancelPass struct {
	cancel context.CancelFunc
	broken bool
}

func (cancelPass) Name() string { return "cancel" }

func (p cancelPass) Run(_ context.Context, doc *ir.Document) (*ir.Document, []ir.Diagnostic) {
	p.cancel()
	if p.broken {
		return nil, nil
	}
	return doc, nil
}

// unreachedPass fails its test if it runs at all.
type unreachedPass struct {
	t    *testing.T
	name string
}

func (p unreachedPass) Name() string { return p.name }

func (p unreachedPass) Run(_ context.Context, doc *ir.Document) (*ir.Document, []ir.Diagnostic) {
	p.t.Errorf("pass %q ran in a cancelled run", p.name)
	return doc, nil
}

// cancelCompiler compiles as splitDiagCompiler does, cancelling the run on its
// way out.
type cancelCompiler struct {
	splitDiagCompiler
	cancel context.CancelFunc
}

func (c cancelCompiler) Compile(
	ctx context.Context, sources []compilers.Source, opts compilers.Options,
) (*ir.Document, []ir.Diagnostic, error) {
	c.cancel()
	return c.splitDiagCompiler.Compile(ctx, sources, opts)
}

// runWith compiles the tiny spec with comp, runs each of passes that disabled
// does not name, and returns the result with its diagnostics' codes in order.
func runWith(
	t *testing.T, comp compilers.Compiler, passes []pass.Pass, disabled ...string,
) (*engine.Result, []string) {
	t.Helper()
	eng, err := engine.New(engine.WithCompilers(comp), engine.WithPasses(passes...))
	require.NoError(t, err)

	res, err := eng.Run(t.Context(), writeSpec(t, testspec.Tiny), engine.RunOptions{DisablePasses: disabled})
	require.NoError(t, err)
	require.NotNil(t, res.Document)
	assert.Empty(t, cmp.Diff(res.Diagnostics, res.Document.Diagnostics),
		"the Result and the Document carry one list")

	codes := make([]string, 0, len(res.Diagnostics))
	for _, d := range res.Diagnostics {
		codes = append(codes, d.Code)
	}
	return res, codes
}

// TestPasses_ValidateRunsLast holds the default list, read rather than
// restated, to what its order means: validate last, since it judges the
// document the run returns, under the name engine.ValidatePass spells. Each
// call builds the list afresh, so a caller composing its own from it cannot
// change the next.
func TestPasses_ValidateRunsLast(t *testing.T) {
	t.Parallel()
	passes := engine.Passes()
	require.NotEmpty(t, passes)
	names := passNames(passes)
	passes[0] = nil

	assert.Equal(t, pass.ValidateName, names[len(names)-1], "validate runs after every transform")
	assert.Equal(t, pass.ValidateName, engine.ValidatePass)
	assert.Equal(t, names, passNames(engine.Passes()), "a caller's change to the list reached the next")
}

// TestEngine_RunThreadsEachPassIntoTheNext shows each pass reads the document
// the one before it returned, so their order decides what validate finds.
func TestEngine_RunThreadsEachPassIntoTheNext(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		passes []pass.Pass
		want   []string
	}{
		{"transform then validate", []pass.Pass{danglePass{}, pass.Validator{}},
			[]string{"test/dangle", "ir/dangling-type-ref"}},
		{"validate then transform", []pass.Pass{pass.Validator{}, danglePass{}},
			[]string{"test/dangle"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			res, got := runWith(t, splitDiagCompiler{}, tt.passes)

			assert.Empty(t, cmp.Diff(tt.want, got))
			assert.Len(t, res.Document.Services, 1, "the transform's document is the one returned")
		})
	}
}

// TestEngine_RunDisablesPassesByName pins that a name switches off its own
// pass and no other, whatever else is named, and that naming one twice is
// harmless.
func TestEngine_RunDisablesPassesByName(t *testing.T) {
	t.Parallel()
	passes := []pass.Pass{notePass("first"), notePass("second")}
	tests := []struct {
		disabled []string
		want     []string
	}{
		{nil, []string{"test/first", "test/second"}},
		{[]string{"first"}, []string{"test/second"}},
		{[]string{"second"}, []string{"test/first"}},
		{[]string{"second", "first"}, []string{}},
		{[]string{"first", "first"}, []string{"test/second"}},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprint(tt.disabled), func(t *testing.T) {
			t.Parallel()
			_, got := runWith(t, splitDiagCompiler{}, passes, tt.disabled...)
			assert.Empty(t, cmp.Diff(tt.want, got))
		})
	}
}

// TestEngine_RunLeavesItsPassesForTheNextRun pins that disabling a pass is a
// property of one run: the next run over the same engine runs it again.
func TestEngine_RunLeavesItsPassesForTheNextRun(t *testing.T) {
	t.Parallel()
	eng, err := engine.New(engine.WithCompilers(splitDiagCompiler{}), engine.WithPasses(notePass("first"), notePass("second")))
	require.NoError(t, err)
	path := writeSpec(t, testspec.Tiny)

	_, err = eng.Run(t.Context(), path, engine.RunOptions{DisablePasses: []string{"first"}})
	require.NoError(t, err)

	res, err := eng.Run(t.Context(), path, engine.RunOptions{})

	require.NoError(t, err)
	assert.Empty(t, cmp.Diff([]ir.Diagnostic{
		{Severity: ir.SeverityInfo, Code: "test/first"},
		{Severity: ir.SeverityInfo, Code: "test/second"},
	}, res.Diagnostics))
}

// TestEngine_RunRefusesAnUnknownPass pins that a name no pass carries fails the
// run before the spec is read, which the missing path shows, and that the
// error names both what was asked for and what exists.
func TestEngine_RunRefusesAnUnknownPass(t *testing.T) {
	t.Parallel()
	passes := []pass.Pass{notePass("first"), notePass("second")}
	tests := []struct {
		name     string
		disabled []string
		want     string
	}{
		{"invented", []string{"no-such-pass"}, `"no-such-pass"`},
		{"beside a known one", []string{"first", "no-such-pass"}, `"no-such-pass"`},
		{"empty", []string{""}, `""`},
		{"another case", []string{"First"}, `"First"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			eng, err := engine.New(engine.WithCompilers(splitDiagCompiler{}), engine.WithPasses(passes...))
			require.NoError(t, err)

			res, err := eng.Run(t.Context(), "/no/such/spec.yaml",
				engine.RunOptions{DisablePasses: tt.disabled})

			require.ErrorIs(t, err, engine.ErrUnknownPass)
			assert.Contains(t, err.Error(), tt.want)
			assert.Contains(t, err.Error(), "(known: first, second)")
			assert.Nil(t, res)
		})
	}
}

// TestEngine_RunRefusesAPassReturningNoDocument pins that a pass breaking its
// contract fails the run, naming the pass, rather than being papered over.
func TestEngine_RunRefusesAPassReturningNoDocument(t *testing.T) {
	t.Parallel()
	eng, err := engine.New(engine.WithCompilers(splitDiagCompiler{}), engine.WithPasses(notePass("first"), nilPass{}))
	require.NoError(t, err)

	res, err := eng.Run(t.Context(), writeSpec(t, testspec.Tiny), engine.RunOptions{})

	require.ErrorIs(t, err, engine.ErrNoDocument)
	require.ErrorIs(t, err, pass.ErrContract)
	assert.Contains(t, err.Error(), `"nil"`)
	assert.Nil(t, res)
}

// TestEngine_RunKeepsTheCompilersDiagnosticsThroughATransform pins that the
// engine, not a pass, owns Document.Diagnostics: a transform that builds a
// fresh document loses nothing the compiler reported on either channel.
func TestEngine_RunKeepsTheCompilersDiagnosticsThroughATransform(t *testing.T) {
	t.Parallel()
	want := ir.Diagnostic{Severity: ir.SeverityError, Code: "stub/spec-problem"}
	cases := []struct {
		name string
		comp splitDiagCompiler
	}{
		{"mirrored", splitDiagCompiler{stored: []ir.Diagnostic{want}, returned: []ir.Diagnostic{want}}},
		{"returned only", splitDiagCompiler{returned: []ir.Diagnostic{want}}},
		{"stored only", splitDiagCompiler{stored: []ir.Diagnostic{want}}},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, got := runWith(t, tt.comp, []pass.Pass{rebuildPass{}, notePass("after")})
			assert.Empty(t, cmp.Diff([]string{want.Code, "test/rebuild", "test/after"}, got))
		})
	}
}

// TestEngine_RunKeepsOneCopyOfAFindingAPassRepeats pins that a pass reporting
// what the compiler already reported, on either channel, leaves one copy of it:
// identity is the whole value, as it is where compilers/compile dedupes.
func TestEngine_RunKeepsOneCopyOfAFindingAPassRepeats(t *testing.T) {
	t.Parallel()
	echo := ir.Diagnostic{Severity: ir.SeverityInfo, Code: "test/echo"}
	cases := []struct {
		name string
		comp splitDiagCompiler
	}{
		{"mirrored", splitDiagCompiler{stored: []ir.Diagnostic{echo}, returned: []ir.Diagnostic{echo}}},
		{"returned only", splitDiagCompiler{returned: []ir.Diagnostic{echo}}},
		{"stored only", splitDiagCompiler{stored: []ir.Diagnostic{echo}}},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, got := runWith(t, tt.comp, []pass.Pass{notePass("echo")})
			assert.Empty(t, cmp.Diff([]string{echo.Code}, got))
		})
	}
}

// TestEngine_RunHoldsPassesToTheSourceTable pins that a pass may append to
// Document.Sources but not drop, replace or move an entry, because what the
// compiler reported indexes the table by position. A refusal names the pass
// that broke the rule, not a later one.
func TestEngine_RunHoldsPassesToTheSourceTable(t *testing.T) {
	t.Parallel()
	path := writeSpec(t, testspec.Tiny)
	plain, err := engine.New(engine.WithCompilers(openapi.New()), engine.WithPasses())
	require.NoError(t, err)
	compiled, err := plain.Run(t.Context(), path, engine.RunOptions{})
	require.NoError(t, err)
	require.Len(t, compiled.Sources, 1, "the edits below assume one source")

	added := ir.SourceInfo{Format: "openapi", Path: "added.yaml"}
	tests := []struct {
		name    string
		edit    func([]ir.SourceInfo) []ir.SourceInfo
		refused bool
	}{
		{"kept", func(s []ir.SourceInfo) []ir.SourceInfo { return s }, false},
		{"appended to", func(s []ir.SourceInfo) []ir.SourceInfo { return append(s, added) }, false},
		{"dropped", func([]ir.SourceInfo) []ir.SourceInfo { return nil }, true},
		{"replaced", func([]ir.SourceInfo) []ir.SourceInfo { return []ir.SourceInfo{added} }, true},
		{"moved", func(s []ir.SourceInfo) []ir.SourceInfo { return append([]ir.SourceInfo{added}, s...) }, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			eng, err := engine.New(engine.WithCompilers(openapi.New()), engine.WithPasses(notePass("before"), sourcesPass{tt.edit}, notePass("after")))
			require.NoError(t, err)

			res, err := eng.Run(t.Context(), path, engine.RunOptions{})

			if tt.refused {
				require.ErrorIs(t, err, engine.ErrSourcesMoved)
				require.ErrorIs(t, err, pass.ErrContract)
				assert.Contains(t, err.Error(), `"sources"`)
				assert.Nil(t, res)
				return
			}
			require.NoError(t, err)
			assert.Empty(t, cmp.Diff(tt.edit(slices.Clone(compiled.Sources)), res.Sources))
		})
	}
}

// TestEngine_RunHoldsPassDiagnosticsToTheSourceTable pins the other half of
// what Result.Sources promises: every finding a pass reports indexes the table
// or no source at all, so a renderer never looks a file up past its end. The
// compiler is held to the same by compilers.Detection.Compile.
func TestEngine_RunHoldsPassDiagnosticsToTheSourceTable(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		source  int
		refused bool
	}{
		{source: 0},
		{source: ir.NoSource},
		{source: 1, refused: true},
		{source: -2, refused: true},
	} {
		t.Run(fmt.Sprint(tc.source), func(t *testing.T) {
			t.Parallel()
			finding := ir.NewDiagnostic(ir.SeverityWarning, "x/finding", "m", ir.Provenance{Source: tc.source})
			eng, err := engine.New(engine.WithCompilers(splitDiagCompiler{}), engine.WithPasses(reportPass{[]ir.Diagnostic{finding}}))
			require.NoError(t, err)

			res, err := eng.Run(t.Context(), writeSpec(t, testspec.Tiny), engine.RunOptions{})

			if !tc.refused {
				require.NoError(t, err)
				assert.Contains(t, res.Diagnostics, finding)
				return
			}
			require.ErrorIs(t, err, engine.ErrSourceOutOfRange)
			require.ErrorIs(t, err, pass.ErrContract)
			assert.Contains(t, err.Error(), `"report"`, "the error names the pass")
			assert.Contains(t, err.Error(), fmt.Sprintf("x/finding at source %d of 1", tc.source),
				"and the finding, with the index it gave")
			assert.Nil(t, res)
		})
	}
}

// TestEngine_RunStopsAtTheStageTheCancelLandsIn pins that a run cancelled
// during a stage, the last included, runs no pass after it and fails naming
// that stage, each held to its context as it returns. What the stage returned
// is never judged: a pass cut short may return what its contract forbids.
func TestEngine_RunStopsAtTheStageTheCancelLandsIn(t *testing.T) {
	t.Parallel()
	type setup func(t *testing.T, cancel context.CancelFunc) (compilers.Compiler, []pass.Pass)
	tests := []struct {
		name  string
		setup setup
		names string
	}{
		{"during the compile", func(t *testing.T, cancel context.CancelFunc) (compilers.Compiler, []pass.Pass) {
			return cancelCompiler{cancel: cancel}, []pass.Pass{unreachedPass{t, "first"}}
		}, "engine: compile"},
		{"during a pass", func(t *testing.T, cancel context.CancelFunc) (compilers.Compiler, []pass.Pass) {
			return splitDiagCompiler{}, []pass.Pass{cancelPass{cancel: cancel}, unreachedPass{t, "after"}}
		}, `stop in pass "cancel"`},
		{"during the last pass", func(_ *testing.T, cancel context.CancelFunc) (compilers.Compiler, []pass.Pass) {
			return splitDiagCompiler{}, []pass.Pass{notePass("first"), cancelPass{cancel: cancel}}
		}, `stop in pass "cancel"`},
		{"during a pass returning no document", func(_ *testing.T, cancel context.CancelFunc) (compilers.Compiler, []pass.Pass) {
			return splitDiagCompiler{}, []pass.Pass{cancelPass{cancel: cancel, broken: true}}
		}, `stop in pass "cancel"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx, cancel := context.WithCancel(t.Context())
			t.Cleanup(cancel)
			comp, passes := tt.setup(t, cancel)
			eng, err := engine.New(engine.WithCompilers(comp), engine.WithPasses(passes...))
			require.NoError(t, err)

			res, err := eng.Run(ctx, writeSpec(t, testspec.Tiny), engine.RunOptions{})

			require.ErrorIs(t, err, context.Canceled)
			assert.NotErrorIs(t, err, pass.ErrContract, "what a pass returned once cancelled is not judged")
			assert.Contains(t, err.Error(), tt.names)
			assert.Nil(t, res)
		})
	}
}

// ctxKey keys the value TestEngine_HandsEachPassTheRunsContext puts on a run's
// context.
type ctxKey struct{}

// contextPass is an analysis recording, in seen, the value its context carries
// under ctxKey.
type contextPass struct{ seen *[]any }

func (contextPass) Name() string { return "context" }

func (p contextPass) Run(ctx context.Context, doc *ir.Document) (*ir.Document, []ir.Diagnostic) {
	*p.seen = append(*p.seen, ctx.Value(ctxKey{}))
	return doc, nil
}

// TestEngine_HandsEachPassTheRunsContext pins that a pass runs under the
// context its run was given, whether the run compiles or starts from a
// document: a pass handed another would go on past a cancel the run was told
// of.
func TestEngine_HandsEachPassTheRunsContext(t *testing.T) {
	t.Parallel()
	var seen []any
	eng := engineOf(t, splitDiagCompiler{}, contextPass{&seen})
	ctx := context.WithValue(t.Context(), ctxKey{}, "the run's")

	_, err := eng.Run(ctx, writeSpec(t, testspec.Tiny), engine.RunOptions{})
	require.NoError(t, err)
	_, err = eng.RunDocument(ctx, danglingDocument(), engine.RunOptions{})
	require.NoError(t, err)

	assert.Equal(t, []any{"the run's", "the run's"}, seen)
}

// TestEngine_RunCancelledDuringVerificationFails pins the one step a cancel
// waits out in a verified run, holding a document to the invariants, which
// reads no context: ctx is asked after it, before the next pass or after the
// last, so a run cancelled during one fails as one cancelled in a stage does.
func TestEngine_RunCancelledDuringVerificationFails(t *testing.T) {
	t.Parallel()
	rename := changed(func(d *ir.Document) { d.Name = "Renamed" })
	tests := []struct {
		name   string
		passes func(t *testing.T) []pass.Pass
		at     int
		names  string
	}{
		{"the compile's, before a pass", func(t *testing.T) []pass.Pass {
			return []pass.Pass{unreachedPass{t, "first"}}
		}, 1, `stop before pass "first"`},
		{"the compile's, with no pass", func(*testing.T) []pass.Pass { return nil }, 1, "stop after the passes"},
		{"the last pass's", func(*testing.T) []pass.Pass { return []pass.Pass{rename} }, 2, "stop after the passes"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx, cancel := context.WithCancel(t.Context())
			t.Cleanup(cancel)
			eng := engineOf(t, openapi.New(), tt.passes(t)...)
			verified := 0
			engine.OnVerify(eng, func() {
				if verified++; verified == tt.at {
					cancel()
				}
			})

			res, err := eng.Run(ctx, writeSpec(t, testspec.Tiny), engine.RunOptions{Verify: true})

			require.ErrorIs(t, err, context.Canceled)
			assert.Contains(t, err.Error(), tt.names)
			assert.Nil(t, res)
			assert.Equal(t, tt.at, verified, "nothing was verified after the cancel")
		})
	}
}
