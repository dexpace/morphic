package harness

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
)

// badExtDoc returns a document that cannot be marshalled: its Extensions hold an
// invalid json.RawMessage whose MarshalJSON rejects the malformed bytes.
func badExtDoc() *ir.Document {
	return &ir.Document{Extensions: ir.Extensions{"openapi:x": ir.RawValue("{invalid")}}
}

// soundDoc returns a minimal, structurally-sound document: one model keyed by its
// own ID with a neutral canonical name. It has no violations and round-trips
// through JSON cleanly, so the oracles reach the step under test.
func soundDoc() *ir.Document {
	m := &ir.Model{TypeCommon: ir.TypeCommon{
		ID:   "t/x/Model",
		Name: ir.Naming{Source: "Model", Canonical: "model"},
	}}
	return &ir.Document{Types: ir.TypeRegistry{m.ID: m}}
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

func TestRoundTrips_MismatchIsReported(t *testing.T) {
	// Two type IDs that are distinct invalid-UTF-8 byte strings both encode to the
	// same U+FFFD key, so the marshalled object carries a duplicate key that
	// decodes back to a single entry — a genuine serialization loss the JSON
	// round-trip comparison catches.
	doc := &ir.Document{Types: ir.TypeRegistry{
		ir.TypeID("\xff"): &ir.Primitive{},
		ir.TypeID("\xfe"): &ir.Primitive{},
	}}
	detail, ok := roundTrips(doc)
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
			ir.TypeID("t/x/Key"): &ir.Any{TypeCommon: ir.TypeCommon{ID: "t/x/Other"}},
		}}, nil, nil
	}

	r := Check(context.Background(), "spec", []byte("x"))
	assert.Equal(t, OutcomeViolations, r.Outcome)
}

func TestCheck_RoundtripOutcome(t *testing.T) {
	orig := compile
	t.Cleanup(func() { compile = orig })
	compile = func(context.Context, string, []byte) (*ir.Document, []ir.Diagnostic, error) {
		return badExtDoc(), nil, nil
	}

	r := Check(context.Background(), "spec", []byte("x"))
	assert.Equal(t, OutcomeRoundtrip, r.Outcome)
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
