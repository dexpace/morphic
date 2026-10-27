package load

import (
	"context"
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/speakeasy-api/openapi/marshaller"
	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi/internal/bind"
	"github.com/dexpace/morphic/compilers/openapi/internal/bind/typed"
	"github.com/dexpace/morphic/compilers/openapi/internal/budget"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
	"github.com/dexpace/morphic/compilers/openapi/internal/oas"
	"github.com/dexpace/morphic/compilers/openapi/internal/overlay"
	"github.com/dexpace/morphic/engine/reader"
	"github.com/dexpace/morphic/ir"
)

// externalDir writes files, name to content, into a fresh directory for a root
// to reach, and returns the directory.
func externalDir(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		writeFile(t, dir, name, content)
	}
	return dir
}

// loadExternal loads root as root.yaml in dir, with external references
// allowed, and requires it to load.
func loadExternal(t *testing.T, dir, root string, opts Options) (*Document, []ir.Diagnostic) {
	t.Helper()
	opts.Reader = reader.Disk{}
	got, diags, err := Load(t.Context(), 0, compilers.Source{Path: filepath.Join(dir, "root.yaml"), Data: []byte(root)}, opts)
	require.NoError(t, err)
	require.NotNil(t, got, "%+v", diags)
	return got, diags
}

// TestSpanned covers spanned's properties: it counts every node under root,
// an alias counts as the copy of what it names and nothing more, a tree of
// exactly limit nodes is counted whole, a limit stops the count one node past
// it, never at the whole tree, and math.MaxInt, past which no node lies,
// counts it all.
func TestSpanned(t *testing.T) {
	t.Parallel()
	wide := &yaml.Node{Kind: yaml.MappingNode}
	for range 1000 {
		wide.Content = append(wide.Content, &yaml.Node{Kind: yaml.ScalarNode})
	}
	target := &yaml.Node{Kind: yaml.ScalarNode}
	aliased := &yaml.Node{Kind: yaml.MappingNode, Content: []*yaml.Node{{Kind: yaml.AliasNode, Alias: target}}}

	for name, c := range map[string]struct {
		root   *yaml.Node
		within budget.Limit
		count  int
	}{
		"every node under root":                   {wide, budget.Unbounded(), 1001},
		"an alias counts as its target's copy":    {aliased, budget.Unbounded(), 2},
		"a tree of exactly limit nodes is whole":  {wide, budget.AtMost(1001), 1001},
		"a limit stops the count one past it":     {wide, budget.AtMost(6), 7},
		"the largest limit counts the whole tree": {wide, budget.AtMost(math.MaxInt), 1001},
	} {
		assert.Equal(t, c.count, spanned(t.Context(), c.root, c.within), name)
	}
}

// TestReached_ChargeHoldsADocumentToWhatItHasLeft pins that charge measures an
// object against what its document has left, not against the whole budget: of
// a document with 6 nodes left, a 7-node object is refused and a 6-node one is
// admitted, after which a document with none left refuses even one node.
func TestReached_ChargeHoldsADocumentToWhatItHasLeft(t *testing.T) {
	t.Parallel()
	object := func(nodes int) *yaml.Node {
		root := &yaml.Node{Kind: yaml.MappingNode}
		for range nodes - 1 {
			root.Content = append(root.Content, &yaml.Node{Kind: yaml.ScalarNode})
		}
		return root
	}
	charged := func() *reached {
		spans := budget.New(budget.AtMost(10))
		spans.Spend(4)
		return &reached{limit: budget.AtMost(10), spans: map[string]*budget.Budget{"doc": &spans}}
	}

	v := charged()
	d, over := v.charge(t.Context(), ir.Provenance{}, "doc", object(7))
	assert.True(t, over)
	require.Len(t, d, 1)
	assert.Equal(t, diag.BudgetExceeded, d[0].Code)
	assert.Contains(t, d[0].Message, "more than the 10 nodes", "the budget, not what is left of it")

	v = charged()
	d, over = v.charge(t.Context(), ir.Provenance{}, "doc", object(6))
	assert.False(t, over)
	assert.Empty(t, d)
	assert.Equal(t, 10, v.spans["doc"].Spent())

	d, over = v.charge(t.Context(), ir.Provenance{}, "doc", object(1))
	assert.True(t, over)
	require.Len(t, d, 1)
	assert.Equal(t, diag.BudgetExceeded, d[0].Code)
}

