package harness

import (
	"context"
	"encoding/json/v2"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/internal/testspec"
	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/ir/irverify"
	"github.com/dexpace/morphic/pass"
)

// badExtDoc returns a document that cannot be marshalled: its Unmodeled holds
// an ir.RawValue with malformed bytes, which the encoder refuses when it writes
// the value into the document.
//
// Check no longer reaches its round-trip oracle with this: irverify reports the
// payload as ir/invalid-raw-value first, which is the point of that check. It is
// used to drive roundTrips and deterministic directly, where the marshal failure
// is the behaviour under test; Check's round-trip outcome is driven through the
// roundTrip seam instead (TestCheck_RoundtripOutcome).
func badExtDoc() *ir.Document {
	return &ir.Document{IRVersion: ir.IRVersion, Unmodeled: ir.Unmodeled{
		"openapi:x": {Reason: ir.ReasonVendorExtension, Value: ir.RawValue("{invalid")},
	}}
}

// dupKeyDoc returns a document whose two type IDs are distinct invalid-UTF-8
// byte strings. The name dates from when both encoded to one U+FFFD-replaced
// JSON key; marshaling either ID is now refused
// (TestRoundTrips_DupKeyDocRefusedAtMarshal).
//
// Verify reports each ID, as registry key and node ID, as ir/invalid-utf8, so
// Check classifies the document as violations before the round-trip oracle runs
// (TestCheck_InvalidUTF8IsAViolationNotARoundTrip). The IDs are otherwise
// well-shaped and the nodes named Any rather than Primitive, so those are the
// only violations drawn: a primitive's ID derives from its kind, and an unnamed
// node is a defect of its own.
func dupKeyDoc() *ir.Document {
	named := ir.Naming{Source: "node", Canonical: "node"}
	sources, services, spaces := tables()
	return &ir.Document{IRVersion: ir.IRVersion, IDSpaces: spaces, Sources: sources, Services: services, Types: ir.TypeRegistry{
		ir.TypeID("t/x/\xff"): &ir.Any{ID: "t/x/\xff", Name: named},
		ir.TypeID("t/x/\xfe"): &ir.Any{ID: "t/x/\xfe", Name: named},
	}}
}

// cyclicDoc returns soundDoc with one example on its model that holds itself: a
// list whose only element is that list. The JSON encoder refuses it, and Verify
// reports it as ir/cyclic-structure, so Check classifies the document as
// violations before the round-trip oracle runs
// (TestCheck_CyclicStructureIsAViolationNotARoundTrip).
func cyclicDoc() *ir.Document {
	list := make([]ir.Value, 1)
	list[0] = ir.Value{Kind: ir.ValueList, List: list}
	example := ir.Value{Kind: ir.ValueList, List: list}
	doc := soundDoc()
	doc.Types["t/x/Model"].Common().Examples = []ir.Example{{Value: &example}}
	return doc
}

// soundDoc returns a minimal, structurally-sound document: one model keyed by its
// own ID with a neutral canonical name, a service and a source, stamped with the
// IR schema version this build writes and declaring its namespaces. It has no
// violations and round-trips through JSON cleanly, so the oracles reach the step
// under test.
func soundDoc() *ir.Document {
	m := &ir.Model{
		ID:   "t/x/Model",
		Name: ir.Naming{Source: "Model", Canonical: "model"},
	}
	sources, services, spaces := tables()
	return &ir.Document{
		IRVersion: ir.IRVersion,
		IDSpaces:  spaces,
		Sources:   sources,
		Services:  services,
		Types:     ir.TypeRegistry{m.ID: m},
	}
}

func TestRoundTrips_MarshalError(t *testing.T) {
	detail, ok := roundTrips(badExtDoc())
	assert.False(t, ok)
	assert.Contains(t, detail, "marshal:")
}

