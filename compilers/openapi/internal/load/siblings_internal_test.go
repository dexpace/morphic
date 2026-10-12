package load

import (
	"context"
	"encoding/json/jsontext"
	"errors"
	"iter"
	"os"
	"path/filepath"
	"strings"
	"testing"

	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/scan"
	"github.com/dexpace/morphic/ir"
)

// siblingSpec writes fields beside a path item's $ref at /a, aliases that
// mapping at /d, writes a $ref alone at /c and one that only a merge key
// supplies at /m, and nests a second $ref with fields of its own inside /a's
// operation's callback.
const siblingSpec = `openapi: 3.1.0
info: {title: T, version: "1"}
x-base: &base {$ref: '#/paths/~1b'}
paths:
  /a: &a
    $ref: '#/paths/~1b'
    summary: beside
    put:
      callbacks:
        cb:
          '{$request.body#/url}':
            $ref: '#/paths/~1b'
            delete: {responses: {"200": {description: ok}}}
      responses: {"200": {description: ok}}
  /b:
    get: {responses: {"200": {description: ok}}}
  /c: {$ref: '#/paths/~1b'}
  /d: *a
  /m:
    <<: *base
    post: {responses: {"200": {description: ok}}}
`

// pathEntry returns the reference-or-inline entry doc's paths hold at path.
func pathEntry(t *testing.T, doc *soa.OpenAPI, path string) *soa.ReferencedPathItem {
	t.Helper()
	rp, ok := doc.GetPaths().Get(path)
	require.True(t, ok, "the document declares %s", path)
	return rp
}

// TestModelSiblings_ModelsWhatTheLibraryReadsAsAReference pins which use sites
// get a model: every $ref the library reads as one that writes fields beside
// it, once per mapping however many aliases reach it, and one nested inside
// another's model, located where the source writes it.
func TestModelSiblings_ModelsWhatTheLibraryReadsAsAReference(t *testing.T) {
	t.Parallel()
	doc := modelOf(t, siblingSpec)
	sibs, errs, faults := modelSiblings(t.Context(), doc, scan.InSource(0))
	require.Empty(t, faults)
	require.Empty(t, errs, "the fields beside the $refs are valid")

	a, ok := sibs.Of(pathEntry(t, doc, "/a"))
	require.True(t, ok)
	assert.Equal(t, jsontext.Pointer("/paths/~1a"), a.Pointer)
	assert.Equal(t, "beside", a.Item.GetSummary())
	require.NotNil(t, a.Item.Put(), "the operation beside the $ref is modelled")

	d, ok := sibs.Of(pathEntry(t, doc, "/d"))
	require.True(t, ok, "an alias reaches the same mapping")
	assert.Same(t, a.Item, d.Item, "and the same model, built once")

	_, ok = sibs.Of(pathEntry(t, doc, "/c"))
	assert.False(t, ok, "a $ref with nothing beside it has no model")
	_, ok = sibs.Of(pathEntry(t, doc, "/b"))
	assert.False(t, ok, "an inline item is no reference")
	_, ok = sibs.Of(pathEntry(t, doc, "/m"))
	assert.False(t, ok, "a $ref only a merge key supplies is not one the library reads")

	cb, ok := a.Item.Put().GetCallbacks().Get("cb")
	require.True(t, ok)
	nested, ok := cb.GetObject().Get("{$request.body#/url}")
	require.True(t, ok)
	inner, ok := sibs.Of(nested)
	require.True(t, ok, "a $ref inside a model is modelled too")
	assert.Equal(t, jsontext.Pointer("/paths/~1a/put/callbacks/cb/{$request.body#~1url}"), inner.Pointer)
	assert.NotNil(t, inner.Item.Delete())
}

