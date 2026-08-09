package engine_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/engine"
	"github.com/dexpace/morphic/internal/testspec"
	"github.com/dexpace/morphic/ir"
)

func writeSpec(t *testing.T, contents string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "spec.yaml")
	require.NoError(t, os.WriteFile(path, []byte(contents), 0o644))
	return path
}

func TestEngine_RunEndToEnd(t *testing.T) {
	t.Parallel()
	eng, err := engine.New()
	require.NoError(t, err)
	res, err := eng.Run(t.Context(), writeSpec(t, testspec.Tiny), engine.RunOptions{})
	require.NoError(t, err)
	require.NotNil(t, res.Document)
	assert.Equal(t, "Tiny", res.Document.Name)
	assert.Equal(t, "3.1", res.Format.Version)
	for _, d := range res.Diagnostics {
		assert.NotEqual(t, ir.SeverityError, d.Severity, "diag: %+v", d)
	}
}

// annotatedSubtypeSpec is a discriminator hierarchy whose subtype writes a
// description beside the `$ref` that names its base — legal OpenAPI 3.1, and the
// shape where the compiler's lowering and the validate pass have to agree about
// what a composition parent is.
const annotatedSubtypeSpec = `openapi: 3.1.0
info: {title: Pets, version: "1"}
paths: {}
components:
  schemas:
    Pet:
      type: object
      required: [petType]
      properties: {petType: {type: string}}
      discriminator:
        propertyName: petType
        mapping: {cat: '#/components/schemas/Cat'}
    Cat:
      allOf:
        - $ref: '#/components/schemas/Pet'
          description: the base as this subtype narrows it
      type: object
      properties: {meow: {type: boolean}}
`

// TestEngine_AnnotatedSubtypeValidatesClean drives the compiler and the validate
// pass together, which is the only place their disagreement shows. Keeping the
// `$ref` branch's siblings gives the branch a node of its own, so the subtype's
// Base names an alias over the discriminated base rather than the base itself —
// and a subtype check comparing one hop reported the mapping as naming a
// non-variant, an error on a document with nothing wrong with it.
//
// Neither side's own suite can see this: the compiler corpus never runs the
// pass, and the pass's tests build documents by hand.
func TestEngine_AnnotatedSubtypeValidatesClean(t *testing.T) {
	t.Parallel()
	eng, err := engine.New()
	require.NoError(t, err)
	res, err := eng.Run(t.Context(), writeSpec(t, annotatedSubtypeSpec), engine.RunOptions{})
	require.NoError(t, err)
	require.NotNil(t, res.Document)

	cat, ok := res.Document.Types["t/openapi/components/schemas/Cat"].(*ir.Model)
	require.True(t, ok)
	require.NotNil(t, cat.Base)
	assert.Equal(t, ir.TypeID("t/anon/components/schemas/Cat/allOf/0"), cat.Base.Target,
		"the annotated branch owns the node the subtype composes")
	for _, d := range res.Diagnostics {
		assert.NotEqual(t, ir.SeverityError, d.Severity, "diag: %+v", d)
	}
	assert.False(t, hasDiagCode(res.Diagnostics, "pass/discriminator-missing-variant"),
		"the mapping names a genuine subtype, whatever its Base points through")
}

func TestEngine_RunMissingFile(t *testing.T) {
	t.Parallel()
	eng, err := engine.New()
	require.NoError(t, err)
	_, err = eng.Run(t.Context(), filepath.Join(t.TempDir(), "absent.yaml"), engine.RunOptions{})
	require.Error(t, err)
}

// TestEngine_RunSniffProblemsAreDiagnostics covers every way a source can defeat
// the sniff step. None of them is an I/O failure or a programmer error, so none
// may leave Run as a Go error: a caller that maps Go errors to "you invoked me
// wrong" — which the CLI does — would report a spec it read and understood well
// enough to name the problem in as a misuse of itself.
func TestEngine_RunSniffProblemsAreDiagnostics(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, spec, code string
	}{
		{"swagger 2.0", "swagger: \"2.0\"\n", "engine/unsupported-format"},
		{"unrecognized", "hello: world\n", "engine/unrecognized-format"},
		{"undecodable", "openapi: [unterminated\n", "engine/undecodable-source"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			eng, err := engine.New()
			require.NoError(t, err)

			res, err := eng.Run(t.Context(), writeSpec(t, tt.spec), engine.RunOptions{})

			require.NoError(t, err, "a spec problem is not a Go error")
			require.NotNil(t, res)
			assert.Nil(t, res.Document, "nothing was lowered")
			require.Len(t, res.Diagnostics, 1)
			assert.Equal(t, tt.code, res.Diagnostics[0].Code)
			assert.Equal(t, ir.SeverityError, res.Diagnostics[0].Severity)
			assert.Equal(t, ir.NoSource, res.Diagnostics[0].Provenance.Source,
				"the engine read a file it never lowered, so it can index no source table")
			assert.Equal(t, compilers.SourceFormat{}, res.Format, "no format was determined")
		})
	}
}

