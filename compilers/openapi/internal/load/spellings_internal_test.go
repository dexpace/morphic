package load

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
)

// responsesSource is a source with count component responses, R0 to R<count-1>.
// Every fourth is a $ref to R0, a reference object, which is held under a
// spelling no key holds as a stand-in.
func responsesSource(count int) string {
	var b strings.Builder
	b.WriteString("openapi: 3.1.0\ninfo: {title: T, version: \"1\"}\npaths: {}\ncomponents:\n  responses:\n")
	for i := range count {
		if i%4 == 3 {
			fmt.Fprintf(&b, "    R%d: {$ref: '#/components/responses/R0'}\n", i)
			continue
		}
		fmt.Fprintf(&b, "    R%d: {description: ok}\n", i)
	}
	return b.String()
}

// spellingOf returns the i-th spelling of the source at dir/root.yaml that no
// key holds: dir, then i+1 current-directory steps. Each names the file the
// source is, and each differs from the others, as an absolute path is kept as
// written.
func spellingOf(dir string, i int) string {
	return dir + strings.Repeat("/.", i+1) + "/root.yaml"
}

// backRef is a $ref spelled by spelling number spelling, naming response number
// response.
type backRef struct{ spelling, response int }

// holdBackRefs holds a source of objects responses for each $ref in refs, as
// the scan of a document that writes them does, and returns the cache the
// resolver would look them up in, with the spellings used.
func holdBackRefs(t *testing.T, objects int, refs []backRef) (*soa.OpenAPI, []string) {
	t.Helper()
	dir := t.TempDir()
	doc, pass := heldResolution(t, responsesSource(objects), filepath.Join(dir, "root.yaml"))
	var text strings.Builder
	spellings := map[int]string{}
	for i, r := range refs {
		spellings[r.spelling] = spellingOf(dir, r.spelling)
		fmt.Fprintf(&text, "x%d: {$ref: '%s#/components/responses/R%d'}\n", i, spellings[r.spelling], r.response)
	}
	tree, ok := parseTree([]byte(text.String()))
	require.True(t, ok)

	pass.reader.holdSpellings(tree, filepath.Join(dir, "sub", "doc.yaml"))

	used := make([]string, 0, len(spellings))
	for _, s := range spellings {
		used = append(used, s)
	}
	return doc, used
}

// heldEntries counts the pairs of a spelling and a response the source's
// response is held under in doc's cache.
func heldEntries(doc *soa.OpenAPI, spellings []string, objects int) int {
	n := 0
	for _, s := range spellings {
		for j := range objects {
			if _, ok := doc.GetCachedReferencedObject(fmt.Sprintf("%s#/components/responses/R%d", s, j)); ok {
				n++
			}
		}
	}
	return n
}

// TestHold_WorkIsLinearInTheSpellings pins GitHub #772. What is stored for a
// spelling is the objects the $refs written with it name, not every object the
// source has: the resolver asks the cache for a spelling and a pointer a $ref
// writes, so the rest is never read. Counted, not timed: the entries stored
// are exactly the distinct pairs written, so doubling the $refs at most
// doubles them, where storing every object under every spelling multiplies the
// two counts.
func TestHold_WorkIsLinearInTheSpellings(t *testing.T) {
	t.Parallel()
	const objects = 32
	for name, shape := range map[string]func(n int) []backRef{
		"a spelling for each $ref, at a response each": func(n int) []backRef {
			refs := make([]backRef, n)
			for i := range refs {
				refs[i] = backRef{spelling: i, response: i % objects}
			}
			return refs
		},
		"one spelling at every response": func(n int) []backRef {
			refs := make([]backRef, 0, n)
			for i := range min(n, objects) {
				refs = append(refs, backRef{spelling: 0, response: i})
			}
			return refs
		},
		"a spelling for each $ref, at one response": func(n int) []backRef {
			refs := make([]backRef, n)
			for i := range refs {
				refs[i] = backRef{spelling: i, response: 0}
			}
			return refs
		},
		"the same pair written again and again": func(n int) []backRef {
			refs := make([]backRef, n)
			for i := range refs {
				refs[i] = backRef{spelling: i % 2, response: i % 2}
			}
			return refs
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			counted := func(n int) int {
				refs := shape(n)
				distinct := map[backRef]bool{}
				for _, r := range refs {
					distinct[r] = true
				}
				doc, spellings := holdBackRefs(t, objects, refs)
				got := heldEntries(doc, spellings, objects)
				require.Equal(t, len(distinct), got, "exactly the distinct pairs written, for %d $refs", n)
				return got
			}

			single, double := counted(16), counted(32)

			assert.LessOrEqual(t, double, 2*single, "doubling the $refs at most doubles what is stored")
		})
	}
}

// TestHold_OpenHoldsEveryObjectUpToTheCap pins what Open holds under a spelling
// no scanned $ref wrote: every object, for maxOpenedSpellings of them, and the
// document alone past that, where the resolver builds a copy. A name opened
// again is held already and spends nothing of the cap.
func TestHold_OpenHoldsEveryObjectUpToTheCap(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	doc, pass := heldResolution(t, responsesSource(4), filepath.Join(dir, "root.yaml"))
	for i := range maxOpenedSpellings + 1 {
		for _, name := range []string{spellingOf(dir, i), spellingOf(dir, 0)} {
			f, err := pass.reader.Open(name)
			require.NoError(t, err)
			require.NoError(t, f.Close())
		}
	}

	for i := range maxOpenedSpellings + 1 {
		key := spellingOf(dir, i)
		_, ok := doc.GetCachedExternalDocument(key)
		require.True(t, ok, "the document is held under spelling %d", i)
		_, ok = doc.GetCachedReferencedObject(key + "#/components/responses/R0")
		assert.Equal(t, i < maxOpenedSpellings, ok, "spelling %d holds the objects: %t", i, i < maxOpenedSpellings)
	}
}

