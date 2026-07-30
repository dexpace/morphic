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
// compilation produced none to verify.
//
// Two different refusals reach that nil. A Go error is an I/O or programmer
// fault and no corpus spec provokes one; a nil document with a clean error is
// the compiler declining a spec it cannot lower, which is how every one of the
// corpus specs that yields nothing here does it. Both leave the oracle with no
// document, which is not the same as a document that passes.
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
// compiler produces from the committed corpus.
//
// internal/harness already verifies compiler output, so this is not the first
// thing to do so. What it adds is the specs harness.Check never reaches:
// harness.Check returns at the first error diagnostic, before irverify.Verify,
// so every deliberately-broken fixture under testdata/dangling goes unverified
// there. Faulting on a spec is no licence to hand back a document with a
// dangling reference in it — that is the invariant those fixtures exist for.
//
// The reach is narrower than the sweep looks, and worth stating rather than
// leaving to be rediscovered. Every dangling fixture yields a document and is
// verified. Most of testdata/openapi does not: those specs are cyclic or
// amplifying by construction and the compiler declines them outright, so the
// subtest skips with nothing to check. Closing that gap needs the compiler to
// emit a partial document for a spec it refuses, which is a decision about the
// compiler and not something a checker can assert its way into.
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
// reaches: path-item servers, an error response's headers, an error response's
// second media type, and an `items` tail after `prefixItems`. The parameter's
// xml hints are reached by the corpus too, and are here so the spec covers every
// field unmodeledKeys reads — the operation itself, its parameters, its errors
// and the type registry — leaving a collector that stopped reading one of them
// to fail the precondition below rather than quietly narrow what is verified.
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

// unmodeledKeys returns every Unmodeled key the document carries, at the site
// kinds uncorpusedUnmodeled writes to.
func unmodeledKeys(doc *ir.Document) []string {
	var keys []string
	collect := func(p ir.Unmodeled) {
		for k := range p {
			keys = append(keys, k)
		}
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
		"openapi:content", "openapi:headers", "openapi:items-after-prefix",
		"openapi:servers", "openapi:xml",
	}, unmodeledKeys(doc), "the spec must reach every preserve call this test exists for")

	assert.Empty(t, irverify.Verify(doc))
}