func TestRoundTrips_UnmarshalError(t *testing.T) {
	// A nil TypeDef marshals to `null`; the registry decoder then reads an empty
	// kind tag and rejects it as unknown, so marshal succeeds but the reverse
	// fails.
	doc := &ir.Document{Types: ir.TypeRegistry{ir.TypeID("t/x/nil"): nil}}
	detail, ok := roundTrips(doc)
	assert.False(t, ok)
	assert.Contains(t, detail, "unmarshal:")
}

// TestRoundTrips_DupKeyDocRefusedAtMarshal pins the boundary the stricter codec
// moved: dupKeyDoc's two invalid-UTF-8 IDs used to collide into one
// U+FFFD-replaced JSON key on marshal, which is what used to reach the
// byte-comparison branch below. canonicalOptions now refuses invalid UTF-8
// outright (AllowInvalidUTF8(false)), so the fixture is rejected before a
// collision can even form.
func TestRoundTrips_DupKeyDocRefusedAtMarshal(t *testing.T) {
	detail, ok := roundTrips(dupKeyDoc())
	assert.False(t, ok)
	assert.Contains(t, detail, "marshal:")
	assert.Contains(t, detail, "invalid UTF-8")
}

// TestRoundTrips_MismatchIsReported drives the "round-trip JSON differs" branch
// through the reserializeJSON seam rather than a document. dupKeyDoc, the
// fixture that used to reach it, is refused at marshal time instead (see
// TestRoundTrips_DupKeyDocRefusedAtMarshal), and no document found since
// encodes and then decodes into something that encodes differently. The
// branch stays as the oracle's defence against a decode that loses data.
func TestRoundTrips_MismatchIsReported(t *testing.T) {
	orig := reserializeJSON
	t.Cleanup(func() { reserializeJSON = orig })
	reserializeJSON = func(v any) ([]byte, error) {
		b, err := json.Marshal(v)
		if err != nil {
			return nil, err
		}
		return append(b, '!'), nil // an otherwise-honest remarshal, perturbed by one byte
	}

	detail, ok := roundTrips(soundDoc())
	assert.False(t, ok)
	assert.Contains(t, detail, "round-trip JSON differs")
}

func TestRoundTrips_ReserializeError(t *testing.T) {
	orig := reserializeJSON
	t.Cleanup(func() { reserializeJSON = orig })
	reserializeJSON = func(any) ([]byte, error) { return nil, errors.New("reencode boom") }

	detail, ok := roundTrips(soundDoc())
	assert.False(t, ok)
	assert.Contains(t, detail, "remarshal:")
}

func TestDeterministic_MarshalFirstError(t *testing.T) {
	detail, ok := deterministic(context.Background(), "s", []byte("x"), badExtDoc())
	assert.False(t, ok)
	assert.Contains(t, detail, "marshal first:")
}

func TestDeterministic_RecompileError(t *testing.T) {
	orig := compile
	t.Cleanup(func() { compile = orig })
	compile = func(context.Context, string, []byte, []pass.Pass) (*ir.Document, []ir.Diagnostic, error) {
		return nil, nil, errors.New("recompile boom")
	}

	detail, ok := deterministic(context.Background(), "s", []byte("x"), soundDoc())
	assert.False(t, ok)
	assert.Contains(t, detail, "recompile:")
}

func TestDeterministic_MarshalSecondError(t *testing.T) {
	origC, origR := compile, reserializeJSON
	t.Cleanup(func() { compile, reserializeJSON = origC, origR })
	compile = func(context.Context, string, []byte, []pass.Pass) (*ir.Document, []ir.Diagnostic, error) {
		return soundDoc(), nil, nil
	}
	reserializeJSON = func(any) ([]byte, error) { return nil, errors.New("reencode boom") }

	detail, ok := deterministic(context.Background(), "s", []byte("x"), soundDoc())
	assert.False(t, ok)
	assert.Contains(t, detail, "marshal second:")
}

