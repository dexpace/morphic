package engine_test

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi"
	"github.com/dexpace/morphic/engine"
	"github.com/dexpace/morphic/internal/testspec"
	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/ir/irtest"
	"github.com/dexpace/morphic/ir/irverify"
	"github.com/dexpace/morphic/pass"
)

// tamperer lowers with the OpenAPI compiler and then breaks the document it
// got, so the one thing wrong with what it returns is what the test broke.
type tamperer struct {
	*openapi.Compiler
	tamper func(*ir.Document)
}

func (c tamperer) Compile(ctx context.Context, sources []compilers.Source, opts compilers.Options) (*ir.Document, []ir.Diagnostic, error) {
	doc, diags, err := c.Compiler.Compile(ctx, sources, opts)
	if doc != nil {
		c.tamper(doc)
	}
	return doc, diags, err
}

// runOverACleanDocument is RunDocument over what the shipped compiler lowers the
// tiny spec to, a document that keeps every invariant, which is what a verified
// run needs to start from. keptDocument keeps the duties every run checks and
// not those, having no service.
var runOverACleanDocument = entry{"RunDocument", func(t *testing.T, eng *engine.Engine, opts engine.RunOptions) (*engine.Result, error) {
	t.Helper()
	plain, err := engineOf(t, openapi.New()).RunSources(t.Context(),
		[]compilers.Source{{Path: "spec.yaml", Data: []byte(testspec.Tiny)}}, engine.RunOptions{})
	require.NoError(t, err)
	return eng.RunDocument(t.Context(), plain.Document, opts)
}}

// verifiedEntries are the ways a verified run can start with a pass to hold.
var verifiedEntries = []entry{runFromAFile, runFromMemory, runOverACleanDocument}

// violationCodes returns the codes of vs, in order.
func violationCodes(vs []irverify.Violation) []string {
	codes := make([]string, 0, len(vs))
	for _, v := range vs {
		codes = append(codes, v.Code)
	}
	return codes
}

// requireVerifyError requires err to be a *VerifyError for stage, wrapping
// sentinel (none when nil), whose violations include each of codes.
func requireVerifyError(t *testing.T, err, sentinel error, stage string, codes ...string) {
	t.Helper()
	verr, ok := errors.AsType[*engine.VerifyError](err)
	require.True(t, ok, "expected a *VerifyError, got %v", err)
	assert.Contains(t, verr.Stage, stage)
	assert.NotContains(t, err.Error(), "\n", "an error is one line")
	if sentinel != nil {
		require.ErrorIs(t, err, sentinel)
		assert.True(t, strings.HasSuffix(err.Error(), sentinel.Error()), "the message ends with the contract it broke: %v", err)
	}
	for _, other := range []error{compilers.ErrContract, pass.ErrContract} {
		if !errors.Is(sentinel, other) {
			assert.NotErrorIs(t, err, other)
		}
	}
	for _, code := range codes {
		assert.Contains(t, violationCodes(verr.Violations), code)
	}
}

// brokenDocuments are the ways a compiler returns a document that keeps the
// duties the engine always holds it to and breaks the invariants irverify
// states, each by the one edit that breaks it.
var brokenDocuments = []struct {
	name    string
	breakIt func(*ir.Document)
	codes   []string
}{
	{"a duplicate operation ID", func(d *ir.Document) {
		group := &d.Services[0].Groups[0]
		group.Operations = append(slices.Clone(group.Operations), group.Operations[0])
	}, []string{"ir/duplicate-op-id"}},
	{"a registry key unequal to its node's ID", func(d *ir.Document) {
		d.Types = ir.TypeRegistry{"t/openapi/a": &ir.Scalar{ID: "t/openapi/b"}}
	}, []string{"ir/type-id-mismatch"}},
	{"a cased canonical name", func(d *ir.Document) {
		d.Services[0].Name.Canonical = "Ping"
	}, []string{"ir/naming-cased"}},
}

// TestEngine_RunLeavesStructuralBreachesToVerify records the outcome the
// compilers.Compiler docs state for the duties only irverify holds a document
// to: a run returns such a document unremarked, and a run asked to verify
// fails with the violations, naming the compile.
func TestEngine_RunLeavesStructuralBreachesToVerify(t *testing.T) {
	t.Parallel()
	for _, tt := range brokenDocuments {
		for _, via := range compileEntries {
			t.Run(tt.name+"/"+via.name, func(t *testing.T) {
				t.Parallel()
				eng := engineOf(t, tamperer{Compiler: openapi.New(), tamper: tt.breakIt}, pass.Validator{})

				res, err := via.run(t, eng, engine.RunOptions{})
				require.NoError(t, err, "nothing is asked to look")
				require.NotNil(t, res.Document)

				// With every pass off, so the verdict is the verifier's alone.
				res, err = via.run(t, eng, engine.RunOptions{Verify: true, DisablePasses: []string{engine.ValidatePass}})

				requireVerifyError(t, err, compilers.ErrContract, "compile", tt.codes...)
				assert.Nil(t, res)
			})
		}
	}
}

