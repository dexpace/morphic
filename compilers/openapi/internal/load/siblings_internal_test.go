package load

import (
	"context"
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"iter"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/speakeasy-api/openapi/marshaller"
	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/scan"
	"github.com/dexpace/morphic/engine/reader"
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
	sibs, faults := modelSiblings(t.Context(), doc, scan.InSource(0))
	require.Empty(t, faults)

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

// TestLoad_TheFieldsBesideAReferenceAreTheItemsOwnToTheTable pins that the
// fields beside a path item's $ref are held to the table as the item's own: a
// value of the wrong type is a finding at the node that wrote it, once however
// the library models it.
func TestLoad_TheFieldsBesideAReferenceAreTheItemsOwnToTheTable(t *testing.T) {
	t.Parallel()
	for _, version := range []string{"3.0.3", "3.1.0"} {
		doc, diags, err := Load(t.Context(), 0, compilers.Source{Path: "spec.yaml", Data: []byte(`openapi: ` + version + `
info: {title: T, version: "1"}
paths:
  /a:
    $ref: '#/paths/~1b'
    summary: [s]
    description: [d]
    put: {deprecated: notabool, responses: {"200": {description: ok}}}
  /b:
    get: {responses: {"200": {description: ok}}}
`)}, Options{})
		require.NoError(t, err)
		require.NotNil(t, doc)

		lines := map[int]int{}
		for _, d := range diags {
			if d.Code == diag.Validation+"/validation-type-mismatch" {
				lines[d.Provenance.Position.Line]++
			}
		}
		assert.Equal(t, map[int]int{6: 1, 7: 1, 8: 1}, lines,
			"%s: the summary, the description and the operation's flag, once each", version)
	}
}

// TestSiblingsFrom_AParserFaultIsReportedOnce pins what a use site the parser
// faults on leaves behind: an error where it is written, reported once however
// many aliases reach it, and no model, so the lowering reads it as having none.
func TestSiblingsFrom_AParserFaultIsReportedOnce(t *testing.T) {
	t.Parallel()
	doc := modelOf(t, siblingSpec)
	faulting := func(context.Context, *yaml.Node) (*soa.PathItem, error) {
		return nil, errors.New("parser panicked: boom")
	}
	sibs, faults := siblingsFrom(t.Context(), soa.Walk(t.Context(), doc), scan.InSource(0), faulting)
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
		sibs, _ = siblingsFrom(t.Context(), panicking, scan.InSource(0), buildSibling)
	})
	_, ok := sibs.Of(pathEntry(t, doc, "/a"))
	assert.True(t, ok, "what it found before the fault is kept")
	assert.Len(t, sibs.list, 1, "and nothing after it is searched")
}

// TestBuildSibling_FaultsAreErrorsNotPanics pins that the library failing on
// the fields beside a $ref returns as an error rather than escaping.
func TestBuildSibling_FaultsAreErrorsNotPanics(t *testing.T) {
	t.Parallel()
	var err error
	require.NotPanics(t, func() { _, err = buildSibling(t.Context(), nil) })
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

	sibs, _ := modelSiblings(t.Context(), doc, scan.InSource(0))
	assert.False(t, sibs.Unread(pathEntry(t, doc, "/a")), "a modelled use site is read")
}

// TestWithoutRef_KeepsThePositionAndTheDocumentsNodes pins the copy a model is
// built from: every $ref pair, an alias key included, is gone, the position and
// the document's own nodes stay, and a mapping holding nothing else has none.
// Which keys spell $ref is TestWithoutRef_DropsTheRefAsTheLibraryReadsIt's.
func TestWithoutRef_KeepsThePositionAndTheDocumentsNodes(t *testing.T) {
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

	// The two cheap readings agree with the copy: one asks whether a mapping
	// writes anything beside its $ref, the other whether it is a use site at all.
	for _, tc := range []struct {
		node            *yaml.Node
		beside, useSite bool
	}{{m, true, true}, {only, false, false}, {scalar, false, false}, {nil, false, false}, {top, true, false}} {
		assert.Equal(t, tc.beside, writesBeside(tc.node), "writesBeside")
		if tc.node != nil {
			assert.Equal(t, tc.useSite, refBeside(tc.node), "refBeside")
		}
	}
}

