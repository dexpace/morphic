// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package engine_test

import (
	"encoding/json/v2"
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi"
	"github.com/dexpace/morphic/engine"
)

// TestPasses_KeepTheContractOverTheCorpus holds every pass a default
// engine runs, whichever are added later, to the half of the Pass contract the
// engine cannot check as it runs: each leaves the document it was handed
// unchanged. A transform that copies a Document and writes into a map or slice
// the copy still shares breaks that silently, and it is the likeliest way to.
//
// The specs run one after another so the count of those that reached a pass
// needs no synchronisation; a corpus that compiled to nothing would otherwise
// pass for want of anything to check.
func TestPasses_KeepTheContractOverTheCorpus(t *testing.T) {
	t.Parallel()
	compiler, err := engine.New(engine.WithCompilers(openapi.New()), engine.WithPasses())
	require.NoError(t, err)

	reached := 0
	for _, path := range corpusSpecFiles(t, "../testdata") {
		t.Run(filepath.Base(path), func(t *testing.T) {
			res, err := compiler.Run(t.Context(), path, engine.RunOptions{})
			require.NoError(t, err)
			if res.Document == nil {
				return // refused: the engine runs no pass on it
			}
			reached++

			doc := res.Document
			for _, p := range engine.Passes() {
				before, err := json.Marshal(doc)
				require.NoError(t, err)

				next, _ := p.Run(t.Context(), doc)

				require.NotNil(t, next, "pass %q returned no document", p.Name())
				after, err := json.Marshal(doc)
				require.NoError(t, err)
				assert.Empty(t, cmp.Diff(string(before), string(after)), "pass %q mutated its input", p.Name())
				doc = next
			}
		})
	}
	assert.Positive(t, reached, "no corpus spec compiled to a document for the passes to run on")
}
