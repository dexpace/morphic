package engine_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi"
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
			res, err := eng.Run(t.Context(), writeNamed(t, tc.file, tc.src), engine.RunOptions{})

			require.NoError(t, err, "another format's bytes are not a Go error")
			require.NotNil(t, res)
			assert.Nil(t, res.Document)
			require.Len(t, res.Diagnostics, 1)
			assert.Equal(t, "engine/unrecognized-format", res.Diagnostics[0].Code)
			assert.NotContains(t, res.Diagnostics[0].Message, "yaml:",
				"a parser's complaint is not an answer: none of these declares an OpenAPI key")
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
	assert.Equal(t, res.Document.Sources, res.Sources, "the result carries the document's own table")
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

// TestEngine_RunDetectionProblemsAreDiagnostics covers every way a source can
// defeat detection. None of them is an I/O failure or a programmer error, so
// none may leave Run as a Go error: a caller that maps Go errors to "you invoked
// me wrong" — which the CLI does — would report a spec it read and understood
// well enough to name the problem in as a misuse of itself.
//
// The three rows are three different answers. A Swagger document is recognized
// and unserved, so the format it declared survives into the Result. Bytes that
// declare no key at all are nobody's, and no compiler has anything to say. Bytes
// that declare an OpenAPI key and will not parse are the OpenAPI compiler's own,
// and it — not the engine, which parses nothing — reports the parse error.
func TestEngine_RunDetectionProblemsAreDiagnostics(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, spec, code string
		wantFormat       compilers.SourceFormat
	}{
		{"recognized but unserved", "swagger: \"2.0\"\ninfo: {}\n",
			"engine/no-compiler-for-format", compilers.SourceFormat{Name: "swagger", Version: "2.0"}},
		{"unrecognized", "hello: world\n", "engine/unrecognized-format", compilers.SourceFormat{}},
		{"undecodable", "openapi: 3.1.0\ninfo: [unterminated\n", "openapi/undecodable-source", compilers.SourceFormat{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			eng, err := engine.New()
			require.NoError(t, err)

			spec := writeSpec(t, tt.spec)
			res, err := eng.Run(t.Context(), spec, engine.RunOptions{})

			require.NoError(t, err, "a spec problem is not a Go error")
			require.NotNil(t, res)
			assert.Nil(t, res.Document, "nothing was lowered")
			require.Len(t, res.Diagnostics, 1)
			assert.Equal(t, tt.code, res.Diagnostics[0].Code)
			assert.Equal(t, ir.SeverityError, res.Diagnostics[0].Severity)
			assert.Equal(t, ir.Provenance{Source: 0}, res.Diagnostics[0].Provenance,
				"the finding is about the file as a whole, and names it")
			assert.Equal(t, []ir.SourceInfo{{Path: spec}}, res.Sources,
				"nothing was lowered, and the table still names the file")
			assert.Equal(t, tt.wantFormat, res.Format)
		})
	}
}

// TestEngine_RunResultSourcesOnARefusedCompile pins the case #388 named and
// #527 reproduced: a compile that refuses returns no Document, so before this
// fix the run carried no source table at all and a refusal's own diagnostics
// could not be resolved to a file. The compiler is the real one — a stub would
// only prove the plumbing, not that an actual refusal reaches it.
func TestEngine_RunResultSourcesOnARefusedCompile(t *testing.T) {
	t.Parallel()
	const cyclic = `openapi: 3.1.0
info: {title: t, version: '1'}
paths: {}
components: {schemas: {A: {$ref: '#/components/schemas/A'}}}
`
	eng, err := engine.New()
	require.NoError(t, err)
	spec := writeSpec(t, cyclic)

	res, err := eng.Run(t.Context(), spec, engine.RunOptions{})

	require.NoError(t, err, "a cyclic spec is a document problem, not a Go error")
	require.NotNil(t, res)
	assert.Nil(t, res.Document, "the cycle refuses the compile")
	assert.Equal(t, []ir.SourceInfo{{Path: spec}}, res.Sources,
		"the compiler's own table, not the document's — there is no document")
	require.NotEmpty(t, res.Diagnostics)
	for _, d := range res.Diagnostics {
		assert.Equal(t, 0, d.Provenance.Source, "the cycle is in the one source the run read: %+v", d)
	}
}