// reachedFixtureOther, reachedFixtureThird and reachedFixtureRoot exercise
// reachedNode and targetOf through a real, resolved document: a schema
// reference to an object, one to a boolean (nothing but its value), a path
// item, a path item whose chain stops at a pointer naming nothing, a response
// whose chain loops, a chain through two documents, and internal references
// that stay in the source or leave it.
const reachedFixtureOther = `openapi: 3.1.0
info: {title: O, version: "1"}
paths:
  /x:
    get:
      operationId: getX
      responses: {"200": {description: ok}}
  /stops: {$ref: "#/paths/~1missing"}
components:
  schemas:
    Obj: {type: object, properties: {inner: {type: string}}}
    AnyBool: true
    Hop: {$ref: "./third.yaml#/components/schemas/Leaf"}
  responses:
    Loop: {$ref: "#/components/responses/Back"}
    Back: {$ref: "#/components/responses/Loop"}
`

const reachedFixtureThird = `components:
  schemas:
    Leaf: {type: string}
`

const reachedFixtureRoot = `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /x: {$ref: "./other.yaml#/paths/~1x"}
  /stops: {$ref: "./other.yaml#/paths/~1stops"}
components:
  schemas:
    S: {$ref: "./other.yaml#/components/schemas/Obj"}
    Bool: {$ref: "./other.yaml#/components/schemas/AnyBool"}
    Chained: {$ref: "./other.yaml#/components/schemas/Hop"}
    Local: {type: string}
    ToLocal: {$ref: "#/components/schemas/Local"}
    ToS: {$ref: "#/components/schemas/S"}
  responses:
    Loops: {$ref: "./other.yaml#/components/responses/Loop"}
`

// reachedFixtureDoc loads reachedFixtureRoot beside the documents it names
// and returns the resolved document and the directory they are in.
func reachedFixtureDoc(t *testing.T) (*soa.OpenAPI, string) {
	t.Helper()
	dir := externalDir(t, map[string]string{"other.yaml": reachedFixtureOther, "third.yaml": reachedFixtureThird})
	got, _ := loadExternal(t, dir, reachedFixtureRoot, Options{})
	return got.Doc, dir
}

// TestTargetOf covers which reference objects reach an object, and the
// document each names: the one its chain ends in, not the one it first leaves
// for. One whose chain ends on no object reaches nothing.
func TestTargetOf(t *testing.T) {
	t.Parallel()
	doc, dir := reachedFixtureDoc(t)
	x, ok := doc.Paths.Get("/x")
	require.True(t, ok)
	stops, ok := doc.Paths.Get("/stops")
	require.True(t, ok)

	got, ok := targetOf("/site", x, nil)
	require.True(t, ok)
	assert.Equal(t, filepath.Join(dir, "other.yaml"), got.doc)
	assert.False(t, got.inSource)
	assert.Equal(t, jsontext.Pointer("/site"), got.site)
	assert.NotNil(t, got.node)
	assert.Equal(t, oas.PathItem, got.kind)
	_, ok = targetOf("/site", stops, nil)
	assert.False(t, ok, "/stops reaches no object")
}

