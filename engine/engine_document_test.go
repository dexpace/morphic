// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package engine_test

import (
	"context"
	"encoding/json/v2"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/engine"
	"github.com/dexpace/morphic/internal/testspec"
	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/pass"
)

// danglingDocument returns a document whose one operation names a type the
// registry does not hold, which only the validate pass reports.
func danglingDocument() *ir.Document {
	return &ir.Document{
		IRVersion: ir.IRVersion,
		Name:      "Dangling",
		Types:     ir.TypeRegistry{},
		Services: []ir.Service{{ID: "s/x", Groups: []ir.OperationGroup{{Operations: []ir.Operation{{
			ID: "op/x", Errors: []ir.ErrorCase{{Payload: &ir.Payload{
				Contents: []ir.Content{{Type: ir.TypeRef{Target: "t/missing"}}},
			}}},
		}}}}}},
		Sources: []ir.SourceInfo{{Path: "spec.yaml"}},
	}
}

// roundTrip returns a deep copy of doc, made the way a persisted document is
// read back: through its own JSON encoding.
func roundTrip(t *testing.T, doc *ir.Document) *ir.Document {
	t.Helper()
	raw, err := json.Marshal(doc)
	require.NoError(t, err)
	var back ir.Document
	require.NoError(t, json.Unmarshal(raw, &back))
	return &back
}

// TestEngine_RunDocumentRunsThePasses pins that a document already compiled
// goes through the passes as a compiled one does: the validate pass reports
// its dangling reference, the document's own findings come first, and its
// sources stand as the table the findings index. No compiler ran, so the
// Result names no format.
func TestEngine_RunDocumentRunsThePasses(t *testing.T) {
	t.Parallel()
	eng, err := engine.New()
	require.NoError(t, err)
	doc := danglingDocument()
	own := ir.NewDiagnostic(ir.SeverityWarning, "openapi/note", "from the compile", ir.Provenance{Source: 0})
	doc.Diagnostics = []ir.Diagnostic{own}

	res, err := eng.RunDocument(t.Context(), doc, engine.RunOptions{})

	require.NoError(t, err)
	require.NotNil(t, res.Document)
	require.NotEmpty(t, res.Diagnostics)
	assert.Equal(t, own, res.Diagnostics[0], "the document's own findings come first")
	assert.True(t, hasDiagCode(res.Diagnostics, "ir/dangling-type-ref"), "%+v", res.Diagnostics)
	assert.Equal(t, res.Diagnostics, res.Document.Diagnostics, "both channels carry the whole list")
	assert.Equal(t, doc.Sources, res.Sources)
	assert.Zero(t, res.Format)
}

// TestEngine_RunDocumentLeavesTheDocumentAlone pins that the caller's document
// is read and never written, though the engine writes Diagnostics onto the
// document it returns and the validate pass returns the document it is handed.
func TestEngine_RunDocumentLeavesTheDocumentAlone(t *testing.T) {
	t.Parallel()
	eng, err := engine.New()
	require.NoError(t, err)
	doc := danglingDocument()
	before := roundTrip(t, doc)

	res, err := eng.RunDocument(t.Context(), doc, engine.RunOptions{})

	require.NoError(t, err)
	require.NotEmpty(t, res.Document.Diagnostics, "the returned document carries the findings")
	assert.Empty(t, cmp.Diff(before, roundTrip(t, doc)), "the caller's document is unchanged")
	assert.Empty(t, doc.Diagnostics)
}

// notedDanglingCompiler lowers every source to danglingDocument, which is
// stamped, with a finding of its own stored on it, so a run of it holds the
// compile's finding and the validate pass's.
type notedDanglingCompiler struct{ stubFront }

func (notedDanglingCompiler) Compile(_ context.Context, sources []compilers.Source, _ compilers.Options) (*ir.Document, []ir.Diagnostic, error) {
	doc := danglingDocument()
	doc.Sources = sourcesOf("openapi@3.1", sources)
	doc.Diagnostics = []ir.Diagnostic{
		ir.NewDiagnostic(ir.SeverityWarning, "stub/note", "from the compile", ir.Provenance{Source: 0}),
	}
	return doc, nil, nil
}

// findingsSpec compiles to a document carrying the compiler's findings: an
// error for the reference to a schema that is not there, and a note for the
// validation-only keyword kept verbatim.
const findingsSpec = `openapi: 3.1.0
info: {title: Findings, version: "1"}
paths:
  /x:
    get:
      responses:
        "200":
          description: ok
          content:
            application/json: {schema: {$ref: '#/components/schemas/Missing'}}
components:
  schemas:
    S: {type: object, not: {required: [x]}}
`

