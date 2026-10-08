package load

import (
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/google/go-cmp/cmp"
	oas3 "github.com/speakeasy-api/openapi/jsonschema/oas3"
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
// the source's own declaration however its path is spelled: one the resolver
// keys as the source's path or that path cleaned, and one it keys otherwise,
// which it opens, finds held, and reads no further. An alias, a declaration
// that is itself a $ref, is reached through to what it names.
func TestHold_EverySpellingOfTheSourceReachesIt(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	clean := filepath.Join(dir, "root.yaml")
	for _, c := range []struct {
		name, source, spelling string
	}{
		{"its file name", clean, "root.yaml"},
		{"through the current directory", clean, "./root.yaml"},
		{"through its parent", clean, "../" + filepath.Base(dir) + "/root.yaml"},
		{"its absolute path", clean, clean},
		{"an absolute path not cleaned", clean, dir + "/./root.yaml"},
		// Read lexically, as resolve.SameDocument reads it, though no file system
		// opens it (GitHub #780).
		{"through a directory that does not exist", clean, dir + "/nope/../root.yaml"},
		{"its file name, from a source path not cleaned", dir + "/./root.yaml", "root.yaml"},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			name := strings.ReplaceAll(c.name, " ", "-") + ".yaml"
			writeFile(t, dir, name, "components:\n  responses:\n"+
				"    Back: {$ref: '"+c.spelling+"#/components/responses/R'}\n"+
				"    Alias: {$ref: '"+c.spelling+"#/components/responses/RA'}\n")
			root := strings.Replace(backRoot(backPath("/a", name+"#/components/responses/Back")+
				backPath("/b", name+"#/components/responses/Alias")),
				"components:\n  responses:\n", "components:\n  responses:\n    RA: {$ref: '#/components/responses/R'}\n", 1)
			got, diags, err := Load(t.Context(), 0,
				compilers.Source{Path: c.source, Data: []byte(root)}, Options{AllowExternalRefs: true})
			require.NoError(t, err)
			require.NotNil(t, got, "%+v", diags)

			assert.Empty(t, cmp.Diff([]string{" openapi/validation/validation-required-field"}, diagLines(diags)))
			r, ok := got.Doc.Components.Responses.Get("R")
			require.True(t, ok)
			for _, path := range []string{"/a", "/b"} {
				assert.Same(t, r.GetObject(), response200(t, got.Doc, path).GetObject(),
					"%s reaches the source's own declaration, although no root.yaml is on disk", path)
			}
		})
	}
}