func TestDeterministic_MismatchIsReported(t *testing.T) {
	orig := compile
	t.Cleanup(func() { compile = orig })
	compile = func(context.Context, string, []byte, []pass.Pass) (*ir.Document, []ir.Diagnostic, error) {
		// Recompiling yields a document different from the one passed in.
		return &ir.Document{Name: "Different"}, nil, nil
	}

	detail, ok := deterministic(context.Background(), "s", []byte("x"), soundDoc())
	assert.False(t, ok)
	assert.Contains(t, detail, "IR JSON differs")
}

// TestCheck_CompilerErrorIsAnErrorOutcome drives the arm that separates a
// compiler that could not run from a spec that was found wanting. It goes
// through the seam because the OpenAPI compiler no longer reaches it on a
// document that will not parse — that is a diagnostic now, and an ErrorDiag
// outcome — leaving cancellation and a caller's bad options as the live
// producers of a Go error here.
func TestCheck_CompilerErrorIsAnErrorOutcome(t *testing.T) {
	orig := compile
	t.Cleanup(func() { compile = orig })
	compile = func(context.Context, string, []byte, []pass.Pass) (*ir.Document, []ir.Diagnostic, error) {
		return nil, nil, errors.New("compile boom")
	}

	r := Check(context.Background(), "spec", []byte("x"))
	assert.Equal(t, OutcomeError, r.Outcome)
	assert.Contains(t, r.Detail, "compile boom")
}

func TestCheck_CompilerPanicIsCaptured(t *testing.T) {
	orig := compile
	t.Cleanup(func() { compile = orig })
	compile = func(context.Context, string, []byte, []pass.Pass) (*ir.Document, []ir.Diagnostic, error) {
		panic("compiler exploded")
	}

	r := Check(context.Background(), "spec", []byte("x"))
	assert.Equal(t, OutcomePanic, r.Outcome)
	assert.Contains(t, r.Detail, "compiler exploded")
}

// panicsInDetect is a compiler whose recognition panics.
type panicsInDetect struct{ stubCompiler }

func (panicsInDetect) Detect(compilers.Source, compilers.Options) (compilers.Recognition, []ir.Diagnostic, bool) {
	panic("recognition exploded")
}

// TestCheck_ARegisteredCompilersPanicIsAPanicOutcome pins the first oracle for a
// compiler reached through the registry, which recovers a panic into an error:
// in recognition, in the compile, and in a recompile that only the determinism
// oracle makes, each is still the panic outcome and not an error one.
func TestCheck_ARegisteredCompilersPanicIsAPanicOutcome(t *testing.T) {
	t.Run("in the compile", func(t *testing.T) {
		withCompilers(t, stubCompiler{lower: func([]compilers.Source) (*ir.Document, []ir.Diagnostic, error) {
			panic("lowering exploded")
		}})

		r := Check(context.Background(), "spec", []byte(stubSource))

		assert.Equal(t, OutcomePanic, r.Outcome, r.Detail)
		assert.Contains(t, r.Detail, "lowering exploded")
		assert.Contains(t, r.Detail, "(at ", "the panic's site comes with it")
	})
	t.Run("in the recompile", func(t *testing.T) {
		compiles := 0
		withCompilers(t, stubCompiler{lower: func(sources []compilers.Source) (*ir.Document, []ir.Diagnostic, error) {
			compiles++
			if compiles > 1 {
				panic("recompile exploded")
			}
			return stubDocument(sources, false), nil, nil
		}})

		r := Check(context.Background(), "spec", []byte(stubSource))

		assert.Equal(t, OutcomePanic, r.Outcome, r.Detail)
		assert.Contains(t, r.Detail, "recompile exploded")
		assert.Equal(t, 2, compiles, "the first compile passed every oracle before it")
	})
	t.Run("in recognition", func(t *testing.T) {
		withCompilers(t, panicsInDetect{})

		r := Check(context.Background(), "spec", []byte(stubSource))

		assert.Equal(t, OutcomePanic, r.Outcome, r.Detail)
		assert.Contains(t, r.Detail, "recognition exploded")
	})
}