// TestEngine_RunResultSourcesOnARefusedOverlay pins the gap #388 left: it
// assumed the engine already knows every path a refusal could name, which
// holds for the spec but not for an overlay, since that name reaches the
// compiler only through RunOptions.CompilerOptions (or FormatOptions). The
// compiler is what reports it, through SourceTable.
func TestEngine_RunResultSourcesOnARefusedOverlay(t *testing.T) {
	t.Parallel()
	// The same recursive-anchor shape TestCompiler_SourceTableOnARefusal pins at
	// the compiler's own level: the overlay library's clone of the update value
	// has no base case for an anchor that aliases itself.
	const anchorCycle = "overlay: 1.0.0\ninfo: {title: o, version: \"1\"}\n" +
		"actions:\n  - target: $.info\n    update: {p: &a [*a]}\n"

	eng, err := engine.New()
	require.NoError(t, err)
	spec := writeSpec(t, testspec.Tiny)
	overlay := writeNamed(t, "overlay.yaml", anchorCycle)

	res, err := eng.Run(t.Context(), spec, engine.RunOptions{
		CompilerOptions: map[string]string{"overlay": overlay},
	})

	require.NoError(t, err, "a cyclic overlay is a document problem, not a Go error")
	require.NotNil(t, res)
	assert.Nil(t, res.Document, "the overlay's own cycle refuses the compile")
	require.Len(t, res.Sources, 2)
	assert.Equal(t, spec, res.Sources[0].Path)
	assert.Equal(t, overlay, res.Sources[1].Path)
	require.NotEmpty(t, res.Diagnostics)
	named := false
	for _, d := range res.Diagnostics {
		if d.Provenance.Source == 1 {
			named = true
		}
	}
	assert.True(t, named, "the refusal names the overlay, source 1: %+v", res.Diagnostics)
}

// TestNewWith_RefusesAnEmptyCompilerSet pins that an engine which can compile
// nothing cannot be built. There is no way to add a compiler to a built engine,
// so the alternative is one that reports every source it is handed as
// unrecognized — blaming the document for a misconfiguration of the caller.
//
// An earlier note here asked that this precondition not be added, on the grounds
// that an empty engine was the only way to reach Run's nothing-recognized
// branch. Detection belongs to the compilers now, so an ordinary source none of
// them claims reaches that branch with a full registry; the tests above do it.
func TestNewWith_RefusesAnEmptyCompilerSet(t *testing.T) {
	t.Parallel()

	eng, err := engine.NewWith()

	require.Error(t, err)
	assert.Nil(t, eng, "nothing usable comes back from a refused construction")
	assert.Contains(t, err.Error(), "no compilers")
}

// TestEngine_RunEmptySource pins that an empty file is nobody's spec, without a
// compiler having to be asked about zero bytes.
func TestEngine_RunEmptySource(t *testing.T) {
	t.Parallel()
	eng, err := engine.New()
	require.NoError(t, err)
	res, err := eng.Run(t.Context(), writeSpec(t, ""), engine.RunOptions{})

	require.NoError(t, err, "an empty file is a spec problem, not a Go error")
	require.NotNil(t, res)
	assert.Nil(t, res.Document)
	require.Len(t, res.Diagnostics, 1)
	assert.Equal(t, "engine/unrecognized-format", res.Diagnostics[0].Code)
}

// stubFront answers the two contract questions every stub in this file answers
// the same way: it claims openapi 3.1, recognizes anything as openapi 3.1, and
// takes no options. Embedding it leaves each stub holding only the Compile
// behaviour it exists to model.
type stubFront struct{}

func (stubFront) Formats() []compilers.SourceFormat {
	return []compilers.SourceFormat{{Name: "openapi", Version: "3.1"}}
}

func (stubFront) Detect(compilers.Source, compilers.Options) (compilers.Recognition, []ir.Diagnostic, bool) {
	return compilers.Recognition{Format: compilers.SourceFormat{Name: "openapi", Version: "3.1"}}, nil, true
}

func (stubFront) DecodeOptions(compilers.OptionSet) (any, error) { return nil, nil }

func (stubFront) SourceTable([]compilers.Source, compilers.Options) []ir.SourceInfo { return nil }

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
	assert.Contains(t, err.Error(), "engine: register compiler 1")
}

