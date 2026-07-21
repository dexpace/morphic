package openapi

import (
	"os"
	"testing"

	"github.com/dexpace/morphic/compilers"
)

// FuzzCycleDetector is the standing regression guard for GitHub #12: no input,
// however degenerate, may crash the process with a fatal stack overflow. The
// contract is process survival, not a particular verdict — Compile may return a
// document, diagnostics, or a Go error, but must never fault.
//
// It is distinct from the corpus-seeded FuzzCompile in fuzz_test.go, which asserts
// structural oracles on cleanly-compiled specs: this target instead seeds the
// known degenerate reproducers, the legal ref-shaped controls, and the
// parser-panic inputs, so a plain `go test` replays them and a detector regression
// that lets a cycle reach the parser faults here. Mutating from those cycle shapes
// is also the surest way to surface a new crashing shape after a speakeasy bump
// changes which reference positions the resolver recurses through:
//
//	go test -run x -fuzz FuzzCycleDetector ./compilers/openapi
func FuzzCycleDetector(f *testing.F) {
	for _, tc := range cycleReproducers {
		if data, err := os.ReadFile("../../testdata/openapi/" + tc.file + ".yaml"); err == nil {
			f.Add(data)
		}
	}
	for _, tc := range refShapedDataSpecs {
		f.Add([]byte(tc.data))
	}
	f.Add([]byte(" "))         // whitespace-only: recoverable parser panic
	f.Add([]byte("\t\x00: [")) // undecodable: reported as a parse error, not a fault

	f.Fuzz(func(t *testing.T, data []byte) {
		_, _, _ = New().Compile(t.Context(),
			[]compilers.Source{{Path: "fuzz.yaml", Data: data}}, compilers.Options{})
	})
}