// TestEngine_RunDocumentAgreesWithRun pins the use RunDocument exists for: a
// document Run compiled, persisted and read back, runs to the verdict and the
// findings Run reported, each once. The second row is the one that tells: the
// validate pass finds its own finding again on the persisted document.
func TestEngine_RunDocumentAgreesWithRun(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		compiler func() (*engine.Engine, error)
		spec     string
		// passCode is a code the validate pass reported, or "" for none.
		passCode string
	}{
		{"the compiler's findings", func() (*engine.Engine, error) { return engine.New() }, findingsSpec, ""},
		{"the validate pass's findings", func() (*engine.Engine, error) {
			return engine.New(engine.WithCompilers(notedDanglingCompiler{}))
		}, testspec.Tiny, "ir/dangling-type-ref"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			eng, err := tt.compiler()
			require.NoError(t, err)
			compiled, err := eng.Run(t.Context(), writeSpec(t, tt.spec), engine.RunOptions{})
			require.NoError(t, err)
			require.NotNil(t, compiled.Document)
			require.NotEmpty(t, compiled.Diagnostics, "a run with no findings would agree vacuously")
			if tt.passCode != "" {
				require.True(t, hasDiagCode(compiled.Diagnostics, tt.passCode), "%+v", compiled.Diagnostics)
			}

			res, err := eng.RunDocument(t.Context(), roundTrip(t, compiled.Document), engine.RunOptions{})

			require.NoError(t, err)
			assert.Empty(t, cmp.Diff(compiled.Diagnostics, res.Diagnostics))
			assert.Equal(t, compiled.Failed, res.Failed)
			// An empty map round-trips as an absent one, which the encoding leaves out.
			assert.Empty(t, cmp.Diff(compiled.Document, res.Document, cmpopts.EquateEmpty()))
		})
	}
}

// TestEngine_RunDocumentDoesNotAliasTheCallersFindings pins that the list a run
// returns is its own even when the passes add nothing to the document's: an
// append to it reaches neither the caller's document nor its spare capacity.
func TestEngine_RunDocumentDoesNotAliasTheCallersFindings(t *testing.T) {
	t.Parallel()
	eng, err := engine.New()
	require.NoError(t, err)
	doc := danglingDocument()
	own := ir.NewDiagnostic(ir.SeverityWarning, "stub/note", "from the compile", ir.Provenance{Source: 0})
	doc.Diagnostics = make([]ir.Diagnostic, 1, 4)
	doc.Diagnostics[0] = own

	res, err := eng.RunDocument(t.Context(), doc, engine.RunOptions{DisablePasses: []string{pass.ValidateName}})
	require.NoError(t, err)
	res.Diagnostics = append(res.Diagnostics, own)
	res.Diagnostics[0].Message = "changed"

	assert.Equal(t, "from the compile", doc.Diagnostics[0].Message, "the caller's list is not the run's")
	assert.Zero(t, doc.Diagnostics[:2][1], "nothing reached the caller's spare capacity")
}

// TestEngine_RunDocumentHonoursDisabledPasses pins that RunDocument takes
// RunOptions.DisablePasses as Run does: a pass turned off reports nothing, and
// a name no pass carries is ErrUnknownPass.
func TestEngine_RunDocumentHonoursDisabledPasses(t *testing.T) {
	t.Parallel()
	eng, err := engine.New()
	require.NoError(t, err)

	off, err := eng.RunDocument(t.Context(), danglingDocument(),
		engine.RunOptions{DisablePasses: []string{pass.ValidateName}})
	require.NoError(t, err)
	_, err = eng.RunDocument(t.Context(), danglingDocument(), engine.RunOptions{DisablePasses: []string{"nope"}})

	assert.Empty(t, off.Diagnostics)
	require.ErrorIs(t, err, engine.ErrUnknownPass)
}

