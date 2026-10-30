// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package anchors_test

import (
	"os"
	"testing"

	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/anchors"
)

// BenchmarkBuild measures what deriving the $dynamicAnchor index at entry
// would cost a document that never asks for it.
//
// The index stays a lazy memo (lowering.Ctx.DynamicAnchorSites) rather than
// being built with the context: the walk is not free, almost no document
// writes $dynamicRef, and building it emits a diagnostic that at entry would
// reach documents that never use the keyword.
//
// Read the number against BenchmarkCompile_Petstore in compilers/openapi; the
// design's claim is a ratio.
func BenchmarkBuild(b *testing.B) {
	// Four levels up: this package sits at compilers/openapi/internal/anchors,
	// and the corpus is at the repo root. A missing or unparseable fixture
	// stops the benchmark rather than skipping it, since a skip is silent
	// without -v and exits 0.
	data, err := os.ReadFile("../../../../testdata/conformance/openapi/allof-inline-merge.yaml")
	if err != nil {
		b.Fatalf("corpus fixture unavailable: %v", err)
	}
	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		b.Fatalf("fixture does not parse: %v", err)
	}

	b.ReportAllocs()
	for b.Loop() {
		if !anchors.Build(&root).Complete() {
			b.Fatal("the fixture should not exhaust the walk bounds")
		}
	}
}