// TestBuiltTargets covers which schemas built for the bindings' landings are
// validated, and where: one a $ref's chain reaches, at the least such $ref and
// named by its document, in the source or not, placed in the document writing
// that $ref, and read where it lands. One no $ref reaches, one that failed to
// build and a boolean schema are none.
func TestBuiltTargets(t *testing.T) {
	t.Parallel()
	built := buildRaw(t, "{type: object}")
	place := func(p bind.Position) ir.Provenance { return ir.Provenance{Source: int(p.Doc) + 10, Pointer: p.Pointer} }
	got := builtTargets([]typed.Built{
		{Landing: bind.Landing{At: bind.Position{Pointer: "/x-lib/A"}, Name: "spec.yaml",
			Site: bind.Position{Pointer: "/s"}, Reached: true}, Schema: built},
		{Landing: bind.Landing{At: bind.Position{Doc: 1, Pointer: "/B"}, Name: "other.yaml",
			Site: bind.Position{Pointer: "/t"}, Reached: true}, Schema: built},
		{Landing: bind.Landing{At: bind.Position{Doc: 1, Pointer: "/F"}, Name: "other.yaml",
			Site: bind.Position{Doc: 1, Pointer: "/u"}, Reached: true}, Schema: built},
		{Landing: bind.Landing{At: bind.Position{Doc: 1, Pointer: "/C"}, Name: "other.yaml"}, Schema: built},
		{Landing: bind.Landing{At: bind.Position{Pointer: "/x-lib/D"}, Reached: true}, Err: errors.New("no")},
		{Landing: bind.Landing{At: bind.Position{Pointer: "/x-lib/E"}, Reached: true}, Schema: buildRaw(t, "true")},
	}, place, func(id bind.DocID) string { return fmt.Sprintf("doc%d.yaml", id) })

	require.Len(t, got, 3)
	assert.Equal(t, reachedTarget{site: "/s", kind: oas.Schema, node: built.GetSchema().GetRootNode(), doc: "spec.yaml",
		inSource: true, home: refSite{doc: "doc0.yaml", pointer: "/x-lib/A"}}, got[0])
	assert.Equal(t, jsontext.Pointer("/t"), got[1].site)
	assert.Equal(t, refSite{doc: "doc1.yaml", pointer: "/B"}, got[1].home, "read where it lands, in that document")
	assert.Equal(t, "other.yaml", got[1].doc)
	assert.False(t, got[1].inSource)
	assert.Nil(t, got[1].place, "a $ref the source writes is placed as the source's")
	assert.Equal(t, jsontext.Pointer("/u"), got[2].site)
	require.NotNil(t, got[2].place, "a $ref only another document writes")
	assert.Equal(t, ir.Provenance{Source: 11, Pointer: "/u"}, got[2].place("/u"), "placed in that document")
	assert.Equal(t, "doc1.yaml", got[2].siteDoc, "and ordered by its name")
	assert.Empty(t, got[1].siteDoc)
}

// buildRaw decodes src, YAML, as the bindings build a schema for a landing.
func buildRaw(t *testing.T, src string) *schemaRef {
	t.Helper()
	var n yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte(src), &n))
	js := &schemaRef{}
	_, err := marshaller.UnmarshalNode(t.Context(), "", n.Content[0], js)
	require.NoError(t, err)
	return js
}

// TestReachedNode covers every outcome of reachedNode: a reference object
// resolving to an object, a chain that ends on no object, and a resolvable
// that cannot say what it names, which no real document produces, so a fake
// drives it.
func TestReachedNode(t *testing.T) {
	t.Parallel()
	doc, _ := reachedFixtureDoc(t)

	t.Run("a reference object resolving to an object", func(t *testing.T) {
		t.Parallel()
		ref, ok := doc.Paths.Get("/x")
		require.True(t, ok)
		node, ok := reachedNode(ref)
		assert.True(t, ok)
		assert.Same(t, nodeview.Deref(ref.GetObject().GetRootNode()), node)
	})

	stops, ok := doc.Paths.Get("/stops")
	require.True(t, ok)
	loops, ok := doc.Components.Responses.Get("Loops")
	require.True(t, ok)
	for name, ref := range map[string]resolvable{"stops": stops, "loops": loops} {
		t.Run("a chain that "+name+" ends on no object", func(t *testing.T) {
			t.Parallel()
			require.True(t, ref.IsResolved(), "the resolver marks it resolved, with nothing at its end")
			node, ok := reachedNode(ref)
			assert.False(t, ok)
			assert.Nil(t, node)
		})
	}

	t.Run("a resolvable that cannot say what it names names nothing to check", func(t *testing.T) {
		t.Parallel()
		_, ok := reachedNode(fakeResolvable{ref: "other.yaml#/x", resolved: true})
		assert.False(t, ok)
	})
}

// TestKindOf covers the kind each reference object expects, and that a
// resolvable the table has no kind for expects none.
func TestKindOf(t *testing.T) {
	t.Parallel()
	for want, ref := range map[oas.Kind]resolvable{
		oas.PathItem: &soa.ReferencedPathItem{}, oas.Parameter: &soa.ReferencedParameter{},
		oas.Header: &soa.ReferencedHeader{}, oas.RequestBody: &soa.ReferencedRequestBody{},
		oas.Response: &soa.ReferencedResponse{}, oas.Example: &soa.ReferencedExample{},
		oas.Link: &soa.ReferencedLink{}, oas.Callback: &soa.ReferencedCallback{},
		oas.SecurityScheme: &soa.ReferencedSecurityScheme{},
	} {
		got, ok := kindOf(ref)
		assert.True(t, ok, "%T", ref)
		assert.Equal(t, want, got, "%T", ref)
	}
	_, ok := kindOf(fakeResolvable{})
	assert.False(t, ok)
}

