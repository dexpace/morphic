package load

import (
	"encoding/json/jsontext"
	"errors"
	"io"
	"io/fs"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/speakeasy-api/openapi/references"
	"github.com/speakeasy-api/openapi/validation"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi/internal/overlay"
	"github.com/dexpace/morphic/ir"
)

// backRoot is a source whose /a response is a $ref into back.yaml, which
// refers back into the source by spelling, and whose own R lacks the
// description a response requires.
func backRoot(paths string) string {
	return `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
` + paths + `components:
  responses:
    R:
      content: {}
`
}

// backPath is a path item whose get operation's 200 response is ref.
func backPath(path, ref string) string {
	return "  " + path + ":\n    get:\n      responses:\n        \"200\": {$ref: '" + ref + "'}\n"
}

// response200 returns the 200 response the get operation at path names.
func response200(t *testing.T, doc *soa.OpenAPI, path string) *soa.ReferencedResponse {
	t.Helper()
	item, ok := doc.Paths.Get(path)
	require.True(t, ok, path)
	resp, ok := item.GetObject().Get().GetResponses().Get("200")
	require.True(t, ok, path)
	return resp
}

// diagLines renders diags as "pointer code", sorted, which is what an order
// comparison and an expectation both need.
func diagLines(diags []ir.Diagnostic) []string {
	out := make([]string, 0, len(diags))
	for _, d := range diags {
		out = append(out, string(d.Provenance.Pointer)+" "+d.Code)
	}
	slices.Sort(out)
	return out
}

// TestHold_ABackReferenceReachesTheSourceAsHeld pins GitHub #759. A $ref from
// another document back into the source reaches the source's own declaration,
// not the file read again: the finding on it is the source's alone, an overlay
// that fixes it fixes it for the $ref too, and the file on disk is never what
// answers.
func TestHold_ABackReferenceReachesTheSourceAsHeld(t *testing.T) {
	t.Parallel()
	root := backRoot(backPath("/a", "back.yaml#/components/responses/Back"))
	dir := externalDir(t, map[string]string{
		"back.yaml": "components:\n  responses:\n    Back: {$ref: 'root.yaml#/components/responses/R'}\n",
		// The file on disk is not the source compiled: a read of it would find
		// R described, and lose the source's finding.
		"root.yaml": strings.Replace(root, "content: {}", "description: on disk", 1),
	})

	t.Run("the finding is the source's own", func(t *testing.T) {
		t.Parallel()
		got, diags := loadExternal(t, dir, root, Options{})

		assert.Empty(t, cmp.Diff([]string{" openapi/validation/validation-required-field"}, diagLines(diags)),
			"reported once, by the source's own validation, and not again at the $ref")
		r, ok := got.Doc.Components.Responses.Get("R")
		require.True(t, ok)
		assert.Same(t, r.GetObject(), response200(t, got.Doc, "/a").GetObject(),
			"the $ref reaches the source's own R")
	})
	t.Run("an overlay fixes it for the $ref too", func(t *testing.T) {
		t.Parallel()
		opts := overlayOptions("  - target: $.components.responses.R\n    update: {description: patched}\n")
		got, diags := loadExternal(t, dir, root, opts)

		assert.Empty(t, diags)
		assert.Equal(t, "patched", response200(t, got.Doc, "/a").GetObject().GetDescription())
	})
}

// TestHold_AFindingOnlyValidationMakesIsTheSourcesOwn pins the other channel a
// back reference could report through. A parameter's location is checked by
// validation, not drawn when the parameter is read, so it is the validation of
// what references reach that would report it again: a trail into the source
// ends in the source, which its own validation covers.
func TestHold_AFindingOnlyValidationMakesIsTheSourcesOwn(t *testing.T) {
	t.Parallel()
	dir := externalDir(t, map[string]string{
		"back.yaml": "components:\n  parameters:\n    Back: {$ref: 'root.yaml#/components/parameters/Q'}\n",
	})
	root := `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /a:
    get:
      parameters: [{$ref: 'back.yaml#/components/parameters/Back'}]
      responses: {"200": {description: ok}}
components:
  parameters:
    Q: {name: q, in: sideways, schema: {type: string}}
`
	_, diags := loadExternal(t, dir, root, Options{})

	assert.Empty(t, cmp.Diff([]string{" openapi/validation/validation-allowed-values"}, diagLines(diags)))
}