// TestSpecFiles_ARecognitionThatPanicsIsAnError pins that listing a tree does
// not crash on a compiler that panics while asked about a file: the registry
// recovers it, and listing reports it as the fault of the compiler it is.
func TestSpecFiles_ARecognitionThatPanicsIsAnError(t *testing.T) {
	withCompilers(t, panicsInDetect{})
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "api.stub"), []byte(stubSource), 0o600))

	_, err := SpecFiles(context.Background(), dir)

	require.ErrorIs(t, err, compilers.ErrContract)
	assert.Contains(t, err.Error(), "recognition exploded")
}

func TestCheck_ViolationsOutcome(t *testing.T) {
	orig := compile
	t.Cleanup(func() { compile = orig })
	compile = func(context.Context, string, []byte, []pass.Pass) (*ir.Document, []ir.Diagnostic, error) {
		// A registry key that disagrees with the node ID is a structural violation.
		return &ir.Document{Types: ir.TypeRegistry{
			ir.TypeID("t/x/Key"): &ir.Any{ID: "t/x/Other"},
		}}, nil, nil
	}

	r := Check(context.Background(), "spec", []byte("x"))
	assert.Equal(t, OutcomeViolations, r.Outcome)
}

// TestCheck_RoundtripOutcome pins that Check maps a failed round trip to
// OutcomeRoundtrip. It goes through the roundTrip seam rather than a document,
// so what it pins does not depend on which documents Verify lets through to
// that oracle.
func TestCheck_RoundtripOutcome(t *testing.T) {
	origCompile, origRoundTrip := compile, roundTrip
	t.Cleanup(func() { compile, roundTrip = origCompile, origRoundTrip })
	compile = func(context.Context, string, []byte, []pass.Pass) (*ir.Document, []ir.Diagnostic, error) {
		return soundDoc(), nil, nil
	}
	roundTrip = func(*ir.Document) (string, bool) { return "forced round-trip failure", false }

	r := Check(context.Background(), "spec", []byte("x"))
	assert.Equal(t, OutcomeRoundtrip, r.Outcome)
	assert.Equal(t, "forced round-trip failure", r.Detail)
}

// TestCheck_InvalidRawValueIsAViolationNotARoundTrip pins the reordering the
// payload check causes: a document whose Unmodeled payload cannot be marshalled
// used to be classified by the round-trip oracle, which reports it as a
// serialization mismatch rather than the compiler bug it is. Verify now names it
// first.
func TestCheck_InvalidRawValueIsAViolationNotARoundTrip(t *testing.T) {
	orig := compile
	t.Cleanup(func() { compile = orig })
	compile = func(context.Context, string, []byte, []pass.Pass) (*ir.Document, []ir.Diagnostic, error) {
		return badExtDoc(), nil, nil
	}

	r := Check(context.Background(), "spec", []byte("x"))
	assert.Equal(t, OutcomeViolations, r.Outcome)
	assert.Contains(t, r.Detail, "ir/invalid-raw-value")
}

// TestCheck_InvalidUTF8IsAViolationNotARoundTrip pins the reordering checkUTF8
// causes: dupKeyDoc's two ill-formed type IDs used to reach the round-trip
// oracle, since Verify held no opinion on invalid UTF-8 outside naming and
// diagnostic messages. Verify now reports each ill-formed ID as
// ir/invalid-utf8 before Check ever reaches roundTrip.
func TestCheck_InvalidUTF8IsAViolationNotARoundTrip(t *testing.T) {
	orig := compile
	t.Cleanup(func() { compile = orig })
	compile = func(context.Context, string, []byte, []pass.Pass) (*ir.Document, []ir.Diagnostic, error) {
		return dupKeyDoc(), nil, nil
	}

	r := Check(context.Background(), "spec", []byte("x"))
	assert.Equal(t, OutcomeViolations, r.Outcome)
	assert.Contains(t, r.Detail, "ir/invalid-utf8")

	vs := irverify.Verify(dupKeyDoc())
	codes := make([]string, 0, len(vs))
	for _, v := range vs {
		codes = append(codes, v.Code)
	}
	assert.Equal(t, []string{"ir/invalid-utf8", "ir/invalid-utf8", "ir/invalid-utf8", "ir/invalid-utf8"}, codes,
		"each ill-formed ID, as registry key and as node ID, is all Verify reports")
}