// TestNewWith_NilCompiler passes the nil second so the reported position proves
// the index is the argument's own and not a constant. Reaching the assertions at
// all is the point: a nil compiler used to segfault inside the registry, which
// is a panic escaping two packages rather than an error the caller can handle.
func TestNewWith_NilCompiler(t *testing.T) {
	t.Parallel()
	eng, err := engine.NewWith(collidingCompiler{}, nil)
	require.Error(t, err)
	assert.Nil(t, eng)
	assert.Contains(t, err.Error(), "engine: register compiler 1")
	assert.Contains(t, err.Error(), "nil compiler")
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
//
// It is not a stand-in for a compiler whose two diagnostic channels disagree:
// a nil Document short-circuits the validate step entirely, so this stub never
// reaches the code that folds the two together. splitDiagCompiler is what
// covers that.
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

// splitDiagCompiler claims openapi 3.1 and lowers to a non-nil Document whose
// stored and returned diagnostics are set independently, so one finding can be
// put on either channel alone.
//
// All three splits the test below builds are conforming compilers: the
// compilers.Compiler contract names the returned slice as what a compiler
// reports through and leaves storing the same values on the document optional.
// The one compiler in the tree happens to do both, which is why nothing else
// here notices when the engine keeps only one of the two lists.
type splitDiagCompiler struct {
	stubFront
	stored, returned []ir.Diagnostic
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
type danglingCompiler struct{ stubFront }

func (danglingCompiler) Compile(_ context.Context, _ []compilers.Source, _ compilers.Options) (*ir.Document, []ir.Diagnostic, error) {
	doc := &ir.Document{
		Name:  "Dangling",
		Types: ir.TypeRegistry{},
		Services: []ir.Service{{
			ID: "s/x",
			Groups: []ir.OperationGroup{{
				Operations: []ir.Operation{{
					ID: "op/x",
					Errors: []ir.ErrorCase{{
						Payload: &ir.Payload{Contents: []ir.Content{{Type: ir.TypeRef{Target: "t/missing"}}}},
					}},
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

func (*smithyCompiler) Detect(src compilers.Source, _ compilers.Options) (compilers.Recognition, []ir.Diagnostic, bool) {
	if !strings.HasPrefix(string(src.Data), "$version:") {
		return compilers.Recognition{}, nil, false
	}
	return compilers.Recognition{
		Format: compilers.SourceFormat{Name: "smithy", Version: "2.0"},
		Parsed: smithyParse{},
	}, nil, true
}

// smithyParse stands in for what a compiler parses while recognizing a source.
// Its only job is to be a type of this compiler's own, so a test can assert
// that it reaches this compiler's Compile and no other's.
type smithyParse struct{}

func (s *smithyCompiler) DecodeOptions(set compilers.OptionSet) (any, error) {
	if _, ok := set.Settings["boom"]; ok {
		return nil, errors.New("boom is not an option")
	}
	s.options = set
	return set.Settings["shape"], nil
}

func (*smithyCompiler) SourceTable([]compilers.Source, compilers.Options) []ir.SourceInfo { return nil }

func (*smithyCompiler) Compile(_ context.Context, sources []compilers.Source, opts compilers.Options) (*ir.Document, []ir.Diagnostic, error) {
	name, _ := opts.FormatOptions.(string)
	doc := &ir.Document{Name: name, Types: ir.TypeRegistry{}}
	if len(sources) == 1 {
		if _, ok := sources[0].Parsed.(smithyParse); ok {
			doc.Version = "parse-arrived"
		}
	}
	return doc, nil, nil
}

// TestEngine_RunHandsDetectionsParseToCompile pins the engine's half of the
// seam that stops a source being parsed twice: what a compiler made of a
// source while recognizing it arrives on the Source its own Compile is given.
// The compiler records that it arrived, since nothing else about the run can
// show it.
func TestEngine_RunHandsDetectionsParseToCompile(t *testing.T) {
	t.Parallel()
	eng, err := engine.NewWith(&smithyCompiler{})
	require.NoError(t, err)
	path := writeNamed(t, "svc.smithy", "$version: \"2.0\"\n")

	res, err := eng.Run(t.Context(), path, engine.RunOptions{SkipValidate: true})

	require.NoError(t, err)
	require.NotNil(t, res.Document)
	assert.Equal(t, "parse-arrived", res.Document.Version,
		"the compile was handed what detection parsed")
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

// TestEngine_RunRefusesTwoOptionChannelsWhateverTheSource pins that the
// refusal does not wait on detection: a source no compiler recognizes is still
// a run configured two ways.
func TestEngine_RunRefusesTwoOptionChannelsWhateverTheSource(t *testing.T) {
	t.Parallel()
	eng, err := engine.NewWith(&smithyCompiler{})
	require.NoError(t, err)

	_, err = eng.Run(t.Context(), writeNamed(t, "notes.txt", "nobody's format\n"), engine.RunOptions{
		FormatOptions:   "programmatic",
		CompilerOptions: map[string]string{"shape": "Widget"},
	})
	require.EqualError(t, err, "engine: set FormatOptions or CompilerOptions, not both")
}

// TestEngine_RunHoldsDetectionToTheCallersByteBudget pins the budget reaching
// detection through the engine: a spec past it is refused as past the budget,
// not reported as a format nothing recognizes. The compile would refuse it in
// the same words, so the format is what shows which one did: detection read
// nothing and so named none.
func TestEngine_RunHoldsDetectionToTheCallersByteBudget(t *testing.T) {
	t.Parallel()
	eng, err := engine.New()
	require.NoError(t, err)
	spec := writeSpec(t, "openapi: 3.1.0\ninfo: {title: T, version: \"1\"}\npaths: {}\n")

	res, err := eng.Run(t.Context(), spec, engine.RunOptions{
		FormatOptions: openapi.Options{Limits: openapi.Limits{MaxSourceBytes: 8}},
	})

	require.NoError(t, err, "a source past the budget is a spec problem, not a Go error")
	assert.Nil(t, res.Document)
	require.Len(t, res.Diagnostics, 1, "%+v", res.Diagnostics)
	assert.Equal(t, "openapi/budget-exceeded", res.Diagnostics[0].Code)
	assert.Contains(t, res.Diagnostics[0].Message, "past the 8-byte budget")
	assert.Equal(t, compilers.SourceFormat{}, res.Format, "refused before anything read the spec")
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
		}, "engine: set FormatOptions or CompilerOptions, not both"},
		{"undecodable setting", engine.RunOptions{
			CompilerOptions: map[string]string{"boom": "yes"},
		}, "options for smithy@2.0: decode: boom is not an option"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			eng, err := engine.NewWith(&smithyCompiler{})
			require.NoError(t, err)
			_, err = eng.Run(t.Context(), writeNamed(t, "model.smithy", "$version: \"2\"\n"), tc.opts)
			require.Error(t, err)
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

// TestEngine_RunNamesWhatItCanCompile pins that a source the engine will not
// take is told what it would have taken. Naming the failure without naming the
// alternative leaves the next step to guesswork, and this is the whole of what
// separates "morphic cannot read your file" from "morphic reads OpenAPI 3.x".
func TestEngine_RunNamesWhatItCanCompile(t *testing.T) {
	t.Parallel()
	tests := []struct{ name, spec string }{
		{"unrecognized", "hello: world\n"},
		{"recognized but unserved", "swagger: \"2.0\"\ninfo: {}\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			eng, err := engine.New()
			require.NoError(t, err)

			res, err := eng.Run(t.Context(), writeSpec(t, tt.spec), engine.RunOptions{})

			require.NoError(t, err)
			require.Len(t, res.Diagnostics, 1)
			for _, format := range []string{"openapi@3.0", "openapi@3.1", "openapi@3.2"} {
				assert.Contains(t, res.Diagnostics[0].Message, format,
					"the refusal must name the formats this build serves")
			}
		})
	}
}

// TestEngine_RunOnAnUnbuiltEngine pins that an Engine which never went through a
// constructor reports the caller's mistake instead of crashing on the nil
// registry it carries. Run reserves its Go error for programmer errors and this
// is one; a panic out of a library is not a report, and the caller cannot tell
// it from a bug in the compiler it was calling.
func TestEngine_RunOnAnUnbuiltEngine(t *testing.T) {
	t.Parallel()
	spec := writeSpec(t, testspec.Tiny)
	tests := []struct {
		name string
		eng  *engine.Engine
	}{
		{"zero value", &engine.Engine{}},
		{"nil receiver", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			res, err := tt.eng.Run(t.Context(), spec, engine.RunOptions{})

			require.Error(t, err, "an unbuilt engine must not panic")
			assert.Nil(t, res)
			assert.Contains(t, err.Error(), "uninitialized")
		})
	}
}

// TestEngine_RunDiagnosticsAreOneLineEach pins the rendering contract the README
// states — one diagnostic per line — across the ways a source can fail, rather
// than at the one site where a multi-line message was first noticed.
//
// A message carrying a newline splits one report into several, and every line
// after the first has no severity, code or location: a reader takes it for
// another finding, and a wrapper parsing stderr takes it for a malformed one.
// Both libraries reported through here write multi-line errors, so the sites
// that embed one are where this keeps breaking; the inputs below reach the
// detection, parse, overlay and validation paths in turn.
func TestEngine_RunDiagnosticsAreOneLineEach(t *testing.T) {
	t.Parallel()
	const okSpec = "openapi: 3.1.0\ninfo: {title: T, version: \"1\"}\npaths: {}\n"
	tests := []struct {
		name, spec string
		// withOverlay applies an overlay that fails validation on two counts,
		// which is the input that made the overlay reporter write two lines.
		withOverlay bool
	}{
		{"unparseable", "openapi: 3.1.0\ninfo: [unterminated\n", false},
		{"unrecognized", "hello: world\n", false},
		{"unsupported version", "openapi: 4.0.0\ninfo: {title: T, version: \"1\"}\npaths: {}\n", false},
		{"invalid overlay", okSpec, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			spec := filepath.Join(dir, "spec.yaml")
			require.NoError(t, os.WriteFile(spec, []byte(tt.spec), 0o600))
			var settings map[string]string
			if tt.withOverlay {
				overlay := filepath.Join(dir, "overlay.yaml")
				require.NoError(t, os.WriteFile(overlay,
					[]byte("overlay: 1.0.0\ninfo: {title: O}\nactions: []\n"), 0o600))
				settings = map[string]string{"overlay": overlay}
			}
			eng, err := engine.New()
			require.NoError(t, err)

			res, err := eng.Run(t.Context(), spec, engine.RunOptions{CompilerOptions: settings})

			require.NoError(t, err)
			require.NotNil(t, res)
			require.NotEmpty(t, res.Diagnostics, "the case must produce a diagnostic to be worth checking")
			for _, d := range res.Diagnostics {
				assert.NotContains(t, d.Message, "\n",
					"one diagnostic is one line: %s / %s", d.Code, d.Message)
			}
		})
	}
}

// TestEngine_RunReadsAVersionKeyPastTheSniffCap drives the whole pipeline over
// the shape that motivated the detection change: a JSON document whose version
// key sits behind an object too large to read on the fast path. Nothing else
// reaches that path from the outside — the largest spec in the corpus is a few
// kilobytes — so without this the scan ships covered only by tests that call
// detection directly, and a break between Detect and a compiled document would
// have nothing to fail.
func TestEngine_RunReadsAVersionKeyPastTheSniffCap(t *testing.T) {
	t.Parallel()
	var b strings.Builder
	b.WriteString(`{"info":{"title":"T","version":"1"},"paths":{},"components":{"schemas":{`)
	for i := 0; b.Len() <= 64<<10; i++ {
		if i > 0 {
			b.WriteByte(',')
		}
		fmt.Fprintf(&b, `"S%d":{"type":"object","description":"a schema"}`, i)
	}
	b.WriteString(`}},"openapi":"3.1.0"}`)
	require.Greater(t, b.Len(), 64<<10, "the version key must sit past the cap to test it")

	eng, err := engine.New()
	require.NoError(t, err)
	res, err := eng.Run(t.Context(), writeNamed(t, "spec3.json", b.String()), engine.RunOptions{})
	require.NoError(t, err)
	assert.Equal(t, compilers.SourceFormat{Name: "openapi", Version: "3.1"}, res.Format)
	require.NotNil(t, res.Document, "a document that declares its version last still compiles")
}
