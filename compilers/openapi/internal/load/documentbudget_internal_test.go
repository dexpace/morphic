package load

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/google/go-cmp/cmp"
	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/overlay"
	"github.com/dexpace/morphic/engine/reader"
	"github.com/dexpace/morphic/ir"
)

// endless marks a chainServer chain that never ends, within reason: it ends at
// endlessCap, so a budget that fails to bind fails a test rather than hanging it.
const (
	endless    = -1
	endlessCap = 1000
)

// chainServer serves a chain of documents: n.yaml's x is a $ref to x in
// n+1.yaml, and last.yaml's a string schema, or endlessCap.yaml's where last is
// endless. It counts the requests it answers.
func chainServer(t *testing.T, last int) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var requests atomic.Int32
	srv := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		n, err := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/"), ".yaml"))
		if !assert.NoError(t, err) {
			return
		}
		body := fmt.Sprintf("x: {$ref: './%d.yaml#/x'}\n", n+1)
		if n == last || n == endlessCap {
			body = "x: {type: string}\n"
		}
		_, err = w.Write([]byte(body))
		assert.NoError(t, err)
	})
	return srv, &requests
}

// schemaRoot is a source whose one schema, A, is a $ref to ref.
func schemaRoot(ref string) string {
	return "openapi: 3.1.0\ninfo: {title: T, version: \"1\"}\npaths: {}\n" +
		"components:\n  schemas:\n    A: {$ref: '" + ref + "'}\n"
}

// budgetFindings returns the budget-exceeded diagnostics among diags, and the
// messages of the unresolved-ref ones.
func budgetFindings(diags []ir.Diagnostic) ([]ir.Diagnostic, []string) {
	var budget []ir.Diagnostic
	var unresolved []string
	for _, d := range diags {
		switch d.Code {
		case diag.BudgetExceeded:
			budget = append(budget, d)
		case diag.UnresolvedRef:
			unresolved = append(unresolved, d.Message)
		}
	}
	return budget, unresolved
}

// TestLoad_AnEndlessChainIsReadToTheDocumentBudget pins GitHub #904: a chain
// of documents that never ends makes exactly the budgeted number of requests,
// and the compile reports the budget once, at the source, beside the reference
// whose chain it stopped, which names the first document it did not read.
func TestLoad_AnEndlessChainIsReadToTheDocumentBudget(t *testing.T) {
	t.Parallel()
	srv, requests := chainServer(t, endless)

	_, diags := loadExternal(t, t.TempDir(), schemaRoot(srv.URL+"/0.yaml#/x"), Options{MaxExternalDocuments: 5})

	assert.Equal(t, int32(5), requests.Load(), "exactly the budget is read")
	budget, unresolved := budgetFindings(diags)
	require.Len(t, budget, 1, "%+v", diags)
	assert.Equal(t, ir.SeverityError, budget[0].Severity)
	assert.Equal(t, ir.Provenance{Source: 0}, budget[0].Provenance, "at the source, not a position in it")
	assert.Contains(t, budget[0].Message, "more documents than the 5-document budget")
	require.Len(t, unresolved, 1, "%+v", diags)
	assert.Contains(t, unresolved[0], "external document "+srv.URL+"/5.yaml refused: it is past the 5-document budget")
}

// TestLoad_AChainWithinTheDocumentBudgetIsUnchanged pins both sides of the
// budget's edge on a chain of six documents: at a budget of six, every one is
// read and the load reports what it reports with no budget at all; at five, the
// last is refused.
func TestLoad_AChainWithinTheDocumentBudgetIsUnchanged(t *testing.T) {
	t.Parallel()
	const documents = 6
	load := func(limit int) (int32, []ir.Diagnostic) {
		srv, requests := chainServer(t, documents-1)
		_, diags := loadExternal(t, t.TempDir(), schemaRoot(srv.URL+"/0.yaml#/x"), Options{MaxExternalDocuments: limit})
		return requests.Load(), diags
	}
	unboundedReads, unbounded := load(0)
	require.Equal(t, int32(documents), unboundedReads, "sanity: the chain is six documents long")

	atReads, at := load(documents)
	pastReads, past := load(documents - 1)

	assert.Equal(t, int32(documents), atReads)
	if d := cmp.Diff(unbounded, at); d != "" {
		t.Errorf("a budget the chain fits changed the load (-unbounded +budgeted):\n%s", d)
	}
	assert.Equal(t, int32(documents-1), pastReads)
	budget, _ := budgetFindings(past)
	assert.Len(t, budget, 1, "one document short of the chain, the budget binds")
}

