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
// micro-compiler-design §4.1 originally put the index in the immutable context,
// derived once at entry. It stays a memo instead, and this is half the reason:
// the walk is small but not free, and almost no document writes $dynamicRef. The
// other half is that building it emits a diagnostic, which at entry would reach
// documents that never use the keyword.
//
// Compare against a whole compile rather than reading the number alone — the
// claim in the design is a ratio, and a ratio is what has to stay true.
// BenchmarkCompile_Petstore in compilers/openapi is the other half.
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
