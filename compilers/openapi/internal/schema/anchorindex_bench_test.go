package schema

import (
	"os"
	"strings"
	"testing"

	"github.com/speakeasy-api/openapi/marshaller"
	soa "github.com/speakeasy-api/openapi/openapi"
)

// BenchmarkAnchorWalk measures what deriving the $dynamicAnchor index at entry
// would cost a document that never asks for it.
//
// The index stays a lazy memo rather than living in the immutable context
// (micro-compiler-design §4.1): the walk is not free, almost no document writes
// $dynamicRef, and building it emits a diagnostic that at entry would reach
// documents that never use the keyword.
//
// Read the number against BenchmarkCompile_Petstore in compilers/openapi; the
// design's claim is a ratio.
func BenchmarkAnchorWalk(b *testing.B) {
	// Four levels up, not two: this package sits at compilers/openapi/internal/
	// schema, and the corpus is at the repo root. A missing or unparseable
	// fixture stops the benchmark rather than skipping it — a skip is silent
	// without -v and exits 0, which is how the shorter path went unnoticed.
	data, err := os.ReadFile("../../../../testdata/conformance/openapi/allof-inline-merge.yaml")
	if err != nil {
		b.Fatalf("corpus fixture unavailable: %v", err)
	}
	var doc soa.OpenAPI
	if _, err := marshaller.Unmarshal(b.Context(), strings.NewReader(string(data)), &doc); err != nil {
		b.Fatalf("fixture does not parse: %v", err)
	}
	root := doc.GetRootNode()

	b.ReportAllocs()
	for range b.N {
		if _, complete := dynamicAnchors(root); !complete {
			b.Fatal("the fixture should not exhaust the walk bounds")
		}
	}
}
