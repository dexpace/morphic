package load

import (
	"context"
	"encoding/json/jsontext"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/speakeasy-api/openapi/references"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/ir"
)

// rootOfSchemas is a source whose component schemas are entries, written in
// the order given, each a line of the components.schemas mapping.
func rootOfSchemas(entries ...string) string {
	return "openapi: 3.1.0\ninfo: {title: T, version: \"1\"}\npaths: {}\ncomponents:\n  schemas:\n" +
		strings.Join(entries, "")
}

// TestSettle_AChainInAnotherDocumentResolvesInEitherOrder pins GitHub #761.
// Each row has an entry whose chain takes a further hop inside another
// document, and an earlier one that resolves that document first. The library
// then reports the document as its cached bytes, and the hop was resolved
// against them. Each row is written in the order that failed and is compiled
// reversed too: both must resolve every chain to an object, with nothing
// reported.
func TestSettle_AChainInAnotherDocumentResolvesInEitherOrder(t *testing.T) {
	t.Parallel()
	const other = `components:
  schemas:
    Base: {type: object}
    Alias: {$ref: '#/components/schemas/Base'}
    C: {oneOf: [{type: string}, {type: integer}]}
    IntoOneOf: {$ref: '#/components/schemas/C/oneOf/0'}
    P: {type: object, properties: {p: {type: string}}}
    IntoProperty: {$ref: '#/components/schemas/P/properties/p'}
    BesideOneOf: {$ref: '#/components/schemas/Base', oneOf: [{type: object}]}
    BesideDescription: {$ref: '#/components/schemas/Base', description: d}
    BesideEnum: {$ref: '#/components/schemas/Base', enum: [{}]}
    ToThird: {$ref: './third.yaml#/components/schemas/T'}
`
	const third = `components:
  schemas:
    U: {type: object}
    T: {$ref: '#/components/schemas/U'}
`
	// A document whose root declares $id is named by it in each record, not by
	// the path it was read from, so only its bytes say which tree it is.
	const byID = `$id: 'https://example.com/schemas/by-id.yaml'
components:
  schemas:
    Base: {type: object}
    Alias: {$ref: '#/components/schemas/Base'}
`
	ref := func(name, target string) string {
		return "    " + name + ": {$ref: './" + target + "'}\n"
	}
	for _, c := range []struct {
		name           string
		earlier, chain string
	}{
		{"an alias", ref("Base", "other.yaml#/components/schemas/Base"),
			ref("Alias", "other.yaml#/components/schemas/Alias")},
		{"a hop into a oneOf branch", ref("C", "other.yaml#/components/schemas/C"),
			ref("IntoOneOf", "other.yaml#/components/schemas/IntoOneOf")},
		{"a hop into a property", ref("P", "other.yaml#/components/schemas/P"),
			ref("IntoProperty", "other.yaml#/components/schemas/IntoProperty")},
		{"an earlier $ref nested in a schema",
			"    Holder: {type: object, properties: {h: {$ref: './other.yaml#/components/schemas/Base'}}}\n",
			ref("Alias", "other.yaml#/components/schemas/Alias")},
		{"a oneOf beside the $ref", ref("Base", "other.yaml#/components/schemas/Base"),
			ref("BesideOneOf", "other.yaml#/components/schemas/BesideOneOf")},
		{"a description beside the $ref", ref("Base", "other.yaml#/components/schemas/Base"),
			ref("BesideDescription", "other.yaml#/components/schemas/BesideDescription")},
		{"an enum beside the $ref", ref("Base", "other.yaml#/components/schemas/Base"),
			ref("BesideEnum", "other.yaml#/components/schemas/BesideEnum")},
		{"a hop in a third document", ref("U", "third.yaml#/components/schemas/U"),
			ref("ToThird", "other.yaml#/components/schemas/ToThird")},
		{"a document named by its $id", ref("Base", "by-id.yaml#/components/schemas/Base"),
			ref("Alias", "by-id.yaml#/components/schemas/Alias")},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			dir := externalDir(t, map[string]string{"other.yaml": other, "third.yaml": third, "by-id.yaml": byID})
			for _, root := range []string{rootOfSchemas(c.earlier, c.chain), rootOfSchemas(c.chain, c.earlier)} {
				got, diags := loadExternal(t, dir, root, Options{})
				assert.Empty(t, diags, "nothing is reported, in either order")
				for name, schema := range got.Doc.Components.Schemas.All() {
					if !schema.IsReference() {
						continue
					}
					trail := resolutionTrail(schema)
					assert.Empty(t, trail.stopped, "%s's chain stops at no reference", name)
					assert.NotEmpty(t, trail.target, "%s's chain ends on an object", name)
				}
			}
		})
	}
}