// TestHold_ARelativeSourceIsReachedHoweverItIsSpelled pins GitHub #759 for a
// source named relative to the working directory, which a back reference can
// spell otherwise: by climbing out of its directory and back, or by an absolute
// path. Each reaches the source's own path item, whose references are resolved
// as the source's are, where a copy of it would hold them unresolved and the
// lowering would drop what they name.
//
// Not run in parallel: the source's path is relative to the working directory.
func TestHold_ARelativeSourceIsReachedHoweverItIsSpelled(t *testing.T) {
	dir := t.TempDir()
	api := filepath.Join(dir, "api")
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "common"), 0o750))
	require.NoError(t, os.MkdirAll(api, 0o750))
	t.Chdir(api)
	for _, c := range []struct {
		name, file, spelling string
	}{
		{"climbing out of its directory", "../common/x.yaml", "../api/root.yaml"},
		{"from beside it", "beside.yaml", "root.yaml"},
		{"by its absolute path", "absolute.yaml", filepath.Join(api, "root.yaml")},
		{"by an absolute path not cleaned", "unclean.yaml", api + "/./root.yaml"},
	} {
		t.Run(c.name, func(t *testing.T) {
			writeFile(t, api, c.file, "paths:\n  /a: {$ref: '"+c.spelling+"#/components/pathItems/P'}\n")
			root := `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /a: {$ref: '` + c.file + `#/paths/~1a'}
components:
  pathItems:
    P:
      get:
        parameters: [{$ref: '#/components/parameters/Q'}]
        responses: {"200": {$ref: '#/components/responses/R'}}
  parameters:
    Q: {name: q, in: query, schema: {type: string}}
  responses:
    R: {description: ok}
`
			got, diags, err := Load(t.Context(), 0,
				compilers.Source{Path: "root.yaml", Data: []byte(root)}, Options{AllowExternalRefs: true})
			require.NoError(t, err)
			require.NotNil(t, got, "%+v", diags)

			assert.Empty(t, diags)
			p, ok := got.Doc.Components.PathItems.Get("P")
			require.True(t, ok)
			a, ok := got.Doc.Paths.Get("/a")
			require.True(t, ok)
			require.Same(t, p.GetObject(), a.GetObject(), "the source's own path item")
			get := a.GetObject().Get()
			require.NotNil(t, get)
			require.Len(t, get.GetParameters(), 1)
			assert.NotNil(t, get.GetParameters()[0].GetObject(), "its parameter $ref is resolved")
			resp, ok := get.GetResponses().Get("200")
			require.True(t, ok)
			assert.NotNil(t, resp.GetObject(), "and its response $ref")
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

// TestHold_APanicStillHoldsTheSpellingsRecorded pins that a walk which stops
// early still holds the source under each spelling recorded, and the objects
// recorded there that it reached, where a second resolution would otherwise
// find the spelling unheld and read the file.
func TestHold_APanicStillHoldsTheSpellingsRecorded(t *testing.T) {
	t.Parallel()
	doc := &soa.OpenAPI{}
	root := &yaml.Node{Kind: yaml.DocumentNode}
	read := newExternalReads(sourceDocument{path: "root.yaml", data: []byte("x"), root: root})
	read.spelled.note("spelled.yaml", "/first")
	read.spelled.note("spelled.yaml", "/second")
	reader := newExternal(doc, Options{}, read)
	items := func(yield func(soa.WalkItem) bool) {
		if !yield(fakeWalkItem("first", fakeResolvable{ref: "#/first"})) {
			return
		}
		panic("boom")
	}

	require.NotPanics(t, func() { reader.holdWalked(items) })

	_, ok := doc.GetCachedExternalDocument("spelled.yaml")
	assert.True(t, ok, "the spelling's document")
	_, ok = doc.GetCachedReferencedObject("spelled.yaml#/first")
	assert.True(t, ok, "the object the walk reached")
	_, ok = doc.GetCachedReferencedObject("spelled.yaml#/second")
	assert.False(t, ok, "and not the one it did not")
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
			"https://example.com/a.yaml", true},
		{"another spelling of a source named by URL", sourceDocument{path: "https://example.com/a.yaml", root: tree},
			"https://example.com/./a.yaml", false},
		{"from a source whose name parses as a URL", sourceDocument{path: "x:root.yaml", root: tree},
			"x:root.yaml", true},
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

// TestHold_ARelativeSourceResolvesItsComponentAsItDoesInEitherOrder pins that
// the source's own component is resolved as the source resolves it when a back
// reference spelling the source otherwise reaches it first. Resolved against
// that spelling, its $ref named the file beside it otherwise, so the file was
// read twice, and a finding in it was named by whichever spelling came first.
//
// Not run in parallel: the source's path is relative to the working directory.
func TestHold_ARelativeSourceResolvesItsComponentAsItDoesInEitherOrder(t *testing.T) {
	dir := t.TempDir()
	api := filepath.Join(dir, "api")
	require.NoError(t, os.MkdirAll(api, 0o750))
	writeFile(t, dir, "x.yaml", "components:\n  responses:\n    Back: {$ref: 'api/root.yaml#/components/responses/P'}\n")
	writeFile(t, api, "other.yaml", "components:\n  responses:\n    Q:\n      description: q\n"+
		"      links: {L: {operationId: getA, operationRef: '#/paths/~1a/get'}}\n")
	t.Chdir(api)
	back := backPath("/a", "../x.yaml#/components/responses/Back")
	internal := backPath("/z", "#/components/responses/P")
	root := func(paths string) string {
		return "openapi: 3.1.0\ninfo: {title: T, version: \"1\"}\npaths:\n" + paths +
			"components:\n  responses:\n    P: {$ref: './other.yaml#/components/responses/Q'}\n"
	}

	for _, spec := range []string{root(back + internal), root(internal + back)} {
		got, diags, err := Load(t.Context(), 0,
			compilers.Source{Path: "root.yaml", Data: []byte(spec)}, Options{AllowExternalRefs: true})
		require.NoError(t, err)
		require.NotNil(t, got, "%+v", diags)
		require.Len(t, diags, 1, "%+v", diags)
		assert.Equal(t, jsontext.Pointer("/components/responses/P"), diags[0].Provenance.Pointer)
		assert.Contains(t, diags[0].Message, "of other.yaml", "named as the source names it")
	}
}

// heldResolution returns spec's model, built as the source at path, and the
// resolution newResolution makes of it with external references allowed, which
// has held the source where the resolver looks for it.
func heldResolution(t *testing.T, spec, path string) (*soa.OpenAPI, *resolution) {
	t.Helper()
	data := []byte(spec)
	root, _, err := decodeStream(data)
	require.NoError(t, err)
	releaseAnchors(root)
	doc, valErrs, err := unmarshal(t.Context(), data, root)
	require.NoError(t, err)
	self := newSourceDocument(path, data, root, valErrs)
	opts := Options{AllowExternalRefs: true}
	reader := newExternal(doc, opts, newExternalReads(self))
	return doc, newResolution(t.Context(), pointerAt(0, overlay.Origin{}), doc, self, opts, &reader)
}

// TestExternal_OpenHoldsTheSourceItIsAskedFor pins what Open does with the
// source spelled as no key holds it: it holds the source under that spelling,
// so the next $ref spelling it so finds the source's own objects, and serves
// the bytes held for it, which the resolver reads none of, its tree being held.
func TestExternal_OpenHoldsTheSourceItIsAskedFor(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	doc, pass := heldResolution(t, backRoot(backPath("/a", "#/components/responses/R")), filepath.Join(dir, "root.yaml"))
	spelled := dir + "/./root.yaml"

	f, err := pass.reader.Open(spelled)

	require.NoError(t, err, "no root.yaml is on disk, and none is read")
	data, err := io.ReadAll(f)
	require.NoError(t, err)
	assert.Empty(t, data)
	_, err = f.Stat()
	require.ErrorIs(t, err, fs.ErrInvalid)
	require.NoError(t, f.Close())
	tree, ok := doc.GetCachedExternalDocument(spelled)
	require.True(t, ok)
	assert.Same(t, pass.reader.read.self.root, tree)
	held, ok := doc.GetCachedReferencedObject(spelled + "#/components/responses/R")
	require.True(t, ok)
	r, ok := doc.Components.Responses.Get("R")
	require.True(t, ok)
	assert.Same(t, r, held, "the source's own object, a response being no reference")
}

// TestHold_EachSpellingADocumentUsesIsHeldAhead pins which $refs in a document
// hold the source under their spelling, read against the key the document was
// read by: one with a document part naming the source, here an absolute path
// not cleaned, which the resolver keys as written. A spelling the source is
// held under already, a pointer alone, a $ref that is no string, another file
// and a URL hold nothing more.
func TestHold_EachSpellingADocumentUsesIsHeldAhead(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "root.yaml")
	doc, pass := heldResolution(t, backRoot(backPath("/a", "#/components/responses/R")), path)
	unclean := dir + "/./root.yaml"
	tree, ok := parseTree([]byte(`a: {$ref: '#/components/responses/R'}
b: {$ref: [root.yaml]}
c: {$ref: 'other.yaml#/x'}
d: {$ref: 'https://example.com/root.yaml#/x'}
e: [{$ref: '` + unclean + `#/components/responses/R'}, {$ref: '` + unclean + `'}, {$ref: '../root.yaml'}]
`))
	require.True(t, ok)

	pass.reader.holdSpellings(tree, filepath.Join(dir, "sub", "doc.yaml"))

	assert.Equal(t, []string{unclean}, pass.reader.read.spelled.keys(),
		"the spelling no key holds, and none of the source's own keys")
	held, ok := doc.GetCachedReferencedObject(unclean + "#/components/responses/R")
	require.True(t, ok)
	r, ok := doc.Components.Responses.Get("R")
	require.True(t, ok)
	assert.Same(t, r, held, "the source's own object, under the spelling")
	for _, key := range []string{filepath.Join(dir, "sub", "other.yaml"), "https://example.com/root.yaml"} {
		_, ok := doc.GetCachedExternalDocument(key)
		assert.False(t, ok, key)
	}
}

// TestHold_ASpellingTheSourceUsesForItselfIsHeldAhead pins that a $ref in the
// source naming the source's own file by a spelling no key holds, here an
// absolute path not cleaned, reaches the source's own declaration, as a $ref
// from another document spelling it so does.
func TestHold_ASpellingTheSourceUsesForItselfIsHeldAhead(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	got, diags := loadExternal(t, dir, backRoot(backPath("/a", dir+"/./root.yaml#/components/responses/R")), Options{})

	assert.Empty(t, cmp.Diff([]string{" openapi/validation/validation-required-field"}, diagLines(diags)))
	r, ok := got.Doc.Components.Responses.Get("R")
	require.True(t, ok)
	assert.Same(t, r.GetObject(), response200(t, got.Doc, "/a").GetObject())
}

// TestHold_ASpellingHeldAlreadyIsNotHeldAgain pins that each document naming
// the source by a spelling and a pointer it is held under costs nothing more:
// holding it again would store the object again, and a new stand-in for a reference
// object, once for each such document.
func TestHold_ASpellingHeldAlreadyIsNotHeldAgain(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	unclean := dir + "/./root.yaml"
	root := strings.Replace(backRoot(backPath("/a", "#/components/responses/RA")),
		"components:\n  responses:\n", "components:\n  responses:\n    RA: {$ref: '#/components/responses/R'}\n", 1)
	doc, pass := heldResolution(t, root, filepath.Join(dir, "root.yaml"))
	pass.reader.holdSite(unclean, "/components/responses/RA")
	stand, ok := doc.GetCachedReferencedObject(unclean + "#/components/responses/RA")
	require.True(t, ok)

	pass.reader.holdSite(unclean, "/components/responses/RA")

	again, ok := doc.GetCachedReferencedObject(unclean + "#/components/responses/RA")
	require.True(t, ok)
	assert.Same(t, stand, again)
}

// TestHold_AKeyTheSourceIsHeldWholeUnderIsNotHeldAgain pins the same for the
// source's own keys, which hold every object already: a $ref spelling the
// cleaned path stores nothing over the stand-in held there.
func TestHold_AKeyTheSourceIsHeldWholeUnderIsNotHeldAgain(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	root := strings.Replace(backRoot(backPath("/a", "#/components/responses/RA")),
		"components:\n  responses:\n", "components:\n  responses:\n    RA: {$ref: '#/components/responses/R'}\n", 1)
	doc, pass := heldResolution(t, root, dir+"/./root.yaml")
	clean := filepath.Join(dir, "root.yaml")
	stand, ok := doc.GetCachedReferencedObject(clean + "#/components/responses/RA")
	require.True(t, ok, "a reference object is held under the cleaned path as a stand-in")

	pass.reader.holdSite(clean, "/components/responses/RA")

	again, ok := doc.GetCachedReferencedObject(clean + "#/components/responses/RA")
	require.True(t, ok)
	assert.Same(t, stand, again)
}

// TestHold_SpellingsAskedForTogetherAreEachHeld pins that the source can be
// handed under several spellings at once, as a resolver reading references on
// more than one goroutine would ask, by a scan or by Open: the keys it records
// are guarded, so each spelling is held and none is lost to a concurrent write.
func TestHold_SpellingsAskedForTogetherAreEachHeld(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	doc, pass := heldResolution(t, backRoot(backPath("/a", "#/components/responses/R")), filepath.Join(dir, "root.yaml"))
	spellings := make([]string, 0, 16)
	for i := range 16 {
		spellings = append(spellings, dir+strings.Repeat("/.", i+1)+"/root.yaml")
	}

	var wg sync.WaitGroup
	for i, spelled := range spellings {
		wg.Go(func() {
			if i%2 == 0 {
				pass.reader.holdSite(spelled, "/components/responses/R")
				return
			}
			pass.reader.holdOpened(spelled)
		})
	}
	wg.Wait()

	for _, spelled := range spellings {
		_, ok := doc.GetCachedReferencedObject(spelled + "#/components/responses/R")
		assert.True(t, ok, spelled)
	}
}

// TestHold_ASecondResolutionHoldsTheSpellingsTheFirstFound pins that the source
// is held, for the document a second resolution rebuilds, under each spelling
// the first found: that resolution reads a prepared document's tree without
// preparing it again, so no $ref in it is read for a spelling then.
func TestHold_ASecondResolutionHoldsTheSpellingsTheFirstFound(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	spelled := dir + "/./root.yaml"
	doc, pass := heldResolution(t, backRoot(backPath("/a", "#/components/responses/R")), filepath.Join(dir, "root.yaml"))
	pass.reader.holdSite(spelled, "/components/responses/R")

	again, _ := heldResolution(t, backRoot(backPath("/a", "#/components/responses/R")), filepath.Join(dir, "root.yaml"))
	reader := newExternal(again, Options{}, pass.reader.read)
	reader.hold(t.Context())

	_, ok := again.GetCachedReferencedObject(spelled + "#/components/responses/R")
	assert.True(t, ok, "held under the spelling the first resolution found")
	_, ok = doc.GetCachedReferencedObject(spelled + "#/components/responses/R")
	assert.True(t, ok)
}

// TestHold_ASchemaRefNamingTheSourceCopiesNoBytesOfIt pins the cost of a schema
// $ref that names the source's file. The resolver reads the whole document
// such a $ref names, copying the bytes it holds for it, with nothing cached
// for the next one to reuse. The bytes held beside the source's tree are none
// of the source's, so no read copies the source, and none is left holding a
// copy.
func TestHold_ASchemaRefNamingTheSourceCopiesNoBytesOfIt(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "root.yaml")
	doc, pass := heldResolution(t, rootOfSchemas(
		"    A: {$ref: 'root.yaml#/components/schemas/B'}\n",
		"    B: {type: string}\n"), path)
	a, ok := doc.Components.Schemas.Get("A")
	require.True(t, ok)

	pass.visit("/components/schemas/A", a, a.GetReference())

	require.Empty(t, pass.failures)
	assert.Equal(t, "string", string(a.GetResolvedSchema().GetSchema().GetType()[0]))
	cached, ok := doc.GetCachedReferenceDocument(path)
	require.True(t, ok)
	assert.Empty(t, cached)
}

// TestHold_ASchemaCycleThroughTheSourceEndsAsACycle pins that a schema chain
// leaving the source and coming back to where it started ends as the cycle it
// is. The resolver follows a schema hop it finds resolved without tracking it,
// so the held schema the chain started from looped it until the stack ran out.
// This is the resolution the walk starts, before anything settles it.
func TestHold_ASchemaCycleThroughTheSourceEndsAsACycle(t *testing.T) {
	t.Parallel()
	dir := externalDir(t, map[string]string{"other.yaml": "A: {$ref: 'root.yaml#/components/schemas/W'}\n"})
	doc, pass := heldResolution(t, rootOfSchemas(
		"    P: {$ref: 'other.yaml#/A'}\n",
		"    W: {$ref: '#/components/schemas/P'}\n"), filepath.Join(dir, "root.yaml"))
	p, ok := doc.Components.Schemas.Get("P")
	require.True(t, ok)

	_, err := p.Resolve(t.Context(), pass.opts)

	require.ErrorContains(t, err, "circular reference detected")
}

// TestHold_ASourceComponentIsResolvedAgainstItsSelfInEitherOrder pins that the
// source's own component is resolved against the base its $self sets, as the
// source resolves it, whichever reaches it first: an internal $ref, or a $ref
// back into the source from another document. The resolver resolved what it
// had handed the back reference against the path it read the source by.
func TestHold_ASourceComponentIsResolvedAgainstItsSelfInEitherOrder(t *testing.T) {
	t.Parallel()
	srv := newTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		_, err := w.Write([]byte("components:\n  responses:\n    Q: {description: by self}\n"))
		assert.NoError(t, err)
	})
	dir := externalDir(t, map[string]string{
		"ext.yaml":   "components:\n  responses:\n    Back: {$ref: 'root.yaml#/components/responses/P'}\n",
		"other.yaml": "components:\n  responses:\n    Q: {description: by path}\n",
	})
	root := func(paths string) string {
		return "openapi: 3.2.0\n$self: " + srv.URL + "/api/root.yaml\ninfo: {title: T, version: \"1\"}\npaths:\n" +
			paths + "components:\n  responses:\n    P: {$ref: './other.yaml#/components/responses/Q'}\n"
	}
	// The source's own relative $refs resolve against $self, so ext.yaml is
	// named by its path.
	back := backPath("/a", filepath.Join(dir, "ext.yaml")+"#/components/responses/Back")
	internal := backPath("/z", "#/components/responses/P")

	reports := make([][]string, 0, 2)
	for _, spec := range []string{root(back + internal), root(internal + back)} {
		got, diags := loadExternal(t, dir, spec, Options{})
		reports = append(reports, diagLines(diags))
		for _, path := range []string{"/a", "/z"} {
			assert.Equal(t, "by self", response200(t, got.Doc, path).GetObject().GetDescription(), path)
		}
	}
	assert.Empty(t, cmp.Diff(reports[0], reports[1]), "the same in either order")
}

