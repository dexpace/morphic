package harness

import (
	"context"
	"encoding/json/v2"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
)

// badExtDoc returns a document that cannot be marshalled: its Unmodeled holds
// an ir.RawValue with malformed bytes, which the encoder refuses when it writes
// the value into the document.
//
// Check no longer reaches its round-trip oracle with this: irverify reports the
// payload as ir/invalid-raw-value first, which is the point of that check. It is
// used to drive roundTrips and deterministic directly, where the marshal failure
// is the behaviour under test; dupKeyDoc is the fixture for Check's round-trip
// outcome.
func badExtDoc() *ir.Document {
	return &ir.Document{IRVersion: ir.IRVersion, Unmodeled: ir.Unmodeled{
		"openapi:x": {Reason: ir.ReasonVendorExtension, Value: ir.RawValue("{invalid")},
	}}
}

// dupKeyDoc returns a document whose two type IDs are distinct invalid-UTF-8
// byte strings. Before canonicalOptions started refusing invalid UTF-8, the
// two encoded to the same U+FFFD-replaced JSON key, so the marshalled object
// carried a duplicate key that silently lost an entry on the way back in —
// that collision is what gives the fixture its name. Now marshaling either ID
// is refused outright, before any such collision can form
// (TestRoundTrips_DupKeyDocRefusedAtMarshal, which calls roundTrips directly).
//
// checkUTF8 reports each ill-formed ID as ir/invalid-utf8
// (TestCheck_InvalidUTF8IsAViolationNotARoundTrip), so Check no longer reaches
// the round-trip oracle on this document at all: it is a structural violation,
// and Check returns at the first one. TestCheck_RoundtripOutcome proves the
// round-trip classification through the roundTrip seam instead, since no
// document that verifies clean reaches roundTrips' own failure branches to
// prove it with any more.
//
// The IDs are otherwise well-shaped, the nodes are Any rather than Primitive,
// and both are named, for the reason each was originally: so that, called
// directly against roundTrips, nothing but the ill-formed bytes this fixture
// exists to drive is what fires. A primitive's ID is derived from its kind, so
// a primitive anywhere but t/prim/<kind> would be a defect of its own, and an
// unnamed node is a structural violation in its own right — either would be
// classified before roundTrips ever ran.
func dupKeyDoc() *ir.Document {
	named := ir.Naming{Source: "node", Canonical: "node"}
	return &ir.Document{IRVersion: ir.IRVersion, Types: ir.TypeRegistry{
		ir.TypeID("t/x/\xff"): &ir.Any{ID: "t/x/\xff", Name: named},
		ir.TypeID("t/x/\xfe"): &ir.Any{ID: "t/x/\xfe", Name: named},
	}}
}

// soundDoc returns a minimal, structurally-sound document: one model keyed by its
// own ID with a neutral canonical name, stamped with the IR schema version this
// build writes. It has no violations and round-trips through JSON cleanly, so the
// oracles reach the step under test.
func soundDoc() *ir.Document {
	m := &ir.Model{
		ID:   "t/x/Model",
		Name: ir.Naming{Source: "Model", Canonical: "model"},
	}
	return &ir.Document{IRVersion: ir.IRVersion, Types: ir.TypeRegistry{m.ID: m}}
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
	compile = func(context.Context, string, []byte) (*ir.Document, []ir.Diagnostic, error) {
		return nil, nil, errors.New("recompile boom")
	}

	detail, ok := deterministic(context.Background(), "s", []byte("x"), soundDoc())
	assert.False(t, ok)
	assert.Contains(t, detail, "recompile:")
}

