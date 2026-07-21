package openapi

import (
	"os"
	"testing"

	"github.com/dexpace/morphic/compilers"
)

// FuzzCompile is the standing regression guard for GitHub #12: no input, however
// degenerate, may crash the process with a fatal stack overflow. The contract is
// process survival, not a particular verdict — Compile may return a document,
// diagnostics, or a Go error, but must never fault.
//
// The seed corpus pins the known reproducers, the legal ref-shaped controls, and
// the parser-panic inputs, so a plain `go test` replays them and a detector
// regression that lets a cycle reach the parser faults here rather than in
// production. After a speakeasy bump, run the active search to look for new cycle
// shapes the schema-position scoping may no longer cover:
//
//	go test -run x -fuzz FuzzCompile ./compilers/openapi
func FuzzCompile(f *testing.F) {
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
