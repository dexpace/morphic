package irverify_test // external test package — importing across layers is legal in tests

import (
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi"
	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/ir/irverify"
)

// compile lowers one OpenAPI source and returns the document, or nil when the
// compiler refused the input outright (a Go error rather than a diagnostic).
func compile(t *testing.T, name string, data []byte) *ir.Document {
	t.Helper()
	doc, _, err := openapi.New().Compile(t.Context(),
		[]compilers.Source{{Path: name, Data: data}}, compilers.Options{})
	if err != nil {
		return nil
	}
	return doc
}

// corpusSpecs lists every committed spec under testdata, excluding golden IR
// snapshots, in WalkDir's lexical order.
func corpusSpecs(t *testing.T) []string {
	t.Helper()
	var out []string
	err := filepath.WalkDir(filepath.FromSlash("../../testdata"), func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || strings.HasSuffix(p, ".golden.json") {
			return err
		}
		switch strings.ToLower(filepath.Ext(p)) {
		case ".yaml", ".yml", ".json":
			out = append(out, p)
		}
		return nil
	})
	require.NoError(t, err)
	require.NotEmpty(t, out, "corpus sweep found no specs")
	return out
}

// TestVerify_Corpus runs the structural oracle over every document the OpenAPI
// compiler produces from the committed corpus. Without it the checks here only
// ever see hand-built fixtures, and a compiler that writes a malformed document
// is caught by nothing.
//
// It verifies documents the compiler faulted on as well as clean ones, which is
// what it adds over the harness sweep in internal/harness: harness.Check returns
// at the first error diagnostic and never reaches irverify.Verify, so the
// deliberately-broken fixtures under testdata/dangling and testdata/openapi are
// unverified there. Refusing a spec is no licence to hand back a document with a
// dangling reference in it — that is the invariant those fixtures exist for.
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

// uncorpusedPreserved exercises the four Preserved writes no committed fixture
// reaches: path-item servers, an error response's headers, an error response's
// second media type, and an `items` tail after `prefixItems`.
const uncorpusedPreserved = `openapi: 3.1.0
info: {title: PreservedSites, version: "1"}
paths:
  /widgets:
    servers:
      - url: https://widgets.example.com
    get:
      operationId: listWidgets
      responses:
        "200":
          description: ok
          content:
            application/json:
              schema:
                type: array
                prefixItems: [{type: string}]
                items: {type: integer}
        "404":
          description: missing
          headers:
            X-Reason: {schema: {type: string}}
          content:
            application/json: {schema: {type: object}}
            text/plain: {schema: {type: string}}
`

// preservedKeys returns every Preserved key the document carries, at the three
// site kinds uncorpusedPreserved writes to.
func preservedKeys(doc *ir.Document) []string {
	var keys []string
	collect := func(p ir.Preserved) {
		for k := range p {
			keys = append(keys, k)
		}
	}
	for _, svc := range doc.Services {
		for _, g := range svc.Groups {
			for _, op := range g.Operations {
				collect(op.Preserved)
				for _, ec := range op.Errors {
					collect(ec.Preserved)
				}
			}
		}
	}
	for _, td := range doc.Types {
		collect(td.Common().Preserved)
	}
	slices.Sort(keys)
	return keys
}

// TestVerify_PreservedSitesOutsideTheCorpus verifies a document built from the
// compiler write paths the committed corpus leaves unreached. A checker only
// sees what some spec makes the compiler write: measured against the corpus
// sweep alone, four of the compiler's preserve calls are dead statements, so the
// reason each of them passes is inspected by nothing. This spec reaches all
// four.
//
// The key assertion is the precondition, not the oracle: without it a compiler
// that stopped writing these entries would leave Verify with nothing to check
// and the test would still pass.
func TestVerify_PreservedSitesOutsideTheCorpus(t *testing.T) {
	t.Parallel()
	doc := compile(t, "preserved-sites.yaml", []byte(uncorpusedPreserved))
	require.NotNil(t, doc)
	require.Equal(t, []string{
		"openapi:content", "openapi:headers", "openapi:items-after-prefix", "openapi:servers",
	}, preservedKeys(doc), "the spec must reach every preserve call this test exists for")

	assert.Empty(t, irverify.Verify(doc))
}