// TestHold_EverySpellingOfTheSourceReachesIt pins that a back reference reaches
// the source however its path is spelled. Each spelling the resolver keys as
// the source's path, or that path cleaned, reaches its own declaration. One it
// keys otherwise, an absolute path not cleaned, is opened, and answered with
// the source's tree, so it reaches the source's node, read once.
func TestHold_EverySpellingOfTheSourceReachesIt(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	clean := filepath.Join(dir, "root.yaml")
	for _, c := range []struct {
		name, source, spelling string
		sameObject             bool
	}{
		{"its file name", clean, "root.yaml", true},
		{"through the current directory", clean, "./root.yaml", true},
		{"through its parent", clean, "../" + filepath.Base(dir) + "/root.yaml", true},
		{"its absolute path", clean, clean, true},
		{"an absolute path not cleaned", clean, dir + "/./root.yaml", false},
		{"its file name, from a source path not cleaned", dir + "/./root.yaml", "root.yaml", true},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			sub := filepath.Join(dir, strings.ReplaceAll(c.name, " ", "-"))
			writeFile(t, dir, filepath.Base(sub)+".yaml",
				"components:\n  responses:\n    Back: {$ref: '"+c.spelling+"#/components/responses/R'}\n")
			root := backRoot(backPath("/a", filepath.Base(sub)+".yaml#/components/responses/Back"))
			got, diags, err := Load(t.Context(), 0,
				compilers.Source{Path: c.source, Data: []byte(root)}, Options{AllowExternalRefs: true})
			require.NoError(t, err)
			require.NotNil(t, got, "%+v", diags)

			assert.Empty(t, cmp.Diff([]string{" openapi/validation/validation-required-field"}, diagLines(diags)))
			r, ok := got.Doc.Components.Responses.Get("R")
			require.True(t, ok)
			reached := response200(t, got.Doc, "/a").GetObject()
			require.NotNil(t, reached, "the $ref resolves although no root.yaml is on disk")
			assert.Same(t, r.GetObject().GetRootNode(), reached.GetRootNode(), "it reaches the source's own node")
			if c.sameObject {
				assert.Same(t, r.GetObject(), reached, "and the source's own declaration")
			}
		})
	}
}

// TestHold_ABackReferenceResolvesAsItDoesInEitherOrder pins that what a back
// reference reaches does not follow declaration order: the source's own
// declaration, whether an internal $ref to it was resolved before it or after.
func TestHold_ABackReferenceResolvesAsItDoesInEitherOrder(t *testing.T) {
	t.Parallel()
	dir := externalDir(t, map[string]string{
		"back.yaml": "components:\n  responses:\n    Back: {$ref: 'root.yaml#/components/responses/R'}\n",
	})
	back := backPath("/back", "back.yaml#/components/responses/Back")
	internal := backPath("/internal", "#/components/responses/R")

	reports := make([][]string, 0, 2)
	for _, root := range []string{backRoot(back + internal), backRoot(internal + back)} {
		got, diags := loadExternal(t, dir, root, Options{})
		reports = append(reports, diagLines(diags))
		assert.Same(t, response200(t, got.Doc, "/internal").GetObject(), response200(t, got.Doc, "/back").GetObject())
	}
	assert.Empty(t, cmp.Diff(reports[0], reports[1]))
}

// TestHold_AFindingTheSourceDoesNotMakeIsReportedOnce pins what the source's
// own findings leave to the $refs. A response under an extension is no object
// the source's validation reaches, so its finding is reported at a $ref
// reaching it, once, at the least of an internal $ref and a back reference.
func TestHold_AFindingTheSourceDoesNotMakeIsReportedOnce(t *testing.T) {
	t.Parallel()
	dir := externalDir(t, map[string]string{
		"back.yaml": "components:\n  responses:\n    Back: {$ref: 'root.yaml#/x-lib/R'}\n",
	})
	root := `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
` + backPath("/b", "back.yaml#/components/responses/Back") + backPath("/a", "#/x-lib/R") + `x-lib:
  R: {content: {}}
`
	_, diags := loadExternal(t, dir, root, Options{})

	assert.Empty(t, cmp.Diff([]string{"/paths/~1a/get/responses/200 openapi/validation/validation-required-field"},
		diagLines(diags)))
}

// TestHold_APanicStopsTheWalkWhereItStands pins that a walk which panics holds
// what it held before, and that the panic does not escape: the resolution walk
// that follows reaches the same shape and reports it.
func TestHold_APanicStopsTheWalkWhereItStands(t *testing.T) {
	t.Parallel()
	doc := &soa.OpenAPI{}
	root := &yaml.Node{Kind: yaml.DocumentNode}
	reader := newExternal(doc, Options{}, newExternalReads(sourceDocument{path: "root.yaml", data: []byte("x"), root: root}))
	first := fakeResolvable{ref: "#/first"}
	items := func(yield func(soa.WalkItem) bool) {
		if !yield(fakeWalkItem("first", first)) {
			return
		}
		panic("boom")
	}

	require.NotPanics(t, func() { reader.holdWalked(items) })

	tree, ok := doc.GetCachedExternalDocument("root.yaml")
	require.True(t, ok)
	assert.Same(t, root, tree)
	held, ok := doc.GetCachedReferencedObject("root.yaml#/first")
	require.True(t, ok, "what the walk reached before the panic is held")
	assert.Equal(t, first, held)
}