// TestCheckReached_APanicReadingTheWalkIsReported pins the barrier around the
// walk: a panic in it is reported at the root, and nothing is checked.
func TestCheckReached_APanicReadingTheWalkIsReported(t *testing.T) {
	t.Parallel()
	doc := parseSpec(t, minimal31)
	item := fakeWalkItem("x", fakeResolvable{ref: "other.yaml#/x", resolved: true})
	items := func(yield func(soa.WalkItem) bool) {
		if yield(item) {
			panic("walk boom")
		}
	}

	diags := checkReached(t.Context(), pointerAt(0, overlay.Origin{}), items, nil,
		newReached(doc, Options{}, sourceTree{}, judgment{}))

	require.Len(t, diags, 1, "%+v", diags)
	assert.Equal(t, diag.Validation, diags[0].Code)
	assert.Equal(t, ir.SeverityError, diags[0].Severity)
	assert.Equal(t, "validation panicked: walk boom", diags[0].Message)
	assert.Equal(t, jsontext.Pointer(""), diags[0].Provenance.Pointer)
}

// TestLoad_ExternalRefResolutionErrors pins how a finding inside a document an
// external reference names is reported: under its own rule and severity, in
// that document's Document.Sources entry, at the node it is about (GitHub #537,
// #1030). Findings come in the table's order: an object's missing fields, then
// the fields it holds.
func TestLoad_ExternalRefResolutionErrors(t *testing.T) {
	t.Parallel()
	path := "../../../../testdata/openapi/resolve_main_external.yaml"
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	ld, diags, loadErr := Load(t.Context(), 0, compilers.Source{Path: path, Data: data},
		Options{Reader: reader.Disk{}})
	require.NoError(t, loadErr)
	require.NotNil(t, ld)

	want := []struct {
		severity ir.Severity
		code     string
		at       ir.Provenance
		message  string
	}{
		{ir.SeverityError, "openapi/validation/validation-required-field",
			ir.Provenance{Source: 1, Pointer: "/components/responses/Bad", Position: ir.Position{Line: 7, Column: 7}},
			"Response Object field \"description\" is REQUIRED in OpenAPI 3.1 (Response Object)"},
		{ir.SeverityError, "openapi/validation/validation-type-mismatch",
			ir.Provenance{Source: 1, Pointer: "/components/responses/Bad/headers/X/required",
				Position: ir.Position{Line: 10, Column: 21}},
			"Header Object field \"required\" must be a boolean in OpenAPI 3.1 (Header Object)"},
	}
	require.Len(t, diags, len(want), "%+v", diags)
	for i, w := range want {
		assert.Equal(t, w.severity, diags[i].Severity, "entry %d", i)
		assert.Equal(t, w.code, diags[i].Code, "entry %d", i)
		assert.Equal(t, w.at, diags[i].Provenance, "entry %d", i)
		assert.Equal(t, w.message, diags[i].Message, "entry %d", i)
	}
	other, ok := ld.Others.At(1)
	require.True(t, ok)
	assert.Equal(t, "resolve_target_invalid.yaml", other.Label, "the document the $ref names")
}

// TestReached_CheckAllStopsOnceTheContextIsDone pins the context the checks of
// reached objects ask before each target: handed a done one, no target is
// charged or checked, where a live one charges and checks each.
func TestReached_CheckAllStopsOnceTheContextIsDone(t *testing.T) {
	t.Parallel()
	doc := parseSpec(t, minimal31)
	var node yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte("{name: p}"), &node))
	targets := map[objectKey]reachedTarget{
		{node: node.Content[0], kind: oas.Parameter}: {site: "/x", kind: oas.Parameter, node: node.Content[0], doc: "other.yaml"},
	}
	at := pointerAt(0, overlay.Origin{})
	cancelled, cancel := context.WithCancel(t.Context())
	cancel()

	stopped := newReached(doc, Options{}, sourceTree{}, judgment{})
	assert.Empty(t, stopped.checkAll(cancelled, at, targets))
	assert.Empty(t, stopped.spans, "no target was charged")

	live := newReached(doc, Options{}, sourceTree{}, judgment{})
	assert.NotEmpty(t, live.checkAll(t.Context(), at, targets), "a parameter with no `in` is found wanting")
	assert.NotEmpty(t, live.spans)
}