// TestWithoutRef_DropsTheRefAsTheLibraryReadsIt asks the library which key of a
// mapping is its $ref: each row unmarshals into a reference exactly when the
// library reads its first key so, and withoutRef must drop that pair and no
// other. A key spelled through an alias, a quote or a tag is read by its value;
// a merge key, a binary tag or another spelling is not.
func TestWithoutRef_DropsTheRefAsTheLibraryReadsIt(t *testing.T) {
	t.Parallel()
	var root yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte(`anchor: &k $ref
rows:
  - {$ref: '#/x', summary: s}
  - {"$ref": '#/x', summary: s}
  - {!!str $ref: '#/x', summary: s}
  - {!custom $ref: '#/x', summary: s}
  - {? $ref : '#/x', summary: s}
  - {*k : '#/x', summary: s}
  - {$REF: '#/x', summary: s}
  - {'$ref ': '#/x', summary: s}
  - {ref: '#/x', summary: s}
  - {<<: {$ref: '#/x'}, summary: s}
  - {!!binary JHJlZg==: '#/x', summary: s}
`), &root))
	// The library's reading of each row today, so a bump that reads a key
	// otherwise fails here even while withoutRef still agrees with it.
	isRef := []bool{true, true, true, true, true, true, false, false, false, false, false}
	rows := root.Content[0].Content[3].Content
	require.Len(t, rows, len(isRef))

	for i, row := range rows {
		var item soa.ReferencedPathItem
		_, err := marshaller.UnmarshalNode(t.Context(), "", row, &item)
		require.NoError(t, err, "row %d", i)
		library := item.IsReference()
		assert.Equal(t, isRef[i], library, "row %d: the library's reading of its key", i)

		fields, ok := withoutRef(row)
		require.True(t, ok, "row %d keeps its summary", i)
		dropped := len(fields.Content) < len(row.Content)
		assert.Equal(t, library, dropped, "row %d: withoutRef drops the pair the library reads as $ref", i)
		assert.Equal(t, library, isRefKey(row.Content[0]), "row %d", i)
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

// TestLoad_AFieldTheParserFaultsOnIsReported pins the parser fault's way into
// the load phase's report, for shapes a document can write beside a $ref: an
// error where the use site is written, once, and the document still loads, so
// the fields are never dropped in silence.
func TestLoad_AFieldTheParserFaultsOnIsReported(t *testing.T) {
	t.Parallel()
	for _, beside := range []string{
		"parameters: [{$ref: null}]",
		"put: {requestBody: {$ref: null}, responses: {'200': {description: ok}}}",
	} {
		doc, diags, err := Load(t.Context(), 0, compilers.Source{Path: "spec.yaml", Data: []byte(`openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /a:
    $ref: '#/paths/~1b'
    ` + beside + `
  /b:
    get: {responses: {"200": {description: ok}}}
`)}, Options{})
		require.NoError(t, err)
		require.NotNil(t, doc, "%s: the document still loads", beside)

		var faults []ir.Diagnostic
		for _, d := range diags {
			if d.Code == diag.Validation && strings.Contains(d.Message, "could not be read") {
				faults = append(faults, d)
			}
		}
		require.Len(t, faults, 1, "%s: reported once", beside)
		assert.Equal(t, ir.SeverityError, faults[0].Severity)
		assert.Equal(t, 5, faults[0].Provenance.Position.Line, "%s: sited where the use site is written", beside)
	}
}

// TestLoad_TheFieldsBesideARefAreReportedInSourceOrder pins where the model's
// findings land among the document's: in the order the source writes them, as
// the document's own are, not after them.
func TestLoad_TheFieldsBesideARefAreReportedInSourceOrder(t *testing.T) {
	t.Parallel()
	_, diags, err := Load(t.Context(), 0, compilers.Source{Path: "spec.yaml", Data: []byte(`openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /a:
    $ref: '#/paths/~1b'
    put: {deprecated: notabool, responses: {"200": {description: ok}}}
  /b:
    get: {deprecated: alsonotabool, responses: {"200": {description: ok}}}
`)}, Options{})
	require.NoError(t, err)

	var lines []int
	for _, d := range diags {
		if strings.HasPrefix(d.Code, diag.Validation+"/") {
			lines = append(lines, d.Provenance.Position.Line)
		}
	}
	assert.Equal(t, []int{6, 8}, lines, "the finding beside the $ref first, as the source writes it")
}

// loadSiblingFixture writes main beside a target document and loads main with
// external references allowed under limits.
func loadSiblingFixture(t *testing.T, main, target string, opts Options) []ir.Diagnostic {
	t.Helper()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "target.yaml"), []byte(target), 0o600))
	path := filepath.Join(dir, "main.yaml")
	opts.Reader = reader.Disk{}
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
// each reported once at the reference, and a finding in what it reaches kept at
// its own severity, reported once, in the document it is in.
func TestLoad_ASiblingsExternalReferenceIsReadAsAnyOther(t *testing.T) {
	t.Parallel()
	site := ir.Provenance{Pointer: "/paths/~1a/put/parameters/0"}
	param := "openapi: 3.1.0\ninfo: {title: X, version: \"1\"}\npaths: {}\ncomponents:\n  parameters:\n    Ext:\n"
	cases := []struct {
		name, target string
		opts         Options
		at           ir.Provenance
		code         string
		severity     ir.Severity
		says         string
	}{
		{"recursive anchor", param + "      name: q\n      in: query\n      schema: &s {type: object, properties: {self: *s}}\n",
			Options{}, site, diag.UnresolvedRef, ir.SeverityError, "recursive YAML anchor"},
		{"byte budget", param + "      name: q\n      in: query\n      description: " + strings.Repeat("x", len(siblingExternalMain)) + "\n",
			Options{MaxSourceBytes: len(siblingExternalMain)}, site, diag.UnresolvedRef, ir.SeverityError, "byte budget"},
		{"duplicate key", param + "      name: q\n      in: query\n      schema: {type: string}\n      description: one\n      description: two\n",
			Options{}, ir.Provenance{Source: 1, Pointer: "/components/parameters/Ext/description"},
			diag.Validation + "/validation-duplicate-key", ir.SeverityWarning, "more than once"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var at []ir.Diagnostic
			for _, d := range loadSiblingFixture(t, siblingExternalMain, tc.target, tc.opts) {
				if d.Provenance.Source == tc.at.Source && d.Provenance.Pointer == tc.at.Pointer {
					at = append(at, d)
				}
			}
			require.Len(t, at, 1, "one finding, where it is placed")
			assert.Equal(t, tc.code, at[0].Code)
			assert.Equal(t, tc.severity, at[0].Severity)
			assert.Contains(t, at[0].Message, tc.says)
		})
	}
}