// TestSettle_AChainResolvesInTheSecondPassToo pins GitHub #761 through GitHub
// #538's second resolution, which a respelled anchored URL forces. That pass
// prepares each file it reads again, so a document named by its $id, which
// only its digest identifies, must still be found by it: the chain resolves,
// with nothing reported, as it does when no second pass runs.
func TestSettle_AChainResolvesInTheSecondPassToo(t *testing.T) {
	t.Parallel()
	trigger, _ := countingServer(t, anchoredExternalDoc)
	dir := externalDir(t, map[string]string{"by-id.yaml": `$id: 'https://example.com/schemas/by-id.yaml'
components:
  schemas:
    Base: {type: object}
    Alias: {$ref: '#/components/schemas/Base'}
`})
	schemas := "components:\n  schemas:\n" +
		"    Base: {$ref: './by-id.yaml#/components/schemas/Base'}\n" +
		"    Alias: {$ref: './by-id.yaml#/components/schemas/Alias'}\n"
	for _, paths := range []string{"paths: {}\n",
		"paths:\n  /x: {$ref: \"" + respelled(trigger.URL, "HTTP") + "/ext.yaml#/paths/~1x\"}\n"} {
		doc, diags, err := resolveSpec(t, "openapi: 3.1.0\ninfo: {title: T, version: \"1\"}\n"+paths+schemas,
			filepath.Join(dir, "root.yaml"))
		require.NoError(t, err)
		assert.Empty(t, diags, paths)
		alias, ok := doc.Components.Schemas.Get("Alias")
		require.True(t, ok)
		assert.NotEmpty(t, resolutionTrail(alias).target, "Alias's chain ends on an object: %s", paths)
	}
}

// TestSettle_AChainTargetIsValidatedInEitherOrder pins what GitHub #761 cost
// once reached objects were validated: a chain that failed ended on no object,
// so whether its target's finding was reported followed declaration order.
func TestSettle_AChainTargetIsValidatedInEitherOrder(t *testing.T) {
	t.Parallel()
	dir := externalDir(t, map[string]string{"other.yaml": `components:
  schemas:
    Base: {type: object}
    Bad: {type: string, minLength: -1}
    Alias: {$ref: '#/components/schemas/Bad'}
`})
	base := "    Base: {$ref: './other.yaml#/components/schemas/Base'}\n"
	alias := "    Alias: {$ref: './other.yaml#/components/schemas/Alias'}\n"

	reports := make([][]string, 0, 2)
	for _, root := range []string{rootOfSchemas(base, alias), rootOfSchemas(alias, base)} {
		_, diags := loadExternal(t, dir, root, Options{})
		var got []string
		for _, d := range diags {
			got = append(got, string(d.Provenance.Pointer)+" "+d.Code)
		}
		reports = append(reports, got)
	}
	want := []string{"/components/schemas/Alias openapi/validation/validation-invalid-schema"}
	for _, got := range reports {
		assert.Empty(t, cmp.Diff(want, got), "the finding in the chain's target is reported, at the $ref reaching it")
	}
}

