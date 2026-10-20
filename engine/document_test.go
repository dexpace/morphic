package engine_test

import (
	"context"
	"encoding/json/v2"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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

// TestEngine_RunDocumentAgreesWithRun pins the use RunDocument exists for: a
// document Run compiled, persisted and read back, runs to the findings Run
// reported, each once, though the validate pass finds its own again.
func TestEngine_RunDocumentAgreesWithRun(t *testing.T) {
	t.Parallel()
	eng, err := engine.New()
	require.NoError(t, err)
	compiled, err := eng.Run(t.Context(), writeSpec(t, testspec.Tiny), engine.RunOptions{})
	require.NoError(t, err)
	require.NotNil(t, compiled.Document)

	res, err := eng.RunDocument(t.Context(), roundTrip(t, compiled.Document), engine.RunOptions{})

	require.NoError(t, err)
	assert.Empty(t, cmp.Diff(compiled.Diagnostics, res.Diagnostics))
	// An empty map round-trips as an absent one, which the encoding leaves out.
	assert.Empty(t, cmp.Diff(compiled.Document, res.Document, cmpopts.EquateEmpty()))
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
// none, compiler options no compiler would read, an unbuilt engine, and a
// cancelled context, which stops it before the first pass.
func TestEngine_RunDocumentRefusals(t *testing.T) {
	t.Parallel()
	built, err := engine.New()
	require.NoError(t, err)
	stale := danglingDocument()
	stale.IRVersion = "0.0.1"
	unstamped := danglingDocument()
	unstamped.IRVersion = ""
	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	tests := []struct {
		name string
		eng  *engine.Engine
		ctx  context.Context
		doc  *ir.Document
		opts engine.RunOptions
		want string
	}{
		{"no document", built, t.Context(), nil, engine.RunOptions{}, "no document"},
		{"another generation", built, t.Context(), stale, engine.RunOptions{}, `"0.0.1"`},
		{"no generation", built, t.Context(), unstamped, engine.RunOptions{}, `IR version ""`},
		{"format options", built, t.Context(), danglingDocument(),
			engine.RunOptions{FormatOptions: struct{}{}}, "compiles nothing"},
		{"compiler options", built, t.Context(), danglingDocument(),
			engine.RunOptions{CompilerOptions: map[string]string{"grouping": "tags"}}, "compiles nothing"},
		{"an unbuilt engine", &engine.Engine{}, t.Context(), danglingDocument(), engine.RunOptions{}, "uninitialized"},
		{"a cancelled context", built, cancelled, danglingDocument(), engine.RunOptions{}, "context canceled"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			res, err := tt.eng.RunDocument(tt.ctx, tt.doc, tt.opts)

			require.Error(t, err)
			assert.Nil(t, res)
			assert.Contains(t, err.Error(), tt.want)
		})
	}
}