// TestEngine_RunDocumentRefusals pins that a run RunDocument cannot make is a
// Go error naming its fault: no document, one from another IR generation or
// none, one holding a finding past its source table or at a sentinel the IR
// does not declare, compiler options no compiler would read, a threshold that
// names none, an unbuilt engine, and a cancelled context, which stops it before
// the first pass.
func TestEngine_RunDocumentRefusals(t *testing.T) {
	t.Parallel()
	built, err := engine.New()
	require.NoError(t, err)
	stale := danglingDocument()
	stale.IRVersion = "0.0.1"
	unstamped := danglingDocument()
	unstamped.IRVersion = ""
	pastTable, atSentinel := danglingDocument(), danglingDocument()
	pastTable.Diagnostics = []ir.Diagnostic{
		ir.NewDiagnostic(ir.SeverityWarning, "x/near", "m", ir.Provenance{Source: 0}),
		ir.NewDiagnostic(ir.SeverityWarning, "x/far", "m", ir.Provenance{Source: 1}),
	}
	atSentinel.Diagnostics = []ir.Diagnostic{
		ir.NewDiagnostic(ir.SeverityWarning, "x/odd", "m", ir.Provenance{Source: -2}),
	}
	tests := []struct {
		name      string
		eng       *engine.Engine
		cancelled bool
		doc       *ir.Document
		opts      engine.RunOptions
		want      string
	}{
		{"no document", built, false, nil, engine.RunOptions{}, "no document"},
		{"another generation", built, false, stale, engine.RunOptions{}, `"0.0.1"`},
		{"no generation", built, false, unstamped, engine.RunOptions{}, "carries no IR version"},
		{"a finding past the table", built, false, pastTable, engine.RunOptions{}, "x/far indexes source 1 of 1"},
		{"a finding at an undeclared sentinel", built, false, atSentinel, engine.RunOptions{},
			"x/odd indexes source -2 of 1"},
		{"format options", built, false, danglingDocument(),
			engine.RunOptions{FormatOptions: struct{}{}}, "compiles nothing"},
		{"compiler options", built, false, danglingDocument(),
			engine.RunOptions{CompilerOptions: map[string]string{"grouping": "tags"}}, "compiles nothing"},
		{"a threshold naming none", built, false, danglingDocument(),
			engine.RunOptions{FailOn: "fatal"}, "unknown fail-on threshold"},
		{"an unbuilt engine", &engine.Engine{}, false, danglingDocument(), engine.RunOptions{}, "uninitialized"},
		{"a cancelled context", built, true, danglingDocument(), engine.RunOptions{}, "context canceled"},
		{"a cancelled context, every pass off", built, true, danglingDocument(),
			engine.RunOptions{DisablePasses: []string{pass.ValidateName}}, "context canceled"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx, cancel := context.WithCancel(t.Context())
			if tt.cancelled {
				cancel()
			}
			t.Cleanup(cancel)

			res, err := tt.eng.RunDocument(ctx, tt.doc, tt.opts)

			require.Error(t, err)
			assert.Nil(t, res)
			assert.Contains(t, err.Error(), tt.want)
		})
	}
}

// TestEngine_RunDocumentUnderACancelledContextVerifiesNothing pins the ask
// before RunDocument's first step. Holding the input to the invariants reads no
// context, so a run cancelled before it starts never begins that step, though
// with no pass after it the run would fail all the same.
func TestEngine_RunDocumentUnderACancelledContextVerifiesNothing(t *testing.T) {
	t.Parallel()
	eng := engineOf(t, collidingCompiler{})
	verified := 0
	engine.OnVerify(eng, func() { verified++ })
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	res, err := eng.RunDocument(ctx, danglingDocument(), engine.RunOptions{Verify: true})

	require.ErrorIs(t, err, context.Canceled)
	assert.Nil(t, res)
	assert.Zero(t, verified)
}

// TestEngine_RunDocumentFailsOnAPassBreakingItsContract pins that RunDocument
// holds the passes to their contract as Run does: one returning no document, or
// moving an entry of the source table, fails the run naming it, and one that
// cancels the run fails it there, running no pass after it.
func TestEngine_RunDocumentFailsOnAPassBreakingItsContract(t *testing.T) {
	t.Parallel()
	dropSources := sourcesPass{edit: func([]ir.SourceInfo) []ir.SourceInfo { return nil }}
	tests := []struct {
		name   string
		passes func(t *testing.T, cancel context.CancelFunc) []pass.Pass
		want   error
	}{
		{"no document", func(*testing.T, context.CancelFunc) []pass.Pass {
			return []pass.Pass{nilPass{}}
		}, engine.ErrNoDocument},
		{"a source dropped", func(*testing.T, context.CancelFunc) []pass.Pass {
			return []pass.Pass{dropSources}
		}, engine.ErrSourcesMoved},
		{"cancelled by a pass", func(t *testing.T, cancel context.CancelFunc) []pass.Pass {
			return []pass.Pass{cancelPass{cancel: cancel}, unreachedPass{t: t, name: "after"}}
		}, context.Canceled},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx, cancel := context.WithCancel(t.Context())
			t.Cleanup(cancel)
			eng, err := engine.New(engine.WithCompilers(collidingCompiler{}), engine.WithPasses(tt.passes(t, cancel)...))
			require.NoError(t, err)

			res, err := eng.RunDocument(ctx, danglingDocument(), engine.RunOptions{})

			require.ErrorIs(t, err, tt.want)
			assert.Nil(t, res)
		})
	}
}
