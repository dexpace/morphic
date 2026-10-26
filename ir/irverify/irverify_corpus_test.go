package irverify_test // external test package — importing across layers is legal in tests

import (
	"maps"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi"
	"github.com/dexpace/morphic/internal/harness"
	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/ir/irverify"
)

// compile lowers one OpenAPI source and returns the document, or nil when the
// compilation produced none to verify.
//
// Two different refusals reach that nil. A Go error is an I/O or programmer
// fault and no corpus spec provokes one; a nil document with a clean error is
// the compiler declining a spec it cannot lower, which is how every one of the
// corpus specs that yields nothing here does it. Both leave the oracle with no
// document, which is not the same as a document that passes.
func compile(t testing.TB, name string, data []byte) *ir.Document {
	t.Helper()
	doc, _, err := openapi.New().Compile(t.Context(),
		[]compilers.Source{{Path: name, Data: data}}, compilers.Options{})
	if err != nil {
		return nil
	}
	return doc
}

// corpusSpecs lists every committed spec under testdata, as harness.SpecFiles
// lists a directory.
func corpusSpecs(t testing.TB) []string {
	t.Helper()
	out, err := harness.SpecFiles(t.Context(), "../../testdata")
	require.NoError(t, err)
	require.NotEmpty(t, out, "corpus sweep found no specs")
	return out
}

// TestVerify_Corpus runs the structural oracle over every document the OpenAPI
// compiler produces from the committed corpus, before any pass and outside the
// engine: irverify's own hold on real documents, whatever internal/harness
// does with them. Faulting on a spec is no licence to return a document with a
// dangling reference.
//
// Every dangling fixture is verified, but most of testdata/openapi is cyclic or
// amplifying by construction and is declined outright, so those subtests skip.
// Closing that gap would need a partial document for a refused spec, a compiler
// decision.
func TestVerify_Corpus(t *testing.T) {
	t.Parallel()
	for _, f := range corpusSpecs(t) {
		t.Run(filepath.Base(f), func(t *testing.T) {
			t.Parallel()
			data, err := os.ReadFile(f)
			require.NoError(t, err)
			doc := compile(t, filepath.Base(f), data)
			if doc == nil {
				t.Skip("compiler refused the input")
			}
			assert.Empty(t, irverify.Verify(doc), "%s", f)
		})
	}
}

// uncorpusedUnmodeled exercises the Unmodeled writes no committed fixture
// reaches: path-item servers and an error response's own x-* extension. The
// parameter's xml hints and the `items` an object schema cannot hold are
// reached by the corpus too; they are here so the spec covers every field
// unmodeledKeys reads (the operation, its parameters, its errors and the type
// registry), so a collector that stops reading one fails the precondition below
// rather than narrowing what is verified.
//
// The 404's headers and two media types write no Unmodeled entries, as both
// lower structurally onto ir.ErrorCase (GitHub #422).
const uncorpusedUnmodeled = `openapi: 3.1.0
info: {title: UnmodeledSites, version: "1"}
paths:
  /widgets:
    servers:
      - url: https://widgets.example.com
    get:
      operationId: listWidgets
      parameters:
        - name: tag
          in: query
          schema: {type: string, xml: {name: Tag}}
      responses:
        "200":
          description: ok
          content:
            application/json:
              schema:
                type: object
                properties: {h: {type: string}}
                items: {type: integer}
        "404":
          description: missing
          x-mark: kept
          headers:
            X-Reason: {schema: {type: string}}
          content:
            application/json: {schema: {type: object}}
            text/plain: {schema: {type: string}}
`

// unmodeledKeys returns every Unmodeled key the document carries, at the site
// kinds uncorpusedUnmodeled writes to.
func unmodeledKeys(doc *ir.Document) []string {
	var keys []string
	collect := func(p ir.Unmodeled) {
		keys = slices.AppendSeq(keys, maps.Keys(p))
	}
	for _, svc := range doc.Services {
		for _, g := range svc.Groups {
			for _, op := range g.Operations {
				collect(op.Unmodeled)
				for _, p := range op.Params {
					collect(p.Unmodeled)
				}
				for _, ec := range op.Errors {
					collect(ec.Unmodeled)
				}
			}
		}
	}
	for _, td := range doc.Types {
		collect(td.Common().Unmodeled)
	}
	slices.Sort(keys)
	return keys
}

// TestVerify_UnmodeledSitesOutsideTheCorpus verifies a document built from the
// compiler write paths the committed corpus leaves unreached. A checker only
// sees what some spec makes the compiler write, and measured against the corpus
// sweep alone the preserve calls behind the sites listed above are dead
// statements, so the reason each of them passes is inspected by nothing.
//
// The key assertion is the precondition, not the oracle: without it a compiler
// that stopped writing these entries would leave Verify with nothing to check
// and the test would still pass.
func TestVerify_UnmodeledSitesOutsideTheCorpus(t *testing.T) {
	t.Parallel()
	doc := compile(t, "unmodeled-sites.yaml", []byte(uncorpusedUnmodeled))
	require.NotNil(t, doc)
	require.Equal(t, []string{
		"openapi:items", "openapi:servers", "openapi:x-mark", "openapi:xml",
	}, unmodeledKeys(doc), "the spec must reach every preserve call this test exists for")

	assert.Empty(t, irverify.Verify(doc))
}