func TestEngine_RunLookupMiss(t *testing.T) {
	t.Parallel()
	// NewWith() with zero compilers is load-bearing here: it is the only way to
	// reach an engine with no compiler registered for the openapi 3.1 the spec
	// sniffs to. Don't add a len(fronts) == 0 precondition to NewWith — doing so
	// would make this branch unreachable.
	eng, err := engine.NewWith()
	require.NoError(t, err)

	res, err := eng.Run(t.Context(), writeSpec(t, testspec.Tiny), engine.RunOptions{})

	require.NoError(t, err, "a spec no compiler claims is not a Go error")
	require.NotNil(t, res)
	assert.Nil(t, res.Document)
	require.Len(t, res.Diagnostics, 1)
	assert.Equal(t, "engine/no-compiler-for-format", res.Diagnostics[0].Code)
	assert.Contains(t, res.Diagnostics[0].Message, "openapi@3.1")
	assert.Equal(t, compilers.SourceFormat{Name: "openapi", Version: "3.1"}, res.Format,
		"the format that found no compiler is still the one the source declared")
}

// collidingCompiler claims a single fixed format. Two of them registered
// together make the second Register call fail, driving NewWith's error path.
type collidingCompiler struct{}

func (collidingCompiler) Formats() []compilers.SourceFormat {
	return []compilers.SourceFormat{{Name: "openapi", Version: "3.1"}}
}

func (collidingCompiler) Compile(context.Context, []compilers.Source, compilers.Options) (*ir.Document, []ir.Diagnostic, error) {
	return nil, nil, nil
}

func TestNewWith_RegisterError(t *testing.T) {
	t.Parallel()
	eng, err := engine.NewWith(collidingCompiler{}, collidingCompiler{})
	require.Error(t, err)
	assert.Nil(t, eng)
	assert.Contains(t, err.Error(), "engine: register compiler")
}

// errCompiler claims openapi 3.1 and always fails Compile, driving Run's
// parse-error branch.
type errCompiler struct{}

func (errCompiler) Formats() []compilers.SourceFormat {
	return []compilers.SourceFormat{{Name: "openapi", Version: "3.1"}}
}

func (errCompiler) Compile(context.Context, []compilers.Source, compilers.Options) (*ir.Document, []ir.Diagnostic, error) {
	return nil, nil, assert.AnError
}

