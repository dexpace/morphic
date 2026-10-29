package irverify_test // external test package — importing across layers is legal in tests

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi"
	"github.com/dexpace/morphic/internal/harness"
	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/ir/irtest"
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

// TestWalkChecks_ACleanValueCostsNothing holds what Verify's walk carries, the
// declarations and every walking check, together, to costing nothing for a
// value none reports on, in a compiled document. extensions-x.yaml writes an
// extension on every object OpenAPI admits one on, so its document reaches
// nearly every node a compile produces, and holds none of the costs Verify's
// doc excepts. It is not run in parallel: AllocsPerRun measures the process.
func TestWalkChecks_ACleanValueCostsNothing(t *testing.T) {
	data, err := os.ReadFile("../../testdata/conformance/openapi/extensions-x.yaml")
	require.NoError(t, err)
	doc := compile(t, "extensions-x.yaml", data)
	require.NotNil(t, doc)
	require.Empty(t, irverify.Verify(doc), "the document is clean, or what is measured is a report")

	assert.Empty(t, irtest.AllocatingVisits(doc, irverify.WalkVisitors(doc)...))
}

// TestVerify_ACleanComponentCostsNothing holds all Verify does to costing
// nothing for a value it finds clean, including what irtest.AllocatingVisits
// never measures: a check's first meeting with a value, and its report once
// the walk is over. A check keeping a path per PropID it meets, or spelling
// every declaration's path to report a few, passes the test above. Twice the
// components must cost less than one allocation more per component added, the
// walk's growth aside; a map or a log a check keeps grows in steps. It is not
// run in parallel: AllocsPerRun measures the process.
func TestVerify_ACleanComponentCostsNothing(t *testing.T) {
	const components = 100
	few, many := scaledDocument(t, components), scaledDocument(t, 2*components)

	added := verifyingAllocs(many) - verifyingAllocs(few)
	assert.Less(t, added, float64(components), "%v allocations more for %d components more", added, components)
}

// scaledDocument compiles a spec of n components, each an operation and the
// schema it returns, which reach every walking check, and requires the
// document to be clean.
func scaledDocument(t *testing.T, n int) *ir.Document {
	t.Helper()
	var spec strings.Builder
	spec.WriteString("openapi: 3.1.0\ninfo: {title: scaled, version: '1'}\npaths:\n")
	for i := range n {
		fmt.Fprintf(&spec, `  /pets%[1]d/{id}:
    get:
      operationId: getPet%[1]d
      tags: [pets%[1]d]
      x-kept: {a: [1, b]}
      parameters:
        - {name: id, in: path, required: true, schema: {type: string}}
        - {name: q, in: query, schema: {type: integer, minimum: 1, maximum: 2.5e3}}
      responses:
        '200': {description: ok, content: {application/json: {schema: {$ref: '#/components/schemas/Pet%[1]d'}}}}
        '404': {$ref: '#/components/responses/Missing'}
`, i)
	}
	spec.WriteString("components:\n  responses:\n    Missing:\n      description: missing\n" +
		"      headers: {X-Rate: {schema: {type: integer}}}\n  schemas:\n")
	for i := range n {
		fmt.Fprintf(&spec, `    Pet%d:
      type: object
      x-kept: {a: [1, b]}
      required: [name]
      properties:
        name: {type: string, maxLength: 10, example: rex}
        age: {type: number, minimum: 0, maximum: 1.5e3, multipleOf: 0.5}
        kind: {type: string, enum: [cat, dog]}
        tags: {type: array, items: {type: string}}
`, i)
	}
	doc := compile(t, "scaled.yaml", []byte(spec.String()))
	require.NotNil(t, doc)
	require.Empty(t, irverify.Verify(doc), "the document is clean, or what is measured is a report")
	return doc
}

// verifyingAllocs returns what one Verify of doc allocates, less what a walk of
// doc allocates visiting nothing.
func verifyingAllocs(doc *ir.Document) float64 {
	verifying := testing.AllocsPerRun(5, func() { _ = irverify.Verify(doc) })
	walking := testing.AllocsPerRun(5, func() {
		ir.WalkValues(doc, ir.DocumentPath, ir.VisitFunc(func(reflect.Value, ir.WalkPath) bool { return true }))
	})
	return verifying - walking
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