// TestHold_AnInternalRefResolvesThroughEveryAlias pins that holding the source
// leaves an internal $ref resolving as it does when no other document is read:
// through each alias the source declares, however many. Reached as a stand-in,
// each alias would cost a resumed resolution, and a chain longer than
// maxResolutionHops would stop unresolved.
func TestHold_AnInternalRefResolvesThroughEveryAlias(t *testing.T) {
	t.Parallel()
	var responses strings.Builder
	n := maxResolutionHops + 8
	for i := range n {
		fmt.Fprintf(&responses, "    R%d: {$ref: '#/components/responses/R%d'}\n", i, i+1)
	}
	fmt.Fprintf(&responses, "    R%d: {description: ok}\n", n)
	root := "openapi: 3.1.0\ninfo: {title: T, version: \"1\"}\npaths:\n" +
		backPath("/a", "#/components/responses/R0") + "components:\n  responses:\n" + responses.String()

	got, diags := loadExternal(t, t.TempDir(), root, Options{})

	assert.Empty(t, diags)
	last, ok := got.Doc.Components.Responses.Get(fmt.Sprintf("R%d", n))
	require.True(t, ok)
	assert.Same(t, last.GetObject(), response200(t, got.Doc, "/a").GetObject())
}

// TestStandIn pins the stand-in held for each kind of reference object: an
// empty object of the kind, which mend knows to stand for the object. A schema
// has none.
func TestStandIn(t *testing.T) {
	t.Parallel()
	read := newExternalReads(sourceDocument{})
	for _, obj := range []resolvable{
		&soa.ReferencedPathItem{}, &soa.ReferencedParameter{}, &soa.ReferencedHeader{},
		&soa.ReferencedRequestBody{}, &soa.ReferencedResponse{}, &soa.ReferencedExample{},
		&soa.ReferencedLink{}, &soa.ReferencedCallback{}, &soa.ReferencedSecurityScheme{},
	} {
		stand, ok := read.standIn(obj)
		require.True(t, ok, "%T", obj)
		assert.IsType(t, obj, stand)
		assert.NotSame(t, obj, stand)
		stood, ok := read.stoodFor(stand)
		require.True(t, ok, "%T", obj)
		assert.Same(t, obj, stood)
	}
	_, ok := read.standIn(oas3.NewJSONSchemaFromReference("#/components/schemas/S"))
	assert.False(t, ok, "a schema is held as no stand-in")
	_, ok = read.stoodFor(&soa.ReferencedResponse{})
	assert.False(t, ok, "an object made elsewhere stands for nothing")
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
