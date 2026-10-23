package harness

import (
	"bytes"
	"context"
	"testing"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/engine"
	"github.com/dexpace/morphic/ir"
)

// emptySHA256 is the SHA-256 of zero bytes in lowercase hex, the digest a
// source table entry carries.
const emptySHA256 = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"

// tables returns the source table and service every compiled document carries,
// and the namespaces their IDs and the fixtures' types live in, so a stub
// document reaches the oracle it is named for rather than the checks of the
// tables.
func tables() ([]ir.SourceInfo, []ir.Service, map[string][]string) {
	return []ir.SourceInfo{{Format: "openapi@3.1", Path: "api.yaml", Hash: emptySHA256}},
		[]ir.Service{{ID: "s/x/S", Name: ir.Naming{Source: "svc", Canonical: "svc"}}},
		map[string][]string{ir.IDKindType: {"x"}, ir.IDKindService: {"x"}}
}

// stubFormat is the format stubCompiler serves, one the engine does not carry.
var stubFormat = compilers.SourceFormat{Name: "stub", Version: "1.0"}

// stubCompiler is a compiler the engine does not register. It takes any source
// that mentions its marker, and lower stands for its lowering.
type stubCompiler struct {
	lower func(sources []compilers.Source) (*ir.Document, []ir.Diagnostic, error)
}

func (stubCompiler) Formats() []compilers.SourceFormat {
	return []compilers.SourceFormat{stubFormat}
}

func (stubCompiler) Detect(src compilers.Source, _ compilers.Options) (compilers.Recognition, []ir.Diagnostic, bool) {
	if !bytes.Contains(src.Data, []byte("stub:")) {
		return compilers.Recognition{}, nil, false
	}
	return compilers.Recognition{Format: stubFormat}, nil, true
}

func (stubCompiler) DecodeOptions(compilers.OptionSet) (any, error) { return nil, nil }

func (stubCompiler) SourceTable(sources []compilers.Source, _ compilers.Options) []ir.SourceInfo {
	return []ir.SourceInfo{{Path: sources[0].Path}}
}

func (c stubCompiler) Compile(_ context.Context, sources []compilers.Source, _ compilers.Options) (*ir.Document, []ir.Diagnostic, error) {
	return c.lower(sources)
}

// withCompilers makes sweeps compile with the engine's built-ins and extra for
// the rest of the test.
func withCompilers(t *testing.T, extra ...compilers.Compiler) {
	t.Helper()
	replaceRegistered(t, func() []compilers.Compiler { return append(engine.Compilers(), extra...) })
}

// replaceRegistered makes sweeps compile with the set f gives, for the rest of
// the test.
func replaceRegistered(t *testing.T, f func() []compilers.Compiler) {
	t.Helper()
	orig := registered
	t.Cleanup(func() { registered = orig })
	registered = f
}
