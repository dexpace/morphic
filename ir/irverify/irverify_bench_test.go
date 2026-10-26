package irverify_test // external test package — importing across layers is legal in tests

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/ir/irverify"
)

// BenchmarkVerify_Corpus measures Verify over every document the OpenAPI
// compiler produces from the committed corpus, compiled once beforehand. Verify
// is a walk of each document whatever it holds, so the cost tracks the
// documents' size; the corpus gives it every shape a compiler produces rather
// than one spec's.
func BenchmarkVerify_Corpus(b *testing.B) {
	var docs []*ir.Document
	for _, f := range corpusSpecs(b) {
		data, err := os.ReadFile(f)
		require.NoError(b, err)
		if doc := compile(b, filepath.Base(f), data); doc != nil {
			docs = append(docs, doc)
		}
	}
	require.NotEmpty(b, docs, "the corpus must yield documents, or the benchmark measures nothing")

	b.ReportAllocs()
	for b.Loop() {
		for _, doc := range docs {
			_ = irverify.Verify(doc)
		}
	}
}