// TestModelSiblings_ValidatesAsTheSourceIs pins that the fields beside a $ref
// are validated as the document's own are: a value of the wrong type is a
// finding at the node that wrote it, which the model build alone does not make.
func TestModelSiblings_ValidatesAsTheSourceIs(t *testing.T) {
	t.Parallel()
	doc := modelOf(t, `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /a:
    $ref: '#/paths/~1b'
    put:
      deprecated: notabool
      parameters: [{name: q, in: query, schema: {type: string}}]
  /b:
    get: {responses: {"200": {description: ok}}}
`)
	_, errs, _ := modelSiblings(t.Context(), doc, scan.InSource(0))
	rules := make([]string, 0, len(errs))
	for _, e := range errs {
		verr, ok := asValidationError(e)
		require.True(t, ok)
		rules = append(rules, verr.Rule)
	}
	assert.Contains(t, rules, "validation-type-mismatch", "the model build reads the value as the source's is read")
	assert.Contains(t, rules, "validation-required-field", "and validation finds what the build cannot: no responses")
}

// TestSiblingsFrom_AParserFaultIsReportedOnce pins what a use site the parser
// faults on leaves behind: an error where it is written, reported once however
// many aliases reach it, and no model, so the lowering reads it as having none.
func TestSiblingsFrom_AParserFaultIsReportedOnce(t *testing.T) {
	t.Parallel()
	doc := modelOf(t, siblingSpec)
	faulting := func(context.Context, *yaml.Node) (*soa.PathItem, []error, error) {
		return nil, nil, errors.New("parser panicked: boom")
	}
	sibs, errs, faults := siblingsFrom(t.Context(), soa.Walk(t.Context(), doc), scan.InSource(0), faulting)
	assert.Empty(t, errs)
	require.Len(t, faults, 1, "/a and its alias /d are one mapping")
	assert.Equal(t, diag.Validation, faults[0].Code)
	assert.Equal(t, ir.SeverityError, faults[0].Severity)
	assert.Contains(t, faults[0].Message, "parser panicked: boom")
	assert.Equal(t, 5, faults[0].Provenance.Position.Line, "sited where the use site is written")

	rp := pathEntry(t, doc, "/a")
	_, ok := sibs.Of(rp)
	assert.False(t, ok, "a faulted mapping holds no model")
	assert.False(t, sibs.Unread(rp), "and is not reported again as unread")
}

// TestSiblingsFrom_StopsWhereAWalkPanics pins the barrier around the search: a
// walk the library faults in stops it, keeping what it found, and the panic is
// left to the resolution walk to report.
func TestSiblingsFrom_StopsWhereAWalkPanics(t *testing.T) {
	t.Parallel()
	doc := modelOf(t, siblingSpec)
	panicking := func(yield func(soa.WalkItem) bool) {
		for item := range soa.Walk(t.Context(), doc) {
			if !yield(item) {
				return
			}
			if item.Location.ToJSONPointer() == "/paths/~1a" {
				panic("walk faulted")
			}
		}
	}
	var sibs Siblings
	require.NotPanics(t, func() {
		sibs, _, _ = siblingsFrom(t.Context(), panicking, scan.InSource(0), siblingModel(doc))
	})
	_, ok := sibs.Of(pathEntry(t, doc, "/a"))
	assert.True(t, ok, "what it found before the fault is kept")
	assert.Len(t, sibs.list, 1, "and nothing after it is searched")
}

// TestSiblingModel_FaultsAreErrorsNotPanics pins that the library failing on
// the fields beside a $ref returns as an error rather than escaping.
func TestSiblingModel_FaultsAreErrorsNotPanics(t *testing.T) {
	t.Parallel()
	build := siblingModel(modelOf(t, siblingSpec))
	var err error
	require.NotPanics(t, func() { _, _, err = build(t.Context(), nil) })
	require.Error(t, err, "a nil node faults the parser")
}

