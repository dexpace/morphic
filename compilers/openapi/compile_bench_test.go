package openapi_test // external test package — exercises only the public API

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi"
	"github.com/dexpace/morphic/ir"
)

// BenchmarkCompile_Petstore measures one whole compile of the golden petstore —
// parse, lower, assemble — which is the pipeline stage every other cost is
// judged against.
//
// It is also the denominator BenchmarkAnchorWalk asks for. That benchmark's
// claim is a ratio: the $dynamicAnchor walk is small next to a compile, which is
// why the index stays a memo instead of being derived at entry. A ratio needs
// both numbers, measured over the same corpus in the same way.
func BenchmarkCompile_Petstore(b *testing.B) {
	data := petstoreSpec(b)
	c := openapi.New()
	src := []compilers.Source{{Path: "petstore.yaml", Data: data}}

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		doc, _, err := c.Compile(b.Context(), src, compilers.Options{})
		if err != nil {
			b.Fatalf("compile: %v", err)
		}
		if doc == nil {
			b.Fatal("compile produced no document")
		}
	}
}

// BenchmarkMarshalDocument_Petstore measures serializing a compiled document.
// The IR's sum types and BigVal carry hand-written MarshalJSON, and every golden
// snapshot, IR diff and cache entry pays this cost, so it is worth watching
// separately from the compile that produced the document.
func BenchmarkMarshalDocument_Petstore(b *testing.B) {
	doc := compilePetstore(b)

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if _, err := json.Marshal(doc); err != nil {
			b.Fatalf("marshal: %v", err)
		}
	}
}

// BenchmarkUnmarshalDocument_Petstore measures reading a document back. It is
// the other half of the round-trip invariant, and the half a consumer of a
// cached or piped IR document pays.
func BenchmarkUnmarshalDocument_Petstore(b *testing.B) {
	encoded, err := json.Marshal(compilePetstore(b))
	require.NoError(b, err)

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		var back ir.Document
		if err := json.Unmarshal(encoded, &back); err != nil {
			b.Fatalf("unmarshal: %v", err)
		}
	}
}

// petstoreSpec reads the golden petstore, failing the benchmark rather than
// skipping it: a benchmark that quietly measures nothing is worse than one that
// stops.
func petstoreSpec(b *testing.B) []byte {
	b.Helper()
	data, err := os.ReadFile(goldenPetstore)
	require.NoError(b, err)
	require.NotEmpty(b, data)
	return data
}

// compilePetstore compiles the golden petstore once, for the benchmarks whose
// subject is what happens to a document afterwards.
func compilePetstore(b *testing.B) *ir.Document {
	b.Helper()
	doc, _, err := openapi.New().Compile(b.Context(),
		[]compilers.Source{{Path: "petstore.yaml", Data: petstoreSpec(b)}}, compilers.Options{})
	require.NoError(b, err)
	require.NotNil(b, doc)
	return doc
}
