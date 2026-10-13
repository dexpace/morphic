package engine_test

import (
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

func (p notePass) Run(doc *ir.Document) (*ir.Document, []ir.Diagnostic) {
	return doc, []ir.Diagnostic{{Severity: ir.SeverityInfo, Code: "test/" + string(p)}}
}

// danglePass is a transform: it returns a new document whose one operation
// references an undeclared type, and leaves its input alone.
type danglePass struct{}

func (danglePass) Name() string { return "dangle" }

func (danglePass) Run(doc *ir.Document) (*ir.Document, []ir.Diagnostic) {
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

func (rebuildPass) Run(doc *ir.Document) (*ir.Document, []ir.Diagnostic) {
	return &ir.Document{IRVersion: doc.IRVersion, Sources: doc.Sources, Types: doc.Types},
		[]ir.Diagnostic{{Severity: ir.SeverityInfo, Code: "test/rebuild"}}
}

// sourcesPass is a transform that hands its document's source table to edit
// and returns a document carrying whatever edit makes of it.
type sourcesPass struct {
	edit func([]ir.SourceInfo) []ir.SourceInfo
}

func (sourcesPass) Name() string { return "sources" }

func (p sourcesPass) Run(doc *ir.Document) (*ir.Document, []ir.Diagnostic) {
	next := *doc
	next.Sources = p.edit(slices.Clone(doc.Sources))
	return &next, nil
}

// nilPass breaks the Pass contract by returning no document.
type nilPass struct{}

func (nilPass) Name() string { return "nil" }

func (nilPass) Run(*ir.Document) (*ir.Document, []ir.Diagnostic) { return nil, nil }

// runWith compiles the tiny spec with front, runs each of passes that disabled
// does not name, and returns the result with its diagnostics' codes in order.
func runWith(
	t *testing.T, front compilers.Compiler, passes []pass.Pass, disabled ...string,
) (*engine.Result, []string) {
	t.Helper()
	eng, err := engine.NewWithPasses(passes, front)
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

// TestDefaultPasses_Order pins the passes a default engine runs and their
// order, which a change to either has to restate here on purpose. The names are
// literals because callers type them, and they must be distinct, since
// disabling one would otherwise disable both.
func TestDefaultPasses_Order(t *testing.T) {
	t.Parallel()

	passes := engine.DefaultPasses()
	order := make([]string, 0, len(passes))
	distinct := make(map[string]struct{}, len(passes))
	for _, p := range passes {
		order = append(order, p.Name())
		distinct[p.Name()] = struct{}{}
	}

	assert.Empty(t, cmp.Diff([]string{"validate"}, order))
	assert.Len(t, distinct, len(order), "pass names must be distinct: %v", order)
	assert.Equal(t, pass.ValidateName, engine.ValidatePass)
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
	eng, err := engine.NewWithPasses([]pass.Pass{notePass("first"), notePass("second")}, splitDiagCompiler{})
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
			eng, err := engine.NewWithPasses(passes, splitDiagCompiler{})
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
	eng, err := engine.NewWithPasses([]pass.Pass{notePass("first"), nilPass{}}, splitDiagCompiler{})
	require.NoError(t, err)

	res, err := eng.Run(t.Context(), writeSpec(t, testspec.Tiny), engine.RunOptions{})

	require.ErrorIs(t, err, engine.ErrNoDocument)
	assert.Contains(t, err.Error(), `"nil"`)
	assert.Nil(t, res)
}

// TestEngine_RunKeepsTheCompilersDiagnosticsThroughATransform pins that the
// engine, not a pass, owns Document.Diagnostics: a transform that builds a
// fresh document loses nothing the compiler reported on either channel.
func TestEngine_RunKeepsTheCompilersDiagnosticsThroughATransform(t *testing.T) {
	t.Parallel()
	want := ir.Diagnostic{Severity: ir.SeverityError, Code: "stub/spec-problem"}
	fronts := []struct {
		name  string
		front splitDiagCompiler
	}{
		{"mirrored", splitDiagCompiler{stored: []ir.Diagnostic{want}, returned: []ir.Diagnostic{want}}},
		{"returned only", splitDiagCompiler{returned: []ir.Diagnostic{want}}},
		{"stored only", splitDiagCompiler{stored: []ir.Diagnostic{want}}},
	}
	for _, tt := range fronts {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, got := runWith(t, tt.front, []pass.Pass{rebuildPass{}, notePass("after")})
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
	fronts := []struct {
		name  string
		front splitDiagCompiler
	}{
		{"mirrored", splitDiagCompiler{stored: []ir.Diagnostic{echo}, returned: []ir.Diagnostic{echo}}},
		{"returned only", splitDiagCompiler{returned: []ir.Diagnostic{echo}}},
		{"stored only", splitDiagCompiler{stored: []ir.Diagnostic{echo}}},
	}
	for _, tt := range fronts {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, got := runWith(t, tt.front, []pass.Pass{notePass("echo")})
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
	plain, err := engine.NewWithPasses(nil, openapi.New())
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
			eng, err := engine.NewWithPasses(
				[]pass.Pass{notePass("before"), sourcesPass{tt.edit}, notePass("after")}, openapi.New())
			require.NoError(t, err)

			res, err := eng.Run(t.Context(), path, engine.RunOptions{})

			if tt.refused {
				require.ErrorIs(t, err, engine.ErrSourcesMoved)
				assert.Contains(t, err.Error(), `"sources"`)
				assert.Nil(t, res)
				return
			}
			require.NoError(t, err)
			assert.Empty(t, cmp.Diff(tt.edit(slices.Clone(compiled.Sources)), res.Sources))
		})
	}
}