// TestSiblings_UnreadIsFieldsNoModelHolds pins Unread: a reference writing
// fields beside its $ref that no model holds, which a set built from another
// document's walk leaves every one of this document's.
func TestSiblings_UnreadIsFieldsNoModelHolds(t *testing.T) {
	t.Parallel()
	doc := modelOf(t, siblingSpec)
	var none Siblings
	assert.True(t, none.Unread(pathEntry(t, doc, "/a")))
	assert.False(t, none.Unread(pathEntry(t, doc, "/c")), "nothing is written beside the $ref")
	assert.False(t, none.Unread(pathEntry(t, doc, "/b")), "an inline item is no reference")
	assert.False(t, none.Unread(&soa.ReferencedPathItem{}), "a wrapper with no node writes nothing")
	assert.False(t, none.Unread(nil), "nor does no wrapper at all")
	_, ok := none.Of(nil)
	assert.False(t, ok)

	sibs, _, _ := modelSiblings(t.Context(), doc, scan.InSource(0))
	assert.False(t, sibs.Unread(pathEntry(t, doc, "/a")), "a modelled use site is read")
}

// TestWithoutRef_DropsTheRefAsTheLibraryReadsIt pins the copy a model is built
// from: every $ref pair, an alias key included, is gone, the position and the
// document's own nodes stay, and a mapping holding nothing else has none.
func TestWithoutRef_DropsTheRefAsTheLibraryReadsIt(t *testing.T) {
	t.Parallel()
	var root yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte(`k: &k $ref
m: {*k : '#/x', summary: s, $ref: '#/y'}
only: {$ref: '#/x'}
s: scalar
`), &root))
	top := root.Content[0]
	m, only, scalar := top.Content[3], top.Content[5], top.Content[7]

	fields, ok := withoutRef(m)
	require.True(t, ok)
	require.Len(t, fields.Content, 2, "both spellings of $ref are dropped")
	assert.Equal(t, "summary", fields.Content[0].Value)
	assert.Same(t, m.Content[2], fields.Content[0], "the document's own key node is kept")
	assert.Equal(t, m.Line, fields.Line)

	_, ok = withoutRef(only)
	assert.False(t, ok, "a pure reference has no fields")
	_, ok = withoutRef(scalar)
	assert.False(t, ok, "a scalar is no mapping")
}

// TestLocationOf_RendersThePointer pins the round trip a sibling walk relies on
// for every site it reports: the location rendered back is the pointer, escapes
// and empty tokens included.
func TestLocationOf_RendersThePointer(t *testing.T) {
	t.Parallel()
	for _, p := range []jsontext.Pointer{
		"/paths/~1a",
		"/paths/~1a~0b/put/callbacks/cb/{$request.body#~1url}",
		"/paths//get",
		"/webhooks/hook",
	} {
		assert.Equal(t, string(p), string(locationOf(p).ToJSONPointer()))
	}
}

// walkSites returns the site of every item items yields.
func walkSites(items iter.Seq[soa.WalkItem]) []string {
	var sites []string
	for item := range items {
		sites = append(sites, string(item.Location.ToJSONPointer()))
	}
	return sites
}

// TestWalkSource_WalksTheFieldsWhereTheSourceWritesThem pins the order every
// pass reads the source in: a model's items right after its $ref, before the
// next path, as an inline item's would be, each model once though an alias
// reaches it twice, and no item for the wrapper a model's walk starts from.
func TestWalkSource_WalksTheFieldsWhereTheSourceWritesThem(t *testing.T) {
	t.Parallel()
	doc := modelOf(t, siblingSpec)
	sibs, _, _ := modelSiblings(t.Context(), doc, scan.InSource(0))
	sites := walkSites(walkSource(t.Context(), doc, sibs))

	index := func(site string) int {
		t.Helper()
		for i, s := range sites {
			if s == site {
				return i
			}
		}
		require.Failf(t, "site not walked", "%s", site)
		return -1
	}
	put := index("/paths/~1a/put")
	assert.Less(t, index("/paths/~1a"), put)
	assert.Less(t, put, index("/paths/~1b"), "the fields are walked before the next path")
	assert.Less(t, put, index("/paths/~1a/put/callbacks/cb/{$request.body#~1url}/delete"))
	count := 0
	for item := range walkSource(t.Context(), doc, sibs) {
		_ = item.Match(soa.Matcher{Operation: func(op *soa.Operation) error {
			if op == sibs.list[0].Item.Put() {
				count++
			}
			return nil
		}})
	}
	assert.Equal(t, 1, count, "a model is walked once, where it was found, though an alias reaches it twice")
	assert.Equal(t, walkSites(soa.Walk(t.Context(), doc)), walkSites(walkSource(t.Context(), doc, Siblings{})),
		"with no model the walk is the document's own")
}

