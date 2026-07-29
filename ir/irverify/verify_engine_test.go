package irverify_test // external test package — importing across layers is legal in tests

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/engine"
	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/ir/irverify"
)

// noSourceDiagnostics counts the document's diagnostics that address no input
// file. Every diagnostic pass.Validate emits is one: it reports on the document
// it was handed, not on a position in a source.
func noSourceDiagnostics(doc *ir.Document) int {
	var n int
	for _, d := range doc.Diagnostics {
		if d.Provenance.Source == ir.NoSource {
			n++
		}
	}
	return n
}

// TestVerify_EngineOutput runs the committed corpus through the whole pipeline
// and verifies what comes out of it.
//
// TestVerify_Corpus already sweeps compiler output. What the engine adds is
// pass.Validate's diagnostics, which engine.Run folds into Document.Diagnostics,
// and those carry ir.NoSource — so this is the only population that exercises
// the sentinel end to end. A verifier that held those diagnostics to the source
// table would report one violation per validation finding, making a document
// less valid the more spec problems the validator found in it; that is a
// property of the pipeline's own output, which no hand-built fixture can stand
// in for.
//
// The sentinel count is the load-bearing assertion. Only one corpus spec drives
// the validate pass to a finding today, so without it a corpus that stopped
// producing diagnostics — or a pipeline that stopped folding them in — would
// leave this sweep verifying nothing it exists for, and still passing.
func TestVerify_EngineOutput(t *testing.T) {
	t.Parallel()
	eng, err := engine.New()
	require.NoError(t, err)

	var verified, sentinels int
	for _, f := range corpusSpecs(t) {
		res, err := eng.Run(t.Context(), f, engine.RunOptions{})
		require.NoError(t, err, "%s", f)
		if res.Document == nil {
			continue // the compiler declined the spec, as it does for 18 of the corpus
		}
		verified++
		sentinels += noSourceDiagnostics(res.Document)
		assert.Empty(t, irverify.Verify(res.Document), "%s", f)
	}

	require.Positive(t, verified, "the sweep must reach at least one engine document")
	require.Positive(t, sentinels,
		"no corpus spec drove the validate pass to a finding, so this sweep verified no no-source provenance")
}
