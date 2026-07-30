package engine_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

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

func TestEngine_RunSniffError(t *testing.T) {
	t.Parallel()
	eng, err := engine.New()
	require.NoError(t, err)
	// Swagger 2.0 sniffs to a recognized-but-unsupported error, which Run wraps.
	_, err = eng.Run(t.Context(), writeSpec(t, "swagger: \"2.0\"\n"), engine.RunOptions{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "engine: sniff")
}

func TestEngine_RunLookupMiss(t *testing.T) {
	t.Parallel()
	// NewWith() with zero compilers is load-bearing here: it is the only way to
	// reach an engine with no compiler registered for the openapi 3.1 the spec
	// sniffs to. Don't add a len(fronts) == 0 precondition to NewWith — doing so
	// would make this branch unreachable.
	eng, err := engine.NewWith()
	require.NoError(t, err)
	_, err = eng.Run(t.Context(), writeSpec(t, testspec.Tiny), engine.RunOptions{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no compiler registered for format")
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