// TestHold_ASecondResolutionHoldsWhatTheFirstDid pins that the record carries
// each way the first resolution held the source under a spelling to the
// second: the objects a scanned $ref names and no more, every object under a
// spelling Open held whole, and the document alone under one past the cap.
func TestHold_ASecondResolutionHoldsWhatTheFirstDid(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "root.yaml")
	_, pass := heldResolution(t, responsesSource(4), path)
	scanned := spellingOf(dir, 100)
	tree, ok := parseTree([]byte("x: {$ref: '" + scanned + "#/components/responses/R1'}\n"))
	require.True(t, ok)
	pass.reader.holdSpellings(tree, filepath.Join(dir, "sub", "doc.yaml"))
	schemaOnly := spellingOf(dir, 101)
	tree, ok = parseTree([]byte("y: {$ref: '" + schemaOnly + "#/components/schemas/S'}\n"))
	require.True(t, ok)
	pass.reader.holdSpellings(tree, filepath.Join(dir, "sub", "doc.yaml"))
	for i := range maxOpenedSpellings + 1 {
		f, err := pass.reader.Open(spellingOf(dir, i))
		require.NoError(t, err)
		require.NoError(t, f.Close())
	}

	again, _ := heldResolution(t, responsesSource(4), path)
	reader := newExternal(again, Options{}, pass.reader.read)
	reader.hold(t.Context())

	held := func(key, response string) bool {
		_, ok := again.GetCachedReferencedObject(key + "#/components/responses/" + response)
		return ok
	}
	assert.True(t, held(scanned, "R1"), "what the scanned $ref names")
	assert.False(t, held(scanned, "R2"), "and nothing more")
	_, ok = again.GetCachedExternalDocument(schemaOnly)
	assert.True(t, ok, "the document under a spelling whose $ref names no held object")
	assert.True(t, held(spellingOf(dir, 0), "R2"), "every object under a spelling Open held whole")
	last := spellingOf(dir, maxOpenedSpellings)
	_, ok = again.GetCachedExternalDocument(last)
	assert.True(t, ok, "the document under the spelling past the cap")
	assert.False(t, held(last, "R0"), "and no object")
}

// TestHold_EachBackReferenceReachesTheObjectItNamesThroughItsOwnSpelling pins
// that holding by reference answers every lookup holding every object did: each
// $ref in the source, spelled by a different path to the source's file, which
// is on no disk here, resolves to the source's own parameter. Without the
// source held under its spelling the resolver reads a file that is not there.
func TestHold_EachBackReferenceReachesTheObjectItNamesThroughItsOwnSpelling(t *testing.T) {
	t.Parallel()
	const objects, refs = 8, 24
	dir := t.TempDir()
	var spec strings.Builder
	spec.WriteString("openapi: 3.1.0\ninfo: {title: T, version: \"1\"}\npaths:\n")
	for i := range refs {
		fmt.Fprintf(&spec, "  /p%d:\n    get:\n      parameters:\n        - $ref: '%s#/components/parameters/P%d'\n"+
			"      responses: {\"200\": {description: ok}}\n", i, spellingOf(dir, i), i%objects)
	}
	spec.WriteString("components:\n  parameters:\n")
	for i := range objects {
		fmt.Fprintf(&spec, "    P%d: {name: p%d, in: query, schema: {type: string}}\n", i, i)
	}

	got, diags, err := Load(t.Context(), 0,
		compilers.Source{Path: filepath.Join(dir, "root.yaml"), Data: []byte(spec.String())}, Options{AllowExternalRefs: true})

	require.NoError(t, err)
	require.NotNil(t, got, "%+v", diags)
	assert.Empty(t, diags)
	for i := range refs {
		item, ok := got.Doc.Paths.Get(fmt.Sprintf("/p%d", i))
		require.True(t, ok)
		params := item.GetObject().Get().GetParameters()
		require.Len(t, params, 1)
		named, ok := got.Doc.Components.Parameters.Get(fmt.Sprintf("P%d", i%objects))
		require.True(t, ok)
		assert.Same(t, named.GetObject(), params[0].GetObject(), "$ref %d reaches the source's own parameter", i)
	}
}

// TestHold_AnAliasValuedRefIsHeldAsAPlainOneIs pins that a $ref written as a
// YAML alias to an anchored string is read as the model reads it: its spelling
// of the source is held by reference like any other, so it reaches the source's
// own parameter and not a copy of it, which the resolver builds, validates and
// holds unresolved wherever the scan did not hold the object ahead.
func TestHold_AnAliasValuedRefIsHeldAsAPlainOneIs(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	spec := "openapi: 3.1.0\ninfo: {title: T, version: \"1\"}\nx-r: &r '" + spellingOf(dir, 0) + "#/components/parameters/P0'\n" +
		"paths:\n  /a: {get: {parameters: [{$ref: *r}], responses: {\"200\": {description: ok}}}}\n" +
		"components:\n  parameters:\n    P0: {name: p0, in: query, schema: {type: string}}\n"

	got, diags := loadExternal(t, dir, spec, Options{})

	assert.Empty(t, diags)
	item, ok := got.Doc.Paths.Get("/a")
	require.True(t, ok)
	named, ok := got.Doc.Components.Parameters.Get("P0")
	require.True(t, ok)
	params := item.GetObject().Get().GetParameters()
	require.Len(t, params, 1)
	assert.Same(t, named.GetObject(), params[0].GetObject(), "the source's own parameter, not a copy of it")
}