// TestWalkSource_StopsWhenTheReaderDoes pins that a pass leaving the walk early,
// as one stopping at a fault does, stops it inside a model as well as outside.
func TestWalkSource_StopsWhenTheReaderDoes(t *testing.T) {
	t.Parallel()
	doc := modelOf(t, siblingSpec)
	sibs, _, _ := modelSiblings(t.Context(), doc, scan.InSource(0))
	for _, stopAt := range []string{"/paths/~1b", "/paths/~1a/put"} {
		var last string
		for item := range walkSource(t.Context(), doc, sibs) {
			last = string(item.Location.ToJSONPointer())
			if last == stopAt {
				break
			}
		}
		assert.Equal(t, stopAt, last)
	}
}

// loadSiblingFixture writes main beside a target document and loads main with
// external references allowed under limits.
func loadSiblingFixture(t *testing.T, main, target string, opts Options) []ir.Diagnostic {
	t.Helper()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "target.yaml"), []byte(target), 0o600))
	path := filepath.Join(dir, "main.yaml")
	opts.AllowExternalRefs = true
	_, diags, err := Load(t.Context(), 0, compilers.Source{Path: path, Data: []byte(main)}, opts)
	require.NoError(t, err)
	return diags
}

// siblingExternalMain writes, beside a path item's $ref, an operation whose
// parameter is a $ref to target.yaml.
const siblingExternalMain = `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /a:
    $ref: '#/paths/~1b'
    put:
      parameters: [{$ref: './target.yaml#/components/parameters/Ext'}]
      responses: {"200": {description: ok}}
  /b:
    get: {responses: {"200": {description: ok}}}
`

// TestLoad_ASiblingsExternalReferenceIsReadAsAnyOther pins that a reference
// written beside a path item's $ref leaves the document through the load
// phase's own reader: refused for a recursive anchor and past the byte budget,
// and its findings kept at their own severity, each reported once, at the
// reference.
func TestLoad_ASiblingsExternalReferenceIsReadAsAnyOther(t *testing.T) {
	t.Parallel()
	const site = jsontext.Pointer("/paths/~1a/put/parameters/0")
	param := "openapi: 3.1.0\ninfo: {title: X, version: \"1\"}\npaths: {}\ncomponents:\n  parameters:\n    Ext:\n"
	cases := []struct {
		name, target string
		opts         Options
		code         string
		severity     ir.Severity
		says         string
	}{
		{"recursive anchor", param + "      name: q\n      in: query\n      schema: &s {type: object, properties: {self: *s}}\n",
			Options{}, diag.UnresolvedRef, ir.SeverityError, "recursive YAML anchor"},
		{"byte budget", param + "      name: q\n      in: query\n      description: " + strings.Repeat("x", len(siblingExternalMain)) + "\n",
			Options{MaxSourceBytes: len(siblingExternalMain)}, diag.UnresolvedRef, ir.SeverityError, "byte budget"},
		{"duplicate key", param + "      name: q\n      in: query\n      description: one\n      description: two\n",
			Options{}, diag.Validation + "/validation-duplicate-key", ir.SeverityWarning, "duplicate"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var at []ir.Diagnostic
			for _, d := range loadSiblingFixture(t, siblingExternalMain, tc.target, tc.opts) {
				if d.Provenance.Pointer == site {
					at = append(at, d)
				}
			}
			require.Len(t, at, 1, "one finding, at the reference written beside the $ref")
			assert.Equal(t, tc.code, at[0].Code)
			assert.Equal(t, tc.severity, at[0].Severity)
			assert.Contains(t, at[0].Message, tc.says)
		})
	}
}