// TestCheck_CyclicStructureIsAViolationNotARoundTrip pins where a document that
// contains itself is classified. The encoder refuses it, so roundTrips alone
// reports only a failed marshal; Verify names the defect, as
// ir/cyclic-structure, and runs first, so Check reports the cause rather than
// its consequence (GitHub #573).
func TestCheck_CyclicStructureIsAViolationNotARoundTrip(t *testing.T) {
	orig := compile
	t.Cleanup(func() { compile = orig })
	compile = func(context.Context, string, []byte, []pass.Pass) (*ir.Document, []ir.Diagnostic, error) {
		return cyclicDoc(), nil, nil
	}

	r := Check(context.Background(), "spec", []byte("x"))
	assert.Equal(t, OutcomeViolations, r.Outcome)
	assert.Contains(t, r.Detail, "ir/cyclic-structure")

	detail, ok := roundTrips(cyclicDoc())
	assert.False(t, ok)
	assert.True(t, strings.HasPrefix(detail, "marshal: "), "the first marshal refuses it: %s", detail)
	assert.Contains(t, detail, "cycle")
}

func TestCheck_NondeterministicOutcome(t *testing.T) {
	orig := compile
	t.Cleanup(func() { compile = orig })
	var n int
	compile = func(context.Context, string, []byte, []pass.Pass) (*ir.Document, []ir.Diagnostic, error) {
		n++
		if n == 1 {
			return soundDoc(), nil, nil
		}
		return &ir.Document{Name: "SecondCompile"}, nil, nil
	}

	r := Check(context.Background(), "spec", []byte("x"))
	assert.Equal(t, OutcomeNondeterministic, r.Outcome)
}

// stubDocument is a sound document for the one source, naming the first key of
// its text as a model, so a lowering that reads it reads the mapping's order.
func stubDocument(sources []compilers.Source, byFirstKey bool) *ir.Document {
	doc := soundDoc()
	doc.Sources = []ir.SourceInfo{{
		Format: stubFormat.String(), Path: sources[0].Path, Hash: ir.SourceHash(sources[0].Data),
	}}
	if byFirstKey {
		key, _, _ := strings.Cut(string(sources[0].Data), ":")
		id := ir.TypeID("t/x/" + key)
		doc.Types = ir.TypeRegistry{id: &ir.Model{ID: id, Name: ir.Naming{Source: key, Canonical: key}}}
	}
	return doc
}

// stubSource is a source of the stub's, written as a YAML mapping so the order
// oracle has entries to permute.
const stubSource = "stub: 1\na: x\nb: y\n"

// TestCheck_ASecondRegisteredCompilerIsSweptWithEveryOracle is the point of
// compiling through the registry: a compiler added beside the built-ins gets its
// files listed, compiled and put through the oracles with no change to the
// harness. The order oracle is the last of them, so reaching it shows the rest
// were passed.
func TestCheck_ASecondRegisteredCompilerIsSweptWithEveryOracle(t *testing.T) {
	for _, tc := range []struct {
		name       string
		byFirstKey bool
		want       Outcome
	}{
		{"a lowering independent of mapping order", false, OutcomeOK},
		{"a lowering that reads mapping order", true, OutcomeOrderDependent},
	} {
		t.Run(tc.name, func(t *testing.T) {
			withCompilers(t, stubCompiler{lower: func(sources []compilers.Source) (*ir.Document, []ir.Diagnostic, error) {
				return stubDocument(sources, tc.byFirstKey), nil, nil
			}})
			dir := t.TempDir()
			spec := filepath.Join(dir, "api.stub")
			require.NoError(t, os.WriteFile(spec, []byte(stubSource), 0o600))

			files, err := SpecFiles(context.Background(), dir)
			require.NoError(t, err)
			require.Equal(t, []string{spec}, files, "the stub's file is listed by its claim, not its extension")

			results, err := CheckPath(context.Background(), dir)
			require.NoError(t, err)
			require.Len(t, results, 1)
			assert.Equal(t, tc.want, results[0].Outcome, results[0].Detail)
		})
	}
}