// TestEngine_RunVerifiesWhatTheCorpusCompilesTo holds the shipped compiler and
// the default passes to the same invariants over every capability spec, and
// requires verifying to change nothing a run returns.
func TestEngine_RunVerifiesWhatTheCorpusCompilesTo(t *testing.T) {
	t.Parallel()
	eng, err := engine.New()
	require.NoError(t, err)
	specs := corpusSpecs(t)
	for _, spec := range specs {
		t.Run(filepath.Base(spec), func(t *testing.T) {
			t.Parallel()
			plain, err := eng.Run(t.Context(), spec, engine.RunOptions{})
			require.NoError(t, err)

			verified, err := eng.Run(t.Context(), spec, engine.RunOptions{Verify: true})

			require.NoError(t, err)
			assert.Equal(t, plain.Failed, verified.Failed)
			assert.Equal(t, plain.Diagnostics, verified.Diagnostics)
			if plain.Document == nil {
				assert.Nil(t, verified.Document)
				return
			}
			assert.Equal(t, irtest.Encoded(t, plain.Document), irtest.Encoded(t, verified.Document))

			again, err := eng.RunDocument(t.Context(), verified.Document, engine.RunOptions{Verify: true})
			require.NoError(t, err, "a document the engine verified is one it verifies again")
			assert.Equal(t, irtest.Encoded(t, plain.Document), irtest.Encoded(t, again.Document))
		})
	}
}

func TestEngine_RunVerifiesARefusalAsNothing(t *testing.T) {
	t.Parallel()
	eng, err := engine.New()
	require.NoError(t, err)

	res, err := eng.RunSources(t.Context(), []compilers.Source{{Path: "spec.yaml", Data: []byte("openapi: 3.1.0\ninfo: [unterminated\n")}},
		engine.RunOptions{Verify: true})

	require.NoError(t, err, "a refusal has no document to verify")
	assert.Nil(t, res.Document)
	assert.True(t, res.Failed)
}

// invalidated returns a transform that copies its input, breaks the copy and
// returns it, leaving the input as it was.
func invalidated(name string) funcPass {
	return funcPass{name, func(doc *ir.Document) (*ir.Document, []ir.Diagnostic) {
		next := *doc
		next.Services = slices.Clone(doc.Services)
		next.Services[0].Name.Canonical = "Cased"
		return &next, nil
	}}
}

// TestEngine_RunVerifiesWhatAPassReturns holds a transform's output to the
// invariants as the compiler's is held, naming the pass, however the run
// starts.
func TestEngine_RunVerifiesWhatAPassReturns(t *testing.T) {
	t.Parallel()
	for _, via := range verifiedEntries {
		t.Run(via.name, func(t *testing.T) {
			t.Parallel()
			eng := engineOf(t, openapi.New(), invalidated("cases"), unreachedPass{t: t, name: "after"})

			res, err := via.run(t, eng, engine.RunOptions{Verify: true})

			requireVerifyError(t, err, pass.ErrContract, `pass "cases"`, "ir/naming-cased")
			assert.Nil(t, res)
		})
	}
}

// TestEngine_RunAcceptsAPassWhoseOutputIsSound is the control for the test
// above: a transform that returns a changed copy is not refused for being one.
func TestEngine_RunAcceptsAPassWhoseOutputIsSound(t *testing.T) {
	t.Parallel()
	renames := changed(func(d *ir.Document) { d.Name = "Renamed" })
	for _, via := range verifiedEntries {
		t.Run(via.name, func(t *testing.T) {
			t.Parallel()
			eng := engineOf(t, openapi.New(), renames, notePass("after"))

			res, err := via.run(t, eng, engine.RunOptions{Verify: true})

			require.NoError(t, err)
			assert.Equal(t, "Renamed", res.Document.Name)
			assert.Equal(t, []string{"test/after"}, codesOf(res))
		})
	}
}