// TestSettle_ARecordIsMendedToWhatTheUncachedReadReports pins mend on each
// thing a record can say it resolved against. A record reporting a document's
// bytes is given the tree prepared from them, as a read the library had not
// cached reports it. One reporting the source, by its bytes or its tree, is
// given the source's model, which an internal $ref resolves against. Anything
// else is left as it is.
func TestSettle_ARecordIsMendedToWhatTheUncachedReadReports(t *testing.T) {
	t.Parallel()
	doc := &soa.OpenAPI{}
	sourceRoot, otherTree := &yaml.Node{Kind: yaml.DocumentNode}, &yaml.Node{Kind: yaml.DocumentNode}
	sourceBytes, otherBytes := []byte("openapi: 3.1.0\n"), []byte("components: {}\n")
	read := newExternalReads(sourceDocument{path: "root.yaml", data: sourceBytes, root: sourceRoot})
	read.recordTree("other.yaml", otherTree, otherBytes)

	for _, c := range []struct {
		name    string
		was     any
		want    any
		changed bool
	}{
		{"another document's bytes", slices.Clone(otherBytes), otherTree, true},
		{"the source's bytes", slices.Clone(sourceBytes), doc, true},
		{"the source's tree", sourceRoot, doc, true},
		{"bytes no tree was prepared from", []byte("unknown"), []byte("unknown"), false},
		{"another document's tree", otherTree, otherTree, false},
		{"the source's model", doc, doc, false},
		{"nothing", nil, nil, false},
	} {
		document := c.was
		changed := read.mend(chain{records: []record{{path: "other.yaml", document: &document}}}, doc)
		assert.Equal(t, c.want, document, c.name)
		assert.Equal(t, c.changed, changed, "%s: mend reports a change only when it makes one", c.name)
	}
}

// TestTreeFor pins which tree a document's bytes are taken to be. The key the
// record's path names is asked first, a URL's as the request built from it
// spells it, since two documents can be read from the same bytes. Then the
// digest alone, which a record naming its document by $id needs: it names the
// first tree prepared from those bytes, whichever key or pass prepared another
// since, as any of them holds what the bytes do. The last answer for each path
// is kept for the same bytes, but not for none, which every empty document
// shares.
func TestTreeFor(t *testing.T) {
	t.Parallel()
	data := []byte("same bytes")
	a, b := &yaml.Node{}, &yaml.Node{}

	t.Run("by digest", func(t *testing.T) {
		t.Parallel()
		r := newExternalReads(sourceDocument{})
		r.recordTree("a.yaml", a, data)
		assert.Same(t, a, r.treeFor("https://example.com/by-id.yaml", slices.Clone(data)))
	})
	t.Run("by digest, once its document is prepared again", func(t *testing.T) {
		t.Parallel()
		r := newExternalReads(sourceDocument{})
		r.recordTree("a.yaml", a, data)
		r.recordTree("a.yaml", b, data)
		r.recordTree("c.yaml", b, data)
		assert.Same(t, a, r.treeFor("https://example.com/by-id.yaml", slices.Clone(data)),
			"neither a second pass nor a second key leaves the bytes naming no tree")
	})
	t.Run("by path, when the digest is shared", func(t *testing.T) {
		t.Parallel()
		r := newExternalReads(sourceDocument{})
		r.recordTree("a.yaml", a, data)
		r.recordTree("b.yaml", b, data)
		assert.Same(t, a, r.treeFor("a.yaml", slices.Clone(data)))
		assert.Same(t, b, r.treeFor("b.yaml", slices.Clone(data)))
		assert.Same(t, a, r.treeFor("c.yaml", slices.Clone(data)), "a path nothing was prepared under goes by digest")
	})
	t.Run("by the key a respelled URL was prepared under", func(t *testing.T) {
		t.Parallel()
		r := newExternalReads(sourceDocument{})
		r.recordTree("http://host/a.yaml", a, data)
		r.recordTree("http://host/b.yaml", b, data)
		assert.Same(t, b, r.treeFor("HTTP://host/b.yaml", slices.Clone(data)),
			"the resolver's spelling names the tree its request's spelling was prepared under")
	})
	t.Run("not by a path whose bytes were others", func(t *testing.T) {
		t.Parallel()
		r := newExternalReads(sourceDocument{})
		r.recordTree("a.yaml", a, data)
		r.recordTree("b.yaml", b, []byte("other bytes"))
		assert.Same(t, a, r.treeFor("b.yaml", slices.Clone(data)))
		r.recordTree("c.yaml", b, []byte("sane bytes"))
		assert.Same(t, a, r.treeFor("c.yaml", slices.Clone(data)), "bytes of the same length are compared too")
	})
	t.Run("memoized for the same bytes", func(t *testing.T) {
		t.Parallel()
		r := newExternalReads(sourceDocument{})
		r.recordTree("a.yaml", a, data)
		cached := slices.Clone(data)
		require.Same(t, a, r.treeFor("a.yaml", cached))
		r.recordTree("a.yaml", b, data)
		assert.Same(t, a, r.treeFor("a.yaml", cached), "the bytes asked about last keep their answer")
		r.recordTree("c.yaml", b, data)
		assert.Same(t, b, r.treeFor("c.yaml", cached), "the same bytes under another path are looked up afresh")
		assert.Same(t, b, r.treeFor("a.yaml", slices.Clone(data)), "so are other bytes")
		assert.Same(t, b, r.treeFor("a.yaml", cached), "and only each path's last answer is kept")
		for range 8 {
			r.treeFor("a.yaml", slices.Clone(data))
		}
		assert.Len(t, r.mended, 2, "one answer a path, so no copy of the bytes is held past the next")
	})
	t.Run("not memoized for none", func(t *testing.T) {
		t.Parallel()
		r := newExternalReads(sourceDocument{})
		r.recordTree("a.yaml", a, nil)
		r.recordTree("b.yaml", b, nil)
		assert.Same(t, a, r.treeFor("a.yaml", nil))
		assert.Same(t, b, r.treeFor("b.yaml", []byte{}), "an empty document's answer is its own path's")
	})
}