// TestCheck_CompileFaultsAreErrorOutcomes covers what stops a compile before an
// oracle can ask anything of it: a compiler that fails, one that breaks the
// compiler contract, a registry that cannot be built, and a canceled context.
func TestCheck_CompileFaultsAreErrorOutcomes(t *testing.T) {
	refuse := func([]compilers.Source) (*ir.Document, []ir.Diagnostic, error) { return nil, nil, nil }
	fail := func([]compilers.Source) (*ir.Document, []ir.Diagnostic, error) {
		return nil, nil, errors.New("lowering boom")
	}

	for _, tc := range []struct {
		name     string
		canceled bool
		extra    []compilers.Compiler
		want     string
	}{
		{"a compiler that fails", false, []compilers.Compiler{stubCompiler{lower: fail}}, "lowering boom"},
		{"a refusal that says nothing", false, []compilers.Compiler{stubCompiler{lower: refuse}}, compilers.ErrContract.Error()},
		{"a nil compiler", false, []compilers.Compiler{nil}, "register compiler 1"},
		{"a format registered twice", false, []compilers.Compiler{openapiLike{}}, "already registered"},
		{"a canceled context", true, nil, "engine: detect"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			withCompilers(t, tc.extra...)
			ctx, cancel := context.WithCancel(context.Background())
			t.Cleanup(cancel)
			if tc.canceled {
				cancel()
			}

			r := Check(ctx, "spec", []byte(stubSource))
			assert.Equal(t, OutcomeError, r.Outcome, r.Detail)
			assert.Contains(t, r.Detail, tc.want)
		})
	}
}

// openapiLike claims a format the built-in compiler already serves.
type openapiLike struct{ stubCompiler }

func (openapiLike) Formats() []compilers.SourceFormat {
	return []compilers.SourceFormat{{Name: "openapi", Version: "3.1"}}
}

// TestCheck_AFindingOnTheDocumentAloneIsRead pins that the sweep reads both of
// a compiler's channels: one storing its error on the document alone is
// classified by it, as the engine judges the run by it.
func TestCheck_AFindingOnTheDocumentAloneIsRead(t *testing.T) {
	withCompilers(t, stubCompiler{lower: func(sources []compilers.Source) (*ir.Document, []ir.Diagnostic, error) {
		doc := stubDocument(sources, false)
		doc.Diagnostics = []ir.Diagnostic{ir.NewDiagnostic(ir.SeverityError, "stub/broken", "stored only", ir.Provenance{})}
		return doc, nil, nil
	}})

	r := Check(context.Background(), "spec", []byte(stubSource))

	assert.Equal(t, OutcomeErrorDiag, r.Outcome, r.Detail)
	assert.Contains(t, r.Detail, "stub/broken")
}

// reporting is an analysis that reports one error on every document.
type reporting struct{}

func (reporting) Name() string { return "reporting" }

func (reporting) Run(doc *ir.Document) (*ir.Document, []ir.Diagnostic) {
	return doc, []ir.Diagnostic{ir.NewDiagnostic(ir.SeverityError, "stub/reported", "every document",
		ir.Provenance{Source: ir.NoSource})}
}

// TestCheck_RunsTheSweepsPasses pins that every compile of a sweep runs its
// pass list, not the compiler alone: an error only a pass reports is the
// spec's first finding.
func TestCheck_RunsTheSweepsPasses(t *testing.T) {
	withPasses(t, reporting{})

	r := Check(context.Background(), "spec.yaml", []byte(testspec.Tiny))

	assert.Equal(t, OutcomeErrorDiag, r.Outcome, r.Detail)
	assert.Equal(t, "stub/reported: every document", r.Detail)
}
