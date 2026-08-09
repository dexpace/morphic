package engine_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
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
	return writeNamed(t, "spec.yaml", contents)
}

func writeNamed(t *testing.T, name, contents string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	require.NoError(t, os.WriteFile(path, []byte(contents), 0o644))
	return path
}

// TestEngine_RunUnrecognizedFormat drives the input three of the five planned
// compilers take. None of it is YAML, and none of it is any concern of the YAML
// parser: what a user needs back is that no registered compiler claims these
// bytes, named in the vocabulary of spec formats.
//
// Running the parser first made the answer an accident of whether the bytes
// happened to parse — a one-line GraphQL document parses and a two-line one does
// not — and when they did not, the message quoted a decoder's complaint about an
// engine-internal type.
func TestEngine_RunUnrecognizedFormat(t *testing.T) {
	t.Parallel()
	cases := []struct{ name, file, src string }{
		{"protobuf", "svc.proto", "syntax = \"proto3\";\npackage demo;\nservice S { rpc Get (Q) returns (A); }\n"},
		{"typespec", "main.tsp", "import \"@typespec/http\";\nnamespace Demo;\nmodel Pet { name: string; }\n"},
		{"graphql", "schema.graphql", "type Query {\n  pet(id: ID!): Pet\n}\n"},
		{"graphql one line", "one.graphql", "type Query { a: String }\n"},
		{"yaml that is no spec", "junk.yaml", "hello: world\n"},
	}
	eng, err := engine.New()
	require.NoError(t, err)

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := eng.Run(t.Context(), writeNamed(t, tc.file, tc.src), engine.RunOptions{})
			require.Error(t, err)
			assert.Contains(t, err.Error(), "no compiler recognizes")
			assert.NotContains(t, err.Error(), "yaml:", "a parser's complaint is not an answer")
		})
	}
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