// TestEngine_RunLeavesAPassChangingItsInputToVerify records the outcome for the
// duty no walk of one document can check: a pass that edits the document it was
// handed goes unremarked, and a run asked to verify fails, naming the pass,
// whether the pass returns that document or a copy.
func TestEngine_RunLeavesAPassChangingItsInputToVerify(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		run  func(doc *ir.Document) (*ir.Document, []ir.Diagnostic)
	}{
		{"returning the document", func(doc *ir.Document) (*ir.Document, []ir.Diagnostic) {
			doc.Name = "Mutated"
			return doc, nil
		}},
		{"returning a copy", func(doc *ir.Document) (*ir.Document, []ir.Diagnostic) {
			doc.Name = "Mutated"
			next := *doc
			return &next, nil
		}},
		{"deep in the document", func(doc *ir.Document) (*ir.Document, []ir.Diagnostic) {
			doc.Services[0].Name.Source = "mutated"
			return doc, nil
		}},
		{"into a document that will not encode", func(doc *ir.Document) (*ir.Document, []ir.Diagnostic) {
			doc.Name = "not \xff UTF-8"
			return doc, nil
		}},
	}
	for _, tt := range tests {
		for _, via := range verifiedEntries {
			t.Run(tt.name+"/"+via.name, func(t *testing.T) {
				t.Parallel()
				eng := engineOf(t, openapi.New(), funcPass{"mutator", tt.run}, unreachedPass{t: t, name: "after"})

				res, err := via.run(t, eng, engine.RunOptions{Verify: true})

				require.ErrorIs(t, err, engine.ErrInputChanged)
				require.ErrorIs(t, err, pass.ErrContract)
				assert.Contains(t, err.Error(), `engine: run pass "mutator"`)
				assert.Equal(t, strings.Contains(tt.name, "will not encode"), strings.Contains(err.Error(), "no longer encodes"),
					"the message says when the document no longer encodes, and only then")
				assert.Nil(t, res)
			})
		}
	}
}

func TestEngine_RunDoesNotNoticeAPassChangingItsInputUnlessAsked(t *testing.T) {
	t.Parallel()
	mutator := funcPass{"mutator", func(doc *ir.Document) (*ir.Document, []ir.Diagnostic) {
		doc.Name = "Mutated"
		return doc, nil
	}}
	eng := engineOf(t, openapi.New(), mutator)

	res, err := runFromMemory.run(t, eng, engine.RunOptions{})

	require.NoError(t, err)
	assert.Equal(t, "Mutated", res.Document.Name)
}

// TestEngine_RunDocumentVerifiesTheDocumentItIsHanded holds a document decoded
// from a file to the invariants before any pass reads it, and says so without
// naming a contract: the caller handed it in.
func TestEngine_RunDocumentVerifiesTheDocumentItIsHanded(t *testing.T) {
	t.Parallel()
	doc := keptDocument([]compilers.Source{{Path: "spec.yaml", Data: []byte(testspec.Tiny)}}) // no services
	eng := engineOf(t, openapi.New(), unreachedPass{t: t, name: "after"})

	res, err := eng.RunDocument(t.Context(), doc, engine.RunOptions{Verify: true})

	requireVerifyError(t, err, nil, "input document", "ir/services-absent")
	verr, _ := errors.AsType[*engine.VerifyError](err)
	require.NoError(t, verr.Unwrap())
	assert.Nil(t, res)
	assert.Contains(t, err.Error(), "engine: verify input document: the document breaks")
}

// TestEngine_RunDocumentLeavesTheDocumentItIsHandedUnverifiedByDefault is the
// other half: without Verify the same document runs through the passes.
func TestEngine_RunDocumentLeavesTheDocumentItIsHandedUnverifiedByDefault(t *testing.T) {
	t.Parallel()
	doc := keptDocument([]compilers.Source{{Path: "spec.yaml", Data: []byte(testspec.Tiny)}})
	eng := engineOf(t, openapi.New(), notePass("ran"))

	res, err := eng.RunDocument(t.Context(), doc, engine.RunOptions{})

	require.NoError(t, err)
	assert.Equal(t, []string{"kept/note", "test/ran"}, codesOf(res))
}