func TestEngine_RunParseError(t *testing.T) {
	t.Parallel()
	eng, err := engine.NewWith(errCompiler{})
	require.NoError(t, err)
	_, err = eng.Run(t.Context(), writeSpec(t, testspec.Tiny), engine.RunOptions{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "engine: parse")
}

// nilDocCompiler claims openapi 3.1 and returns a nil Document with no error —
// a legal outcome that must skip the validate pass and surface a nil Document.
//
// It is not a stand-in for a compiler whose two diagnostic channels disagree:
// a nil Document short-circuits the validate step entirely, so this stub never
// reaches the code that folds the two together. splitDiagCompiler is what
// covers that.
type nilDocCompiler struct{}

func (nilDocCompiler) Formats() []compilers.SourceFormat {
	return []compilers.SourceFormat{{Name: "openapi", Version: "3.1"}}
}

func (nilDocCompiler) Compile(context.Context, []compilers.Source, compilers.Options) (*ir.Document, []ir.Diagnostic, error) {
	return nil, []ir.Diagnostic{{Code: "x/none"}}, nil
}

func TestEngine_RunNilDocument(t *testing.T) {
	t.Parallel()
	eng, err := engine.NewWith(nilDocCompiler{})
	require.NoError(t, err)
	// SkipValidate is false, but a nil Document must still short-circuit the pass.
	res, err := eng.Run(t.Context(), writeSpec(t, testspec.Tiny), engine.RunOptions{})
	require.NoError(t, err)
	assert.Nil(t, res.Document)
	assert.True(t, hasDiagCode(res.Diagnostics, "x/none"))
}

// splitDiagCompiler claims openapi 3.1 and lowers to a non-nil Document whose
// stored and returned diagnostics are set independently, so one finding can be
// put on either channel alone.
//
// All three splits the test below builds are conforming compilers: the
// compilers.Compiler contract names the returned slice as what a compiler
// reports through and leaves storing the same values on the document optional.
// The one compiler in the tree happens to do both, which is why nothing else
// here notices when the engine keeps only one of the two lists.
type splitDiagCompiler struct{ stored, returned []ir.Diagnostic }

func (splitDiagCompiler) Formats() []compilers.SourceFormat {
	return []compilers.SourceFormat{{Name: "openapi", Version: "3.1"}}
}

func (c splitDiagCompiler) Compile(context.Context, []compilers.Source, compilers.Options) (*ir.Document, []ir.Diagnostic, error) {
	// A fresh document and fresh slices per call: Run writes the merged list onto
	// the document, so shared ones would carry one run's result into the next.
	return &ir.Document{
		IRVersion:   "1.0.0",
		Types:       ir.TypeRegistry{},
		Diagnostics: slices.Clone(c.stored),
	}, slices.Clone(c.returned), nil
}

// TestEngine_RunKeepsDiagnosticsFromEitherChannel pins that neither diagnostic
// channel is dropped in favour of the other. Three of these six rows lost a
// finding before this was fixed — an error-severity one, in silence, on a
// different combination of channel and validate mode each time.
//
// The returned-only row with the validate pass enabled — the default — is the
// worst of them, and the reason the modes are a loop rather than a single case.
// Assigning Document.Diagnostics over the returned list emptied the Result the
// CLI gates its exit code on, so turning validation on *removed* findings and
// the tool exited 0 on a spec its compiler had refused outright.
func TestEngine_RunKeepsDiagnosticsFromEitherChannel(t *testing.T) {
	t.Parallel()
	want := ir.Diagnostic{
		Severity: ir.SeverityError,
		Code:     "stub/spec-problem",
		Message:  "the compiler reported this",
	}
	fronts := []struct {
		name  string
		front splitDiagCompiler
	}{
		{"mirrored", splitDiagCompiler{stored: []ir.Diagnostic{want}, returned: []ir.Diagnostic{want}}},
		{"returned only", splitDiagCompiler{returned: []ir.Diagnostic{want}}},
		{"stored only", splitDiagCompiler{stored: []ir.Diagnostic{want}}},
	}
	for _, tt := range fronts {
		for _, skipValidate := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/skip-validate=%v", tt.name, skipValidate), func(t *testing.T) {
				t.Parallel()
				eng, err := engine.NewWith(tt.front)
				require.NoError(t, err)

				res, err := eng.Run(t.Context(), writeSpec(t, testspec.Tiny),
					engine.RunOptions{SkipValidate: skipValidate})

				require.NoError(t, err)
				require.NotNil(t, res.Document,
					"a non-nil document is the whole point: a nil one skips the fold under test")
				assert.Empty(t, cmp.Diff([]ir.Diagnostic{want}, res.Diagnostics),
					"Result.Diagnostics is what the CLI renders and gates its exit code on")
				assert.Empty(t, cmp.Diff([]ir.Diagnostic{want}, res.Document.Diagnostics),
					"Document.Diagnostics is what the persisted IR JSON carries")
			})
		}
	}
}

// danglingCompiler is a stub that always lowers to a Document containing a
// dangling type reference, so the validate pass — if it runs — reports
// ir/dangling-type-ref. It claims the openapi 3.1 format so a tiny 3.1 spec
// sniffs to it.
type danglingCompiler struct{}

func (danglingCompiler) Formats() []compilers.SourceFormat {
	return []compilers.SourceFormat{{Name: "openapi", Version: "3.1"}}
}

func (danglingCompiler) Compile(_ context.Context, _ []compilers.Source, _ compilers.Options) (*ir.Document, []ir.Diagnostic, error) {
	doc := &ir.Document{
		Name:  "Dangling",
		Types: ir.TypeRegistry{},
		Services: []ir.Service{{
			ID: "s/x",
			Groups: []ir.OperationGroup{{
				Operations: []ir.Operation{{
					ID:     "op/x",
					Errors: []ir.ErrorCase{{Type: ir.TypeRef{Target: "t/missing"}}},
				}},
			}},
		}},
	}
	return doc, nil, nil
}

func hasDiagCode(diags []ir.Diagnostic, code string) bool {
	for _, d := range diags {
		if d.Code == code {
			return true
		}
	}
	return false
}

func TestEngine_ValidateRuns(t *testing.T) {
	t.Parallel()
	// A stub compiler yields a Document with a dangling type ref. The validate
	// pass must surface it when enabled and stay silent when skipped — so removing
	// the pass.Validate call from Run would break this test.
	eng, err := engine.NewWith(danglingCompiler{})
	require.NoError(t, err)
	path := writeSpec(t, testspec.Tiny)

	withPass, err := eng.Run(t.Context(), path, engine.RunOptions{})
	require.NoError(t, err)
	assert.True(t, hasDiagCode(withPass.Diagnostics, "ir/dangling-type-ref"),
		"validate pass reports the dangling ref when enabled")

	withoutPass, err := eng.Run(t.Context(), path, engine.RunOptions{SkipValidate: true})
	require.NoError(t, err)
	assert.False(t, hasDiagCode(withoutPass.Diagnostics, "ir/dangling-type-ref"),
		"skipping validation suppresses the diagnostic")
}