// TestEngine_RunRecognizedButUnsupported pins the other half of the detection
// answer: a Swagger 2.0 document is a spec, and the OpenAPI compiler says so
// while serving no such format. Naming it beats reporting the file as
// unrecognized, which is what a user would be told if recognition and support
// were the same question.
func TestEngine_RunRecognizedButUnsupported(t *testing.T) {
	t.Parallel()
	eng, err := engine.New()
	require.NoError(t, err)
	_, err = eng.Run(t.Context(), writeSpec(t, "swagger: \"2.0\"\ninfo: {}\n"), engine.RunOptions{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no compiler registered for format swagger@2.0")
}

func TestEngine_RunNothingRegistered(t *testing.T) {
	t.Parallel()
	// NewWith() with zero compilers is load-bearing here: it is the only way to
	// reach an engine that has nothing to ask about a source every registered
	// compiler would otherwise claim. Don't add a len(fronts) == 0 precondition
	// to NewWith — doing so would make this branch unreachable.
	eng, err := engine.NewWith()
	require.NoError(t, err)
	_, err = eng.Run(t.Context(), writeSpec(t, testspec.Tiny), engine.RunOptions{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no compiler recognizes")
}

// TestEngine_RunEmptySource pins that an empty file is nobody's spec, without a
// compiler having to be asked about zero bytes.
func TestEngine_RunEmptySource(t *testing.T) {
	t.Parallel()
	eng, err := engine.New()
	require.NoError(t, err)
	_, err = eng.Run(t.Context(), writeSpec(t, ""), engine.RunOptions{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no compiler recognizes")
}

// stubFront answers the two contract questions every stub in this file answers
// the same way: it claims openapi 3.1, recognizes anything as openapi 3.1, and
// takes no options. Embedding it leaves each stub holding only the Compile
// behaviour it exists to model.
type stubFront struct{}

func (stubFront) Formats() []compilers.SourceFormat {
	return []compilers.SourceFormat{{Name: "openapi", Version: "3.1"}}
}

func (stubFront) Detect(compilers.Source) (compilers.SourceFormat, bool) {
	return compilers.SourceFormat{Name: "openapi", Version: "3.1"}, true
}

func (stubFront) DecodeOptions(compilers.OptionSet) (any, error) { return nil, nil }

// collidingCompiler claims a single fixed format. Two of them registered
// together make the second Register call fail, driving NewWith's error path.
type collidingCompiler struct{ stubFront }

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
type errCompiler struct{ stubFront }

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
type nilDocCompiler struct{ stubFront }

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
type danglingCompiler struct{ stubFront }

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

// smithyCompiler is a compiler for a format the engine has never heard of. It
// recognizes its own input and names its own options, which is the whole of what
// registering a format takes.
type smithyCompiler struct{ options compilers.OptionSet }

func (*smithyCompiler) Formats() []compilers.SourceFormat {
	return []compilers.SourceFormat{{Name: "smithy", Version: "2.0"}}
}

func (*smithyCompiler) Detect(src compilers.Source) (compilers.SourceFormat, bool) {
	if !strings.HasPrefix(string(src.Data), "$version:") {
		return compilers.SourceFormat{}, false
	}
	return compilers.SourceFormat{Name: "smithy", Version: "2.0"}, true
}

func (s *smithyCompiler) DecodeOptions(set compilers.OptionSet) (any, error) {
	if _, ok := set.Settings["boom"]; ok {
		return nil, errors.New("boom is not an option")
	}
	s.options = set
	return set.Settings["shape"], nil
}

func (*smithyCompiler) Compile(_ context.Context, _ []compilers.Source, opts compilers.Options) (*ir.Document, []ir.Diagnostic, error) {
	name, _ := opts.FormatOptions.(string)
	return &ir.Document{Name: name, Types: ir.TypeRegistry{}}, nil, nil
}

// TestEngine_RunNewFormatNeedsNoEngineEdit is the acceptance criterion for a
// compiler-owned seam: a format the engine names nowhere is reached by
// registering its compiler and nothing else. Detection, the option vocabulary
// and the version grammar all belong to the compiler, so this file — and every
// other file under engine/ — mentions neither smithy nor its options.
func TestEngine_RunNewFormatNeedsNoEngineEdit(t *testing.T) {
	t.Parallel()
	eng, err := engine.NewWith(&smithyCompiler{})
	require.NoError(t, err)

	res, err := eng.Run(t.Context(), writeNamed(t, "model.smithy", "$version: \"2\"\nnamespace demo\n"),
		engine.RunOptions{CompilerOptions: map[string]string{"shape": "Widget"}})
	require.NoError(t, err)
	require.NotNil(t, res.Document)
	assert.Equal(t, compilers.SourceFormat{Name: "smithy", Version: "2.0"}, res.Format)
	assert.Equal(t, "Widget", res.Document.Name, "the setting reached the compiler that decoded it")
}

// TestEngine_RunCompilerOptionsAreLoadedByTheEngine pins the file half of the
// textual channel: a compiler does no file I/O, so a setting that names one is
// read through the reader the engine supplies.
func TestEngine_RunCompilerOptionsAreLoadedByTheEngine(t *testing.T) {
	t.Parallel()
	front := &smithyCompiler{}
	eng, err := engine.NewWith(front)
	require.NoError(t, err)
	spec := writeNamed(t, "model.smithy", "$version: \"2\"\n")

	_, err = eng.Run(t.Context(), spec, engine.RunOptions{
		CompilerOptions: map[string]string{"shape": "Widget"},
	})
	require.NoError(t, err)
	require.NotNil(t, front.options.ReadFile, "a compiler must be handed a reader it can use")

	got, err := front.options.ReadFile(spec)
	require.NoError(t, err)
	assert.Contains(t, string(got), "$version")
}

func TestEngine_RunOptionRefusals(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		opts    engine.RunOptions
		wantErr string
	}{
		{"both channels", engine.RunOptions{
			FormatOptions:   "programmatic",
			CompilerOptions: map[string]string{"shape": "Widget"},
		}, "set FormatOptions or CompilerOptions, not both"},
		{"undecodable setting", engine.RunOptions{
			CompilerOptions: map[string]string{"boom": "yes"},
		}, "boom is not an option"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			eng, err := engine.NewWith(&smithyCompiler{})
			require.NoError(t, err)
			_, err = eng.Run(t.Context(), writeNamed(t, "model.smithy", "$version: \"2\"\n"), tc.opts)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "engine: options for")
			assert.Contains(t, err.Error(), tc.wantErr)
		})
	}
}

// TestEngine_RunCompilerOptionsReachTheOpenAPICompiler asserts through the real
// compiler what the stubs assert through the seam: the same document compiles
// differently under a setting no engine code understands.
func TestEngine_RunCompilerOptionsReachTheOpenAPICompiler(t *testing.T) {
	t.Parallel()
	eng, err := engine.New()
	require.NoError(t, err)
	spec := writeSpec(t, `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /a/b:
    get: {operationId: ab, tags: [zoo], responses: {"200": {description: ok}}}
`)

	byTag, err := eng.Run(t.Context(), spec, engine.RunOptions{})
	require.NoError(t, err)
	byPath, err := eng.Run(t.Context(), spec, engine.RunOptions{
		CompilerOptions: map[string]string{"grouping": "path-prefix"},
	})
	require.NoError(t, err)

	assert.Equal(t, "zoo", byTag.Document.Services[0].Groups[0].Name.Source)
	assert.Equal(t, "a", byPath.Document.Services[0].Groups[0].Name.Source)
}