// TestLoad_TheDocumentBudgetCountsFilesAndURLsAlike pins the count's reach: a
// chain through a file and on through URLs spends one budget, so a budget of
// four reads the file and three URLs, and refuses the fourth.
func TestLoad_TheDocumentBudgetCountsFilesAndURLsAlike(t *testing.T) {
	t.Parallel()
	srv, requests := chainServer(t, endless)
	dir := externalDir(t, map[string]string{"local.yaml": "x: {$ref: '" + srv.URL + "/0.yaml#/x'}\n"})

	_, diags := loadExternal(t, dir, schemaRoot("./local.yaml#/x"), Options{MaxExternalDocuments: 4})

	assert.Equal(t, int32(3), requests.Load(), "the file takes one of the four")
	budget, unresolved := budgetFindings(diags)
	assert.Len(t, budget, 1)
	require.Len(t, unresolved, 1, "%+v", diags)
	assert.Contains(t, unresolved[0], "external document "+srv.URL+"/3.yaml refused")
}

// TestLoad_TheDocumentBudgetRefusesAFileChainToo pins the budget on a chain of
// files alone: five files under a budget of three read three, and the fourth
// is refused, named by its path.
func TestLoad_TheDocumentBudgetRefusesAFileChainToo(t *testing.T) {
	t.Parallel()
	files := map[string]string{}
	for i := range 5 {
		files[fmt.Sprintf("f%d.yaml", i)] = fmt.Sprintf("x: {$ref: './f%d.yaml#/x'}\n", i+1)
	}
	dir := externalDir(t, files)

	_, diags := loadExternal(t, dir, schemaRoot("./f0.yaml#/x"), Options{MaxExternalDocuments: 3})

	budget, unresolved := budgetFindings(diags)
	assert.Len(t, budget, 1)
	require.Len(t, unresolved, 1, "%+v", diags)
	assert.Contains(t, unresolved[0], "external document "+filepath.Join(dir, "f3.yaml")+" refused: "+
		"it is past the 3-document budget")
}

// resolveCounting resolves spec at path as resolveSpec does, under opts with
// external references allowed, and reports how many times the document was
// rebuilt for a second pass.
func resolveCounting(t *testing.T, spec, path string, opts Options) ([]ir.Diagnostic, int) {
	t.Helper()
	data := []byte(spec)
	root, _, err := decodeStream(data)
	require.NoError(t, err)
	releaseAnchors(root)
	doc, _, err := unmarshal(t.Context(), data, root)
	require.NoError(t, err)
	rebuilds := 0
	rebuild := func() (*soa.OpenAPI, error) {
		rebuilds++
		again, _, err := unmarshal(t.Context(), data, root)
		return again, err
	}
	opts.Reader = reader.Disk{}
	_, _, _, diags, err := resolveExternal(t.Context(), pointerAt(0, overlay.Origin{}), doc, Siblings{},
		newSourceDocument(path, data, root, nil, newSourceTree(0, overlay.Origin{}, root), nil), opts, rebuild)
	require.NoError(t, err)
	return diags, rebuilds
}

// TestResolveExternal_BothPassesShareOneDocumentBudget pins that GitHub #538's
// second pass reads nothing the first read, files included, so it spends none
// of the budget again: a respelled anchored document and a file fit a budget
// of two through both passes, and under a budget of one the budget is reported
// once and the file, asked for second, is refused, as in the first pass.
func TestResolveExternal_BothPassesShareOneDocumentBudget(t *testing.T) {
	t.Parallel()
	trigger, requests := countingServer(t, anchoredExternalDoc)
	dir := externalDir(t, map[string]string{"local.yaml": "components:\n  schemas:\n    S: {type: string}\n"})
	spec := rootReferencing(respelled(trigger.URL, "HTTP")+"/ext.yaml") +
		"components:\n  schemas:\n    A: {$ref: './local.yaml#/components/schemas/S'}\n"
	path := filepath.Join(dir, "root.yaml")

	fits, rebuilds := resolveCounting(t, spec, path, Options{MaxExternalDocuments: 2})

	require.Equal(t, 1, rebuilds, "sanity: the respelled anchored document sets the second pass off")
	assert.Empty(t, fits, "both documents fit the budget in both passes")
	assert.Equal(t, int32(1), requests.Load())

	past, rebuilds := resolveCounting(t, spec, path, Options{MaxExternalDocuments: 1})

	require.Equal(t, 1, rebuilds, "sanity: the anchored document, asked for first, is read")
	budget, unresolved := budgetFindings(past)
	assert.Len(t, budget, 1, "reported once, though both passes ran: %+v", past)
	require.Len(t, unresolved, 1, "%+v", past)
	assert.Contains(t, unresolved[0], filepath.Join(dir, "local.yaml")+" refused: it is past the 1-document budget")
}