// TestSiblings_UnreadAsksWithoutCopying pins Unread's cost: it asks whether a
// mapping no model holds writes a key other than $ref, which the first such key
// answers. Copying the mapping to ask cost its width at every mount reaching it.
func TestSiblings_UnreadAsksWithoutCopying(t *testing.T) {
	var keys strings.Builder
	for i := range 10_000 {
		fmt.Fprintf(&keys, "    k%d: v\n", i)
	}
	doc := modelOf(t, `openapi: 3.1.0
info: {title: T, version: "1"}
x-shared:
  item:
    $ref: '#/paths/~1b'
`+keys.String()+`paths:
  /a: {$ref: '#/x-shared/item'}
  /b:
    get: {responses: {"200": {description: ok}}}
`)
	sibs, _ := modelSiblings(t.Context(), doc, scan.InSource(0))
	_, err := pathEntry(t, doc, "/a").Resolve(t.Context(), soa.ResolveOptions{TargetLocation: "spec.yaml", RootDocument: doc, TargetDocument: doc})
	require.NoError(t, err)
	hop := pathEntry(t, doc, "/a").GetReferenceResolutionInfo().Object
	require.True(t, hop.IsReference(), "the walk does not reach the item under x-shared")

	require.True(t, sibs.Unread(hop))
	assert.Zero(t, testing.AllocsPerRun(20, func() { sibs.Unread(hop) }))
}

// TestModelSiblings_DoesNotWalkADocumentWithNothingBesideARef pins the skip
// that keeps the sibling search off nearly every document: a tree in which no
// mapping keys $ref beside another holds no use site, so the model is not
// walked, and the cost is a scan of the tree, whatever the document's size.
func TestModelSiblings_DoesNotWalkADocumentWithNothingBesideARef(t *testing.T) {
	var spec strings.Builder
	spec.WriteString("openapi: 3.1.0\ninfo: {title: T, version: \"1\"}\npaths:\n  /r: {$ref: '#/paths/~1p0'}\n")
	for i := range 200 {
		fmt.Fprintf(&spec, "  /p%d:\n    get: {responses: {\"200\": {description: ok}}}\n", i)
	}
	doc := modelOf(t, spec.String())

	allocs := testing.AllocsPerRun(10, func() { modelSiblings(t.Context(), doc, scan.InSource(0)) })
	assert.Less(t, allocs, 50.0, "a scan, not a walk of 200 path items")
}

