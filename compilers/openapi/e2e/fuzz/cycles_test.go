// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package fuzz_test

import (
	"testing"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi"
	"github.com/dexpace/morphic/internal/testspec"
	"github.com/dexpace/morphic/ir"
)

// FuzzCycleDetector compiles arbitrary bytes seeded from the cycle corpus.
// Whatever a source holds, the compile must not panic, overflow the stack or
// hang, and must come back with a document or with a refusal: an error
// diagnostic saying why no document came back, or a Go error alone, which an
// empty source raises. A cycle of schema $refs refuses nothing, so a corpus
// seed comes back with a document.
func FuzzCycleDetector(f *testing.F) {
	addYAMLDir(f, cycleFixtureDir)
	for _, spec := range testspec.RefShapedData() {
		f.Add([]byte(spec.Data))
	}
	f.Add([]byte(" "))         // whitespace-only: recoverable parser panic
	f.Add([]byte("\t\x00: [")) // undecodable: reported as a parse error, not a fault

	f.Fuzz(func(t *testing.T, data []byte) {
		doc, diags, err := openapi.New().Compile(t.Context(),
			[]compilers.Source{{Path: "fuzz.yaml", Data: data}}, compilers.Options{})
		if err != nil {
			if doc != nil {
				t.Errorf("a Go error returned a document: %v", err)
			}
			return
		}
		if doc == nil && !ir.HasError(diags) {
			t.Errorf("no document came back, and no error says why: %+v", diags)
		}
	})
}