func TestDeterministic_MarshalSecondError(t *testing.T) {
	origC, origR := compile, reserializeJSON
	t.Cleanup(func() { compile, reserializeJSON = origC, origR })
	compile = func(context.Context, string, []byte) (*ir.Document, []ir.Diagnostic, error) {
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
	compile = func(context.Context, string, []byte) (*ir.Document, []ir.Diagnostic, error) {
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
	compile = func(context.Context, string, []byte) (*ir.Document, []ir.Diagnostic, error) {
		return nil, nil, errors.New("compile boom")
	}

	r := Check(context.Background(), "spec", []byte("x"))
	assert.Equal(t, OutcomeError, r.Outcome)
	assert.Contains(t, r.Detail, "compile boom")
}

func TestCheck_CompilerPanicIsCaptured(t *testing.T) {
	orig := compile
	t.Cleanup(func() { compile = orig })
	compile = func(context.Context, string, []byte) (*ir.Document, []ir.Diagnostic, error) {
		panic("compiler exploded")
	}

	r := Check(context.Background(), "spec", []byte("x"))
	assert.Equal(t, OutcomePanic, r.Outcome)
	assert.Contains(t, r.Detail, "compiler exploded")
}

func TestCheck_ViolationsOutcome(t *testing.T) {
	orig := compile
	t.Cleanup(func() { compile = orig })
	compile = func(context.Context, string, []byte) (*ir.Document, []ir.Diagnostic, error) {
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
// because no document that verifies clean reaches roundTrips' own failure
// branches any more — checkUTF8 and checkRawPayloads between them account for
// every way a Verify-clean document used to fail there (dupKeyDoc, the
// fixture that once did, is now TestCheck_InvalidUTF8IsAViolationNotARoundTrip)
// — leaving this branch in Check itself otherwise uncovered.
func TestCheck_RoundtripOutcome(t *testing.T) {
	origCompile, origRoundTrip := compile, roundTrip
	t.Cleanup(func() { compile, roundTrip = origCompile, origRoundTrip })
	compile = func(context.Context, string, []byte) (*ir.Document, []ir.Diagnostic, error) {
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
	compile = func(context.Context, string, []byte) (*ir.Document, []ir.Diagnostic, error) {
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
	compile = func(context.Context, string, []byte) (*ir.Document, []ir.Diagnostic, error) {
		return dupKeyDoc(), nil, nil
	}

	r := Check(context.Background(), "spec", []byte("x"))
	assert.Equal(t, OutcomeViolations, r.Outcome)
	assert.Contains(t, r.Detail, "ir/invalid-utf8")
}

func TestCheck_NondeterministicOutcome(t *testing.T) {
	orig := compile
	t.Cleanup(func() { compile = orig })
	var n int
	compile = func(context.Context, string, []byte) (*ir.Document, []ir.Diagnostic, error) {
		n++
		if n == 1 {
			return soundDoc(), nil, nil
		}
		return &ir.Document{Name: "SecondCompile"}, nil, nil
	}

	r := Check(context.Background(), "spec", []byte("x"))
	assert.Equal(t, OutcomeNondeterministic, r.Outcome)
}

func TestIsSpecFile_Extensions(t *testing.T) {
	assert.True(t, isSpecFile("api.yaml"))
	assert.True(t, isSpecFile("api.yml"))
	assert.True(t, isSpecFile("api.json"))
	assert.False(t, isSpecFile("ir.golden.json"), "golden IR snapshots are skipped")
	assert.False(t, isSpecFile("notes.txt"), "a non-spec extension is skipped")
}

func TestCheckFile_ReadErrorOnDirectory(t *testing.T) {
	// os.ReadFile of a directory fails with EISDIR, which checkFile surfaces as a
	// Go error rather than a Result.
	_, err := checkFile(context.Background(), t.TempDir())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "harness: read")
}

func TestCheckDir_WalkErrorPropagates(t *testing.T) {
	// A directory that does not exist makes WalkDir invoke the callback with a
	// non-nil error, which checkDir wraps and returns.
	_, err := checkDir(context.Background(), filepath.Join(t.TempDir(), "absent"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "harness: walk")
}

func TestCheckDir_UnreadableSpecFileErrors(t *testing.T) {
	// A broken symlink named like a spec is visited as a non-directory spec file,
	// but reading it fails when the link target is followed. The walk aborts with
	// that error rather than a silent skip.
	dir := t.TempDir()
	link := filepath.Join(dir, "spec.yaml")
	require.NoError(t, os.Symlink(filepath.Join(dir, "no-such-target"), link))

	_, err := checkDir(context.Background(), dir)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "harness: read")
}