// TestSiblings_BesideNamesTheFieldAPointerPasses pins which pointers pass
// through the fields beside a path item's $ref: one naming a key written there,
// at any position the walk met the use site at, an alias's included, and at a
// key a `<<` merge key supplies there. The first use site on the pointer is the
// one named. A key nothing writes beside the $ref, the use site itself, and a
// $ref only a merge key supplies, which the library reads as an inline item,
// are not, and an empty registry passes none.
func TestSiblings_BesideNamesTheFieldAPointerPasses(t *testing.T) {
	t.Parallel()
	doc := modelOf(t, siblingSpec+`  /e:
    $ref: '#/paths/~1b'
    <<: {patch: {responses: {"200": {description: ok}}}}
`)
	sibs, faults := modelSiblings(t.Context(), doc, scan.InSource(0))
	require.Empty(t, faults)

	cases := []struct {
		pointer  jsontext.Pointer
		use, key string
	}{
		{"/paths/~1a/put/responses", "/paths/~1a", "put"},
		{"/paths/~1a/summary", "/paths/~1a", "summary"},
		{"/paths/~1d/put", "/paths/~1d", "put"},
		{"/paths/~1e/patch", "/paths/~1e", "patch"},
		{"/paths/~1a/put/callbacks/cb/{$request.body#~1url}/delete", "/paths/~1a", "put"},
		{"/paths/~1a/get", "", ""},
		{"/paths/~1a", "", ""},
		{"/paths/~1c/put", "", ""},
		{"/paths/~1m/post", "", ""},
		{"", "", ""},
	}
	for _, tc := range cases {
		use, key, ok := sibs.Beside(tc.pointer)
		assert.Equal(t, tc.use != "", ok, "%s", tc.pointer)
		assert.Equal(t, jsontext.Pointer(tc.use), use, "%s", tc.pointer)
		assert.Equal(t, tc.key, key, "%s", tc.pointer)
	}
	_, _, ok := Siblings{}.Beside("/paths/~1a/put")
	assert.False(t, ok, "an empty registry holds no use site")
}

// TestSiblings_WritesReadsTheNodeAnAnswerIsWrittenAs pins Writes on the answers
// a copy gives: one the resolver builds from the node the fields write, as for
// a pointer through an alias's position, is that field, though it is not the
// object their model holds. An answer with no node of its own is none.
func TestSiblings_WritesReadsTheNodeAnAnswerIsWrittenAs(t *testing.T) {
	t.Parallel()
	spec := strings.Replace(besideChainSpec, "  /a:\n", "  /a: &a\n", 1)
	spec = strings.Replace(spec, "components:\n", `  /z: *a
  /d:
    post:
      requestBody: {$ref: 'spec.yaml#/paths/~1z/get/requestBody'}
      responses: {"200": {description: ok}}
components:
`, 1)
	got, diags, err := Load(t.Context(), 0, compilers.Source{Path: "spec.yaml", Data: []byte(spec)},
		Options{Reader: reader.Disk{}})
	require.NoError(t, err)
	for _, d := range diags {
		assert.NotEqual(t, jsontext.Pointer("/paths/~1d/post/requestBody"), d.Provenance.Pointer, "%s", d.Message)
	}

	d := pathEntry(t, got.Doc, "/d").GetObject().Post().GetRequestBody()
	copied := d.GetReferenceResolutionInfo().Object
	a, ok := got.Siblings.Of(pathEntry(t, got.Doc, "/a"))
	require.True(t, ok)
	require.NotSame(t, a.Item.Get().GetRequestBody(), copied, "the resolver built a copy")
	assert.True(t, got.Siblings.Writes("/paths/~1z/get/requestBody", copied))

	got.Siblings.write("/paths/~1a/get/requestBody", bareReference{})
	assert.False(t, got.Siblings.Writes("/paths/~1a/get/requestBody", bareReference{}))
}

// TestSiblings_BesideReadsAPointerInBoundedWork pins Beside's cost, which every
// hop of every reference pays in this phase and in the lowering: it reads a
// pointer only as deep as some use site's position goes, so a long pointer costs
// what a short one does. Building each prefix of the pointer instead made it
// quadratic in the pointer's length. Allocations count the work, since they do
// not depend on the machine.
func TestSiblings_BesideReadsAPointerInBoundedWork(t *testing.T) {
	sibs, _ := modelSiblings(t.Context(), modelOf(t, siblingSpec), scan.InSource(0))
	long := func(head string, tokens int) jsontext.Pointer {
		return jsontext.Pointer(head + strings.Repeat("/x", tokens))
	}
	for _, head := range []string{"/paths/~1a/put", "/paths/~1a/get", "/paths/~1c", "/components"} {
		short := testing.AllocsPerRun(20, func() { sibs.Beside(long(head, 1_000)) })
		longer := testing.AllocsPerRun(20, func() { sibs.Beside(long(head, 100_000)) })
		assert.Equal(t, short, longer, "%s: a hundred times the tokens, the same work", head)
	}
}