// TestSettle_AResumedResolutionFailsWhereItStillFails pins settle through a
// resolution that stalls on a record mending changes and, resumed, fails for a
// reason mending cannot touch: the failure returned is the missing target's,
// past the hop that stalled, not the bytes'.
func TestSettle_AResumedResolutionFailsWhereItStillFails(t *testing.T) {
	t.Parallel()
	dir := externalDir(t, map[string]string{"other.yaml": `components:
  schemas:
    Base: {type: object}
    Alias: {$ref: '#/components/schemas/Missing'}
`})
	got, diags := loadExternal(t, dir, rootOfSchemas(
		"    Base: {$ref: './other.yaml#/components/schemas/Base'}\n",
		"    Alias: {$ref: './other.yaml#/components/schemas/Alias'}\n"), Options{})

	require.Len(t, diags, 1, "%+v", diags)
	assert.Equal(t, jsontext.Pointer("/components/schemas/Alias"), diags[0].Provenance.Pointer)
	assert.Equal(t, ir.SeverityError, diags[0].Severity)
	assert.Contains(t, diags[0].Message, `through "#/components/schemas/Missing" in`,
		"the failure is the missing target's, not the bytes'")
	assert.NotContains(t, diags[0].Message, "expected index", "the hop was resumed against the tree")
	alias, ok := got.Doc.Components.Schemas.Get("Alias")
	require.True(t, ok)
	assert.Equal(t, "#/components/schemas/Missing", string(resolutionTrail(alias).stopped))
}

// TestSettle_AFailureMendingCannotTouchIsNotResumed pins settle's loop guard: a
// resolution whose chain holds no record mending changes is returned as it
// failed, with its findings, and never resumed.
func TestSettle_AFailureMendingCannotTouchIsNotResumed(t *testing.T) {
	t.Parallel()
	resumed := 0
	r := fakeResolvable{ref: "#/x", resolve: func(context.Context, references.ResolveOptions) ([]error, error) {
		resumed++
		return nil, errors.New("again")
	}}
	reader := newExternal(&soa.OpenAPI{}, Options{}, newExternalReads(sourceDocument{}))
	failed := errors.New("failed")

	vErrs, err := reader.settle(t.Context(), r, references.ResolveOptions{}, []error{assert.AnError}, failed)

	assert.Zero(t, resumed, "nothing a mend could change, so nothing to resume")
	require.ErrorIs(t, err, failed)
	assert.Equal(t, []error{assert.AnError}, vErrs)
}

// byID is a document whose root declares $id, so each record of a hop into it
// names it by the $id, and only its bytes say which tree it is.
const byID = `$id: 'https://example.com/schemas/by-id.yaml'
components:
  schemas:
    Base: {type: object}
    Alias: {$ref: '#/components/schemas/Base'}
`