// TestEngine_RunVerifiesADocumentThatWillNotEncode pins that a document no
// consumer could serialize fails the run with a violation of its own beside
// the ones irverify reports for it.
func TestEngine_RunVerifiesADocumentThatWillNotEncode(t *testing.T) {
	t.Parallel()
	eng := engineOf(t, tamperer{Compiler: openapi.New(), tamper: func(d *ir.Document) { d.Name = "not \xff UTF-8" }},
		pass.Validator{})

	_, err := runFromMemory.run(t, eng, engine.RunOptions{Verify: true, DisablePasses: []string{engine.ValidatePass}})

	requireVerifyError(t, err, compilers.ErrContract, "compile", "ir/invalid-utf8", "engine/unencodable")
	verr, _ := errors.AsType[*engine.VerifyError](err)
	last := verr.Violations[len(verr.Violations)-1]
	assert.Equal(t, "engine/unencodable", last.Code, "the encoding failure follows what irverify found")
	assert.Contains(t, last.Message, "invalid UTF-8")
}

// TestVerifyError_NamesTheFirstViolationsOnOneLine pins the message: a bounded
// list on one line whatever the violations are, with the whole list beside it.
func TestVerifyError_NamesTheFirstViolationsOnOneLine(t *testing.T) {
	t.Parallel()
	violation := func(code, path string) irverify.Violation {
		return irverify.Violation{Code: code, Message: "m", Path: path}
	}
	tests := []struct {
		name       string
		violations []irverify.Violation
		want       string
	}{
		{"one", []irverify.Violation{violation("ir/a", "doc.A")},
			`engine: verify stage: the document breaks 1 invariants: ir/a at "doc.A"`},
		{"three", []irverify.Violation{violation("ir/a", "doc.A"), violation("ir/b", "doc.B"), violation("ir/c", "doc.C")},
			`engine: verify stage: the document breaks 3 invariants: ir/a at "doc.A"; ir/b at "doc.B"; ir/c at "doc.C"`},
		{"four", []irverify.Violation{violation("ir/a", "A"), violation("ir/b", "B"), violation("ir/c", "C"), violation("ir/d", "D")},
			`engine: verify stage: the document breaks 4 invariants: ir/a at "A"; ir/b at "B"; ir/c at "C"; and 1 more`},
		{"a path with a line break and a quote", []irverify.Violation{violation("ir/a", "doc.Types[a\n\"b\"]")},
			`engine: verify stage: the document breaks 1 invariants: ir/a at "doc.Types[a\n\"b\"]"`},
		{"a path past the bound", []irverify.Violation{violation("ir/a", strings.Repeat("k", 5000))},
			fmt.Sprintf(`engine: verify stage: the document breaks 1 invariants: ir/a at "%s"`, strings.Repeat("k", 200))},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := &engine.VerifyError{Stage: "stage", Violations: tt.violations}

			assert.Equal(t, tt.want, err.Error())
			assert.Len(t, err.Violations, len(tt.violations), "and the list is whole")
		})
	}
}

func TestRunOptions_VerifyIsOffAndAbsentFromTheEncodingByDefault(t *testing.T) {
	t.Parallel()
	off, err := json.Marshal(engine.RunOptions{})
	require.NoError(t, err)
	on, err := json.Marshal(engine.RunOptions{Verify: true})
	require.NoError(t, err)

	assert.NotContains(t, string(off), "verify")
	assert.Contains(t, string(on), `"verify":true`)
}

// TestEngine_RunWalksADocumentOnlyWhereItChanged pins what a verified run
// costs: the compiler's document is held to the invariants once, and each
// document a pass returns once more, but an analysis hands back the document it
// was given, which was held already, and a second walk of it would double the
// cost of verifying for nothing.
func TestEngine_RunWalksADocumentOnlyWhereItChanged(t *testing.T) {
	t.Parallel()
	rename := changed(func(d *ir.Document) { d.Name = "Renamed" })
	versioned := funcPass{"versioned", func(doc *ir.Document) (*ir.Document, []ir.Diagnostic) {
		next := *doc
		next.Version = "2"
		return &next, nil
	}}
	tests := []struct {
		name   string
		passes []pass.Pass
		verify bool
		want   int
	}{
		{"no pass", nil, true, 1},
		{"analyses only", []pass.Pass{notePass("a"), notePass("b")}, true, 1},
		{"a transform among analyses", []pass.Pass{notePass("a"), rename, notePass("b")}, true, 2},
		{"two transforms", []pass.Pass{rename, versioned}, true, 3},
		{"not asked", []pass.Pass{notePass("a"), rename}, false, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			eng := engineOf(t, openapi.New(), tt.passes...)
			var walks int
			engine.CountVerifies(eng, &walks)

			_, err := runFromMemory.run(t, eng, engine.RunOptions{Verify: tt.verify})

			require.NoError(t, err)
			assert.Equal(t, tt.want, walks)
		})
	}
}