// TestHold_ASourceWithNoKeyIsNotHeld pins that hold stores nothing for a source
// the resolver could not look up: one with no path.
func TestHold_ASourceWithNoKeyIsNotHeld(t *testing.T) {
	t.Parallel()
	doc := &soa.OpenAPI{}
	reader := newExternal(doc, Options{}, newExternalReads(sourceDocument{root: &yaml.Node{}}))
	reader.holdWalked(func(func(soa.WalkItem) bool) { t.Fatal("walked for a source held under no key") })
	_, ok := doc.GetCachedExternalDocument("")
	assert.False(t, ok)
}

// TestSourceDocument_Keys pins the keys the source is held under.
func TestSourceDocument_Keys(t *testing.T) {
	t.Parallel()
	tree := &yaml.Node{}
	for _, c := range []struct {
		name string
		self sourceDocument
		want []string
	}{
		{"a clean path", sourceDocument{path: "dir/root.yaml", root: tree}, []string{"dir/root.yaml"}},
		{"a path cleaned", sourceDocument{path: "./dir/../root.yaml", root: tree}, []string{"./dir/../root.yaml", "root.yaml"}},
		{"a URL, as spelled", sourceDocument{path: "https://example.com/./a.yaml", root: tree}, []string{"https://example.com/./a.yaml"}},
		{"no path", sourceDocument{root: tree}, nil},
		{"no tree", sourceDocument{path: "root.yaml"}, nil},
	} {
		assert.Equal(t, c.want, c.self.keys(), c.name)
	}
}

// TestSourceDocument_Names pins which file names the resolver may open that
// name the source.
func TestSourceDocument_Names(t *testing.T) {
	t.Parallel()
	tree := &yaml.Node{}
	self := sourceDocument{path: "dir/root.yaml", root: tree}
	abs, err := filepath.Abs("dir/root.yaml")
	require.NoError(t, err)
	for _, c := range []struct {
		name string
		self sourceDocument
		file string
		want bool
	}{
		{"itself", self, "dir/root.yaml", true},
		{"through a directory", self, "dir/../dir/./root.yaml", true},
		{"its absolute path", self, abs, true},
		{"another file", self, "dir/other.yaml", false},
		{"from a source with no tree", sourceDocument{path: "dir/root.yaml"}, "dir/root.yaml", false},
		{"from a source named by URL", sourceDocument{path: "https://example.com/a.yaml", root: tree},
			"https://example.com/a.yaml", false},
	} {
		assert.Equal(t, c.want, c.self.names(c.file), c.name)
	}
}

// TestNewSourceDocument_KeepsOnlyFindingsAtANode pins which of the source's
// own findings a $ref's are measured against: one at a node, never one at
// none, which is told apart by where it is placed.
func TestNewSourceDocument_KeepsOnlyFindingsAtANode(t *testing.T) {
	t.Parallel()
	node := &yaml.Node{}
	atNode := &validation.Error{Rule: "r", UnderlyingError: errors.New("m"), Node: node}
	atNone := &validation.Error{Rule: "r", UnderlyingError: errors.New("n")}

	self := newSourceDocument("root.yaml", nil, nil, []error{atNode, atNone, errors.New("plain")})

	assert.Equal(t, map[findingKey]bool{keyOf(atNode, ""): true}, self.found)
}

// TestSourceFile pins the file the source is served as: its bytes, read to the
// end, a close that cannot fail, and no file information to give.
func TestSourceFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "root.yaml")
	reader := newExternal(&soa.OpenAPI{}, Options{},
		newExternalReads(sourceDocument{path: path, data: []byte("held"), root: &yaml.Node{}}))

	f, err := reader.Open(path)
	require.NoError(t, err, "no root.yaml is on disk: the source is served as held")
	data, err := io.ReadAll(f)
	require.NoError(t, err)
	assert.Equal(t, "held", string(data))
	_, err = f.Stat()
	require.ErrorIs(t, err, fs.ErrInvalid)
	assert.NoError(t, f.Close())
}

// TestReachedFindings_DropsAKnownFinding pins the known findings at the one
// place they act: a finding the walk draws at a node the source's own
// validation already reported is dropped there.
func TestReachedFindings_DropsAKnownFinding(t *testing.T) {
	t.Parallel()
	node := &yaml.Node{Line: 1, Column: 1}
	finding := &validation.Error{Severity: validation.SeverityError, Rule: "r", UnderlyingError: errors.New("m"), Node: node}
	found := reachedFindings{sites: map[references.Reference]jsontext.Pointer{}, known: map[findingKey]bool{keyOf(finding, ""): true}}
	found.note("/a", trail{}, []error{finding})
	assert.Empty(t, found.diags(pointerAt(0, overlay.Origin{})))
}