// TestSettle_AChainResolvesUnderEitherSpellingOfItsDocument pins GitHub #761 on
// a document read under two keys: once by its absolute path, once relative to a
// source named by a relative path. Each key prepares a tree of its own from the
// same bytes, so the digest the record is found by names two trees. Written
// with the absolute spelling first, the order that failed, and reversed, the
// chain must resolve, with nothing reported.
func TestSettle_AChainResolvesUnderEitherSpellingOfItsDocument(t *testing.T) {
	t.Parallel()
	dir := externalDir(t, map[string]string{"by-id.yaml": byID})
	cwd, err := os.Getwd()
	require.NoError(t, err)
	source, err := filepath.Rel(cwd, filepath.Join(dir, "root.yaml"))
	require.NoError(t, err)
	absolute := "    First: {$ref: '" + filepath.Join(dir, "by-id.yaml") + "#/components/schemas/Base'}\n"
	relative := "    Base: {$ref: './by-id.yaml#/components/schemas/Base'}\n" +
		"    Alias: {$ref: './by-id.yaml#/components/schemas/Alias'}\n"

	for _, spec := range []string{rootOfSchemas(absolute, relative), rootOfSchemas(relative, absolute)} {
		doc, diags, err := resolveSpec(t, spec, source)
		require.NoError(t, err)
		assert.Empty(t, diags, spec)
		alias, ok := doc.Components.Schemas.Get("Alias")
		require.True(t, ok)
		assert.NotEmpty(t, resolutionTrail(alias).target, "Alias's chain ends on an object: %s", spec)
	}
}

// TestSettle_AChainResolvesInDocumentsOfTheSameBytes pins GitHub #761 on two
// documents served with the same bytes, each reached by a URL its request
// respells. Each chain must resolve, with nothing reported, and each record
// must hold the tree prepared under the key of its own document: the
// resolver's spelling read as the request built from it reads it, not the
// tree the shared digest names. It holds in a second resolution too, which a
// respelled anchored document forces.
func TestSettle_AChainResolvesInDocumentsOfTheSameBytes(t *testing.T) {
	t.Parallel()
	const shared = "components:\n  schemas:\n    Base: {type: object}\n" +
		"    Alias: {$ref: '#/components/schemas/Base'}\n"
	srv := newTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		_, err := w.Write([]byte(shared))
		assert.NoError(t, err)
	})
	trigger, _ := countingServer(t, anchoredExternalDoc)
	url := respelled(srv.URL, "HTTP")
	schemas := "components:\n  schemas:\n" +
		"    BaseA: {$ref: '" + url + "/a.yaml#/components/schemas/Base'}\n" +
		"    AliasA: {$ref: '" + url + "/a.yaml#/components/schemas/Alias'}\n" +
		"    BaseB: {$ref: '" + url + "/b.yaml#/components/schemas/Base'}\n" +
		"    AliasB: {$ref: '" + url + "/b.yaml#/components/schemas/Alias'}\n"

	for _, paths := range []string{"paths: {}\n",
		"paths:\n  /x: {$ref: \"" + respelled(trigger.URL, "HTTP") + "/ext.yaml#/paths/~1x\"}\n"} {
		doc, diags, err := resolveSpec(t, "openapi: 3.1.0\ninfo: {title: T, version: \"1\"}\n"+paths+schemas, "root.yaml")
		require.NoError(t, err)
		assert.Empty(t, diags, paths)
		for name, file := range map[string]string{"AliasA": "/a.yaml", "AliasB": "/b.yaml"} {
			alias, ok := doc.Components.Schemas.Get(name)
			require.True(t, ok)
			c := resolutionChain(alias)
			assert.NotEmpty(t, c.trail().target, "%s's chain ends on an object: %s", name, paths)
			own, ok := doc.GetCachedExternalDocument(srv.URL + file)
			require.True(t, ok)
			for _, rec := range c.records {
				assert.Same(t, own, *rec.document, "%s's records hold its own document's tree: %s", name, paths)
			}
		}
	}
}
