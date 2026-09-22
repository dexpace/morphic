// This file is a package-level suite, not a per-source-file test: it sweeps the
// whole committed corpus through the compiler twice and compares the results,
// so it has no single source file to pair with.
package openapi_test // external test package — exercises only the public API

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi"
)

// TestReuse_MatchesAFreshParseAcrossTheCorpus sweeps every committed spec
// through both paths a compile can take and requires one answer.
//
// The paths are not equally travelled. A caller that assembles a Source itself
// — which is what every golden, conformance, budget, overlay and fuzz driver in
// this repository does, and what internal/harness drives its oracles through —
// parses the bytes inside Compile. A caller that goes through detection, which
// is every invocation of the CLI, hands over the parse detection already made
// and Compile reads that instead. So the oracles sweep the path no shipped
// invocation takes, and the path every shipped invocation takes is swept by
// nothing.
//
// This closes that: for each spec, compile it bare and compile it again with
// the parse its own Detect produced, and compare the documents as their
// persisted JSON plus the diagnostics beside them. Equality is what the reuse
// is for — the parse is an optimization, so a document that differs is a bug
// whichever side is right.
func TestReuse_MatchesAFreshParseAcrossTheCorpus(t *testing.T) {
	t.Parallel()
	specs := corpusSpecs(t)
	require.NotEmpty(t, specs, "the sweep found no specs")

	for _, path := range specs {
		t.Run(filepath.Base(path), func(t *testing.T) {
			t.Parallel()
			data, err := os.ReadFile(path)
			require.NoError(t, err)

			fresh, freshDiags, freshErr := openapi.New().Compile(t.Context(),
				[]compilers.Source{{Path: path, Data: data}}, compilers.Options{})

			// A second buffer, because a compile writes through the tree it is
			// given and the bytes must not be shared between the two runs.
			reusedData := append([]byte(nil), data...)
			src := compilers.Source{Path: path, Data: reusedData}
			rec, _, ok := openapi.New().Detect(src)
			if !ok {
				t.Skip("the corpus holds sources this compiler declines; they never reach Compile through an engine")
			}
			src.Parsed = rec.Parsed
			reused, reusedDiags, reusedErr := openapi.New().Compile(t.Context(),
				[]compilers.Source{src}, compilers.Options{})

			assert.Equal(t, freshErr, reusedErr, "the two paths agree on whether the source is readable")
			assert.Equal(t, freshDiags, reusedDiags, "and on what they have to say about it")
			assert.Equal(t, encodeDoc(t, fresh), encodeDoc(t, reused),
				"a reused parse lowers the document a fresh one does")
		})
	}
}

// encodeDoc renders a document as the JSON it persists as, or "" for none. The
// comparison is over that rather than the struct because it is what a consumer
// stores, and because two documents equal as JSON are equal for every use the
// IR is put to (invariant 7).
func encodeDoc(t *testing.T, doc any) string {
	t.Helper()
	if doc == nil {
		return ""
	}
	encoded, err := json.Marshal(doc)
	require.NoError(t, err)
	return string(encoded)
}
