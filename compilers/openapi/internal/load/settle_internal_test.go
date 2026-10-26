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
	"github.com/speakeasy-api/openapi/jsonpointer"
	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/speakeasy-api/openapi/references"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/ir"
)

// libraryYAML is raw YAML a reference can name from the source, written after
// the components: a mapping, a sequence, nested mappings and a $ref to a
// response in other.yaml.
const libraryYAML = `x-lib:
  R: {description: raw}
  L: [{description: item}]
  N: {a: {b: {description: deep}}}
  Far: {$ref: './other.yaml#/components/responses/Bad'}
`

// rootOfResponses is a source whose component responses are entries, written in
// the order given, each a line of the components.responses mapping, followed by
// libraryYAML.
func rootOfResponses(entries ...string) string {
	return "openapi: 3.1.0\ninfo: {title: T, version: \"1\"}\npaths: {}\ncomponents:\n  responses:\n" +
		strings.Join(entries, "") + libraryYAML
}

// responseRef is the components.responses entry name, a $ref to ref.
func responseRef(name, ref string) string {
	return "    " + name + ": {$ref: '" + ref + "'}\n"
}

// assertChainsEnd requires each reference response of doc to end on an object,
// stopping at no reference.
func assertChainsEnd(t *testing.T, doc *soa.OpenAPI, msgAndArgs ...any) {
	t.Helper()
	checked := 0
	for name, response := range doc.Components.Responses.All() {
		if !response.IsReference() {
			continue
		}
		checked++
		trail := resolutionTrail(response)
		assert.Empty(t, trail.stopped, "%s's chain stops at no reference: %v", name, msgAndArgs)
		assert.False(t, trail.cut, "%s's chain is read to its end: %v", name, msgAndArgs)
		assert.NotEmpty(t, trail.docs, "%s's chain ends on an object: %v", name, msgAndArgs)
	}
	assert.Positive(t, checked, "the source holds a reference response: %v", msgAndArgs)
}

// TestSettle_AChainIntoTheSourceResolvesInEitherOrder pins GitHub #761. The
// library reports the source's bytes as the document a hop into it resolved
// against when it holds the object the hop names, so a further hop written as
// a pointer into the source's raw YAML was walked through bytes and failed.
// Each row is a chain that comes back to a response of the source's not yet
// resolved, compiled in the order that failed and reversed: both must resolve
// every chain, with nothing reported. A chain back under a spelling no key
// holds the source by reaches a stand-in instead, and is resumed alike.
func TestSettle_AChainIntoTheSourceResolvesInEitherOrder(t *testing.T) {
	t.Parallel()
	const other = "components:\n  responses:\n    Back: {$ref: 'root.yaml#/components/responses/Mid'}\n"
	for _, onward := range []struct{ name, mid, description string }{
		{"a pointer to a mapping", "{$ref: '#/x-lib/R'}", "raw"},
		{"a pointer through a sequence", "{$ref: '#/x-lib/L/0'}", "item"},
		{"a pointer through nested mappings", "{$ref: '#/x-lib/N/a/b'}", "deep"},
		{"a description beside the $ref", "{$ref: '#/x-lib/R', description: d}", "raw"},
		{"a pointer to a response of its own", "{$ref: '#/components/responses/Real'}", "real"},
	} {
		for _, via := range []struct{ name, ref string }{
			{"the source's file", "./root.yaml#/components/responses/Mid"},
			{"another document", "./other.yaml#/components/responses/Back"},
			{"a spelling of the source's file no key holds it by", "SPELLED/./root.yaml#/components/responses/Mid"},
		} {
			t.Run(onward.name+", through "+via.name, func(t *testing.T) {
				t.Parallel()
				dir := externalDir(t, map[string]string{"other.yaml": other})
				entries := []string{responseRef("Out", strings.Replace(via.ref, "SPELLED", dir, 1)),
					"    Mid: " + onward.mid + "\n", "    Real: {description: real}\n"}
				for _, order := range [][]string{entries, {entries[1], entries[0], entries[2]}} {
					got, diags := loadExternal(t, dir, rootOfResponses(order...), Options{})
					assert.Empty(t, diags, "nothing is reported, in either order")
					assertChainsEnd(t, got.Doc, order)
					out, ok := got.Doc.Components.Responses.Get("Out")
					require.True(t, ok)
					assert.Equal(t, onward.description, out.GetObject().GetDescription(), "Out reaches what Mid names: %v", order)
				}
			})
		}
	}
}

// TestSettle_AChainResolvesInTheSecondPassToo pins GitHub #761 through GitHub
// #538's second resolution, which a respelled anchored URL forces. That pass
// reads no file again, and holds the source again under the keys the first
// found, so a chain back into the source must still be resumed by it: the
// chain resolves, with nothing reported, as it does when no second pass runs.
func TestSettle_AChainResolvesInTheSecondPassToo(t *testing.T) {
	t.Parallel()
	trigger, _ := countingServer(t, anchoredExternalDoc)
	dir := t.TempDir()
	root := rootOfResponses(responseRef("Out", "./root.yaml#/components/responses/Mid"), responseRef("Mid", "#/x-lib/R"))
	for _, paths := range []string{"paths: {}\n",
		"paths:\n  /x: {$ref: \"" + respelled(trigger.URL, "HTTP") + "/ext.yaml#/paths/~1x\"}\n"} {
		doc, diags, err := resolveSpec(t, strings.Replace(root, "paths: {}\n", paths, 1), filepath.Join(dir, "root.yaml"))
		require.NoError(t, err)
		assert.Empty(t, diags, paths)
		assertChainsEnd(t, doc, paths)
		if paths != "paths: {}\n" {
			assertRecovered(t, doc)
		}
	}
}

// TestSettle_AChainTargetIsValidatedInEitherOrder pins what GitHub #761 cost
// once reached objects were validated: a chain that failed ended on no object,
// so whether its target's finding was reported followed declaration order.
func TestSettle_AChainTargetIsValidatedInEitherOrder(t *testing.T) {
	t.Parallel()
	dir := externalDir(t, map[string]string{"other.yaml": `components:
  responses:
    Bad: {description: bad, headers: {X: {schema: {type: string, minLength: -1}}}}
`})
	out := responseRef("Out", "./root.yaml#/components/responses/Mid")
	mid := responseRef("Mid", "#/x-lib/Far")

	reports := make([][]string, 0, 2)
	for _, root := range []string{rootOfResponses(out, mid), rootOfResponses(mid, out)} {
		_, diags := loadExternal(t, dir, root, Options{})
		var got []string
		for _, d := range diags {
			got = append(got, string(d.Provenance.Pointer)+" "+d.Code)
		}
		reports = append(reports, got)
	}
	want := []string{"/components/responses/Bad/headers/X/schema/minLength openapi/validation/validation-type-mismatch"}
	for _, got := range reports {
		assert.Empty(t, cmp.Diff(want, got), "the finding in the chain's target is reported, at its node")
	}
}

// TestSettle_ARecordIsMendedToWhatTheUncachedReadReports pins mend on each
// thing a record can say it resolved against. A record reporting a document's
// bytes is given the tree prepared from them, as a read the library had not
// cached reports it. One reporting the source, by its bytes or its tree, or by
// any bytes under a key the source is held under, is given the source's model,
// which an internal $ref resolves against, and the source's path. Anything else
// is left as it is.
func TestSettle_ARecordIsMendedToWhatTheUncachedReadReports(t *testing.T) {
	t.Parallel()
	doc := &soa.OpenAPI{}
	sourceRoot, otherTree := &yaml.Node{Kind: yaml.DocumentNode}, &yaml.Node{Kind: yaml.DocumentNode}
	sourceBytes, otherBytes := []byte("openapi: 3.1.0\n"), []byte("components: {}\n")
	read := newExternalReads(sourceDocument{path: "root.yaml", data: sourceBytes, root: sourceRoot})
	read.recordTree("other.yaml", otherTree, otherBytes)
	read.recordTree("held.yaml", sourceRoot, nil)

	for _, c := range []struct {
		name, path   string
		was, want    any
		changed      bool
		resolvedPath string
	}{
		{"another document's bytes", "other.yaml", slices.Clone(otherBytes), otherTree, true, "other.yaml"},
		{"the source's bytes", "other.yaml", slices.Clone(sourceBytes), doc, true, "root.yaml"},
		{"the bytes held for the source", "held.yaml", []byte{}, doc, true, "root.yaml"},
		{"the source's tree", "../dir/root.yaml", sourceRoot, doc, true, "root.yaml"},
		{"bytes no tree was prepared from", "other.yaml", []byte("unknown"), []byte("unknown"), false, "other.yaml"},
		{"another document's tree", "other.yaml", otherTree, otherTree, false, "other.yaml"},
		{"the source's model", "other.yaml", doc, doc, false, "other.yaml"},
		{"nothing", "other.yaml", nil, nil, false, "other.yaml"},
	} {
		document, path := c.was, c.path
		changed := read.mend(chain{records: []record{{path: path, document: &document, documentPath: &path}}}, doc)
		assert.Equal(t, c.want, document, c.name)
		assert.Equal(t, c.changed, changed, "%s: mend reports a change only when it makes one", c.name)
		assert.Equal(t, c.resolvedPath, path, "%s: the path the record says it read", c.name)
	}
}

// TestTreeFor pins which tree a document's bytes are taken to be. The key the
// record's path names is asked first, a URL's as the request built from it
// spells it, since two documents can be read from the same bytes. Then the
// digest alone, for a record naming a key nothing was prepared under: the
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
// reason mending cannot touch. Two references name one response in another
// document, which names a response that is not there: the first fails at it,
// and the library, handed the response it left unresolved with the document's
// bytes, fails the second on the bytes. The failure returned for both is the
// missing target's, past the hop that stalled.
func TestSettle_AResumedResolutionFailsWhereItStillFails(t *testing.T) {
	t.Parallel()
	dir := externalDir(t, map[string]string{"other.yaml": `components:
  responses:
    Alias: {$ref: '#/components/responses/Missing'}
`})
	got, diags := loadExternal(t, dir, rootOfResponses(
		responseRef("First", "./other.yaml#/components/responses/Alias"),
		responseRef("Second", "./other.yaml#/components/responses/Alias")), Options{})

	require.Len(t, diags, 2, "%+v", diags)
	for i, name := range []string{"First", "Second"} {
		assert.Equal(t, jsontext.Pointer("/components/responses/"+name), diags[i].Provenance.Pointer)
		assert.Equal(t, ir.SeverityError, diags[i].Severity)
		assert.Contains(t, diags[i].Message, `through "#/components/responses/Missing" in`,
			"the failure is the missing target's, not the bytes'")
		assert.NotContains(t, diags[i].Message, "expected index", "%s: the hop was resumed against the tree", name)
		response, ok := got.Doc.Components.Responses.Get(name)
		require.True(t, ok)
		assert.Equal(t, "#/components/responses/Missing", string(resolutionTrail(response).stopped), name)
	}
}

// TestSettle_AFailureMendingCannotTouchIsNotResumed pins settle's loop guard: a
// resolution whose chain holds no record mending changes is returned as it
// failed, and never resumed.
func TestSettle_AFailureMendingCannotTouchIsNotResumed(t *testing.T) {
	t.Parallel()
	resumed := 0
	r := fakeResolvable{ref: "#/x", resolve: func(context.Context, references.ResolveOptions) ([]error, error) {
		resumed++
		return nil, errors.New("again")
	}}
	reader := newExternal(t.Context(), &soa.OpenAPI{}, Options{}, newExternalReads(sourceDocument{}))
	failed := errors.New("failed")

	err := reader.settle(t.Context(), r, references.ResolveOptions{}, failed)

	assert.Zero(t, resumed, "nothing a mend could change, so nothing to resume")
	require.ErrorIs(t, err, failed)
}

// report is what a site's one diagnostic says: an unresolved reference, with
// says in its message.
type report struct{ site, says string }

// assertReports requires got to be one unresolved-reference report per site of
// want, each saying what want does.
func assertReports(t *testing.T, got []ir.Diagnostic, want []report, msgAndArgs ...any) {
	t.Helper()
	if !assert.Len(t, got, len(want), msgAndArgs...) {
		return
	}
	slices.SortFunc(got, func(a, b ir.Diagnostic) int {
		return strings.Compare(string(a.Provenance.Pointer), string(b.Provenance.Pointer))
	})
	slices.SortFunc(want, func(a, b report) int { return strings.Compare(a.site, b.site) })
	for i, w := range want {
		assert.Equal(t, jsontext.Pointer(w.site), got[i].Provenance.Pointer, msgAndArgs...)
		assert.Equal(t, diag.UnresolvedRef, got[i].Code, "%s: %v", w.site, msgAndArgs)
		assert.Contains(t, got[i].Message, w.says, msgAndArgs...)
	}
}

// TestSettle_ACycleIsReportedNotResumed pins settle on a resolution whose hops
// lead back into what it resolved. A cycle is reported as the library reports
// it, so which members of a failing chain are reported follows the walk's
// order (GitHub #767); resuming one would only walk it again, up to the bound
// on resumptions, so none is, and the work settle spent says whether any was.
// Each row is resolved in the order that stalls, where a row stalls, and
// reversed. Where only a cycle is met, it is reported alike in both orders.
func TestSettle_ACycleIsReportedNotResumed(t *testing.T) {
	t.Parallel()
	const circular = "circular reference detected: "
	for _, c := range []struct {
		name    string
		files   map[string]string
		entries []string
		// want is the reports in each order, entries first and then reversed,
		// and resumed whether the order resumed a stall before it met the cycle.
		want    [2][]report
		resumed [2]bool
		same    bool
	}{
		{"a cycle through the source",
			map[string]string{"other.yaml": "A: {$ref: 'root.yaml#/components/responses/S'}\nB: {$ref: '#/A'}\n"},
			[]string{responseRef("S", "other.yaml#/B"), responseRef("T", "other.yaml#/A")},
			[2][]report{
				{{"/components/responses/S", circular}, {"/components/responses/T", circular}},
				{{"/components/responses/T", circular}},
			}, [2]bool{}, false},
		{"two references into a cycle the source is not part of",
			map[string]string{"other.yaml": "A: {$ref: 'third.yaml#/B'}\n", "third.yaml": "B: {$ref: 'other.yaml#/A'}\n"},
			[]string{responseRef("R1", "other.yaml#/A"), responseRef("R2", "other.yaml#/A")},
			[2][]report{
				{{"/components/responses/R1", circular}, {"/components/responses/R2", circular}},
				{{"/components/responses/R1", circular}, {"/components/responses/R2", circular}},
			}, [2]bool{}, true},
		{"a cycle the source is not part of",
			map[string]string{
				"other.yaml": "X: {description: x}\nA: {$ref: 'third.yaml#/T'}\nB: {$ref: '#/A'}\n",
				"third.yaml": "T: {$ref: 'other.yaml#/B'}\n",
			},
			[]string{responseRef("R1", "other.yaml#/X"), responseRef("R2", "other.yaml#/B")},
			[2][]report{{{"/components/responses/R2", circular}}, {{"/components/responses/R2", circular}}},
			[2]bool{}, true},
		{"a cycle closing on a hop the source's bytes were read for", nil,
			[]string{responseRef("Out", "./root.yaml#/components/responses/W"), responseRef("W", "#/components/responses/V"),
				responseRef("V", "#/components/responses/W")},
			[2][]report{
				{{"/components/responses/Out", circular}},
				{{"/components/responses/Out", circular}, {"/components/responses/V", circular}},
			}, [2]bool{}, false},
		{"a cycle through another document and back to the source",
			map[string]string{"other.yaml": "A: {$ref: '#/B'}\nB: {$ref: 'root.yaml#/components/responses/W'}\nC: {description: c}\n"},
			[]string{responseRef("E", "other.yaml#/C"), responseRef("Q", "other.yaml#/A"), responseRef("W", "#/components/responses/Q")},
			[2][]report{
				{{"/components/responses/Q", circular}},
				{{"/components/responses/W", circular}},
			}, [2]bool{}, false},
		{"a stall on the source's bytes, then a cycle", nil,
			[]string{responseRef("Out", "./root.yaml#/components/responses/W"), responseRef("W", "#/x-lib/Cyc")},
			[2][]report{
				{{"/components/responses/Out", circular}},
				{{"/components/responses/W", circular}},
			}, [2]bool{true, false}, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			dir := externalDir(t, c.files)
			reversed := slices.Clone(c.entries)
			slices.Reverse(reversed)
			var got [2][]string
			for i, entries := range [][]string{c.entries, reversed} {
				root := strings.Replace(rootOfResponses(entries...), "x-lib:\n", "x-lib:\n  Cyc: {$ref: '#/components/responses/Out'}\n", 1)
				work, diags := resumedOver(t, dir, root, maxResumeWork)
				got[i] = diagLines(diags)
				assert.Equal(t, c.resumed[i], work.spent > 0, "order %d: a stall is resumed, a cycle is not", i)
				assertReports(t, diags, slices.Clone(c.want[i]), "order %d", i)
			}
			if c.same {
				assert.Empty(t, cmp.Diff(got[0], got[1]), "each site reports the cycle alike in either order")
			}
		})
	}
}

// TestResumable pins which failures settle resumes: a pointer within a document
// walked through bytes a tree was prepared from, in the record the stalled hop
// read. Each row differs from the first in one respect.
func TestResumable(t *testing.T) {
	t.Parallel()
	data := []byte("components:\n  responses:\n    Base: {description: b}\n" +
		"    Alias: {$ref: '#/components/responses/Base'}\n    Loop: {$ref: '#/components/responses/Loop'}\n")
	tree, err := parseTree(data)
	require.NoError(t, err)
	read := newExternalReads(sourceDocument{})
	read.recordTree("other.yaml", tree, data)
	reader := newExternal(t.Context(), &soa.OpenAPI{}, Options{}, read)
	walked := jsonpointer.ErrInvalidPath.Wrap(errors.New("expected index, got key at /components"))
	cycle := errors.New("circular reference detected: other.yaml#/A -> other.yaml#/A")
	hop := func(document any) record {
		return record{path: "other.yaml", document: &document}
	}
	bytesHop := chain{records: []record{hop(slices.Clone(data))}, stopped: "#/components/responses/Loop"}
	cut := chain{records: []record{hop(slices.Clone(data))}, stopped: "#/components/responses/Base", cut: true}

	for _, c := range []struct {
		name string
		c    chain
		err  error
		want bool
	}{
		{"a stall GitHub #761 reports", bytesHop, walked, true},
		{"a cycle", chain{records: bytesHop.records, stopped: "#/components/responses/Base"}, cycle, false},
		{"a pointer walked through a tree", chain{records: []record{hop(tree)}, stopped: "#/components/responses/Base"}, walked, false},
		{"bytes no tree was prepared from",
			chain{records: []record{hop([]byte("unknown"))}, stopped: "#/components/responses/Base"}, walked, false},
		{"a resolution that reached nothing", chain{stopped: "#/components/responses/Base"}, walked, false},
		{"a chain followed only so far", cut, walked, false},
		{"a chain stopped at no reference", chain{records: bytesHop.records}, walked, false},
		{"a chain stopped at a reference naming a document",
			chain{records: bytesHop.records, stopped: "third.yaml#/Base"}, walked, false},
	} {
		got, refused := reader.resumable(c.c, c.err)
		assert.Equal(t, c.want, got, c.name)
		assert.NoError(t, refused, "%s: the budget holds", c.name)
	}
}

// TestSettle_AChainResolvesUnderEitherSpellingOfItsDocument pins GitHub #761 on
// a document read under two keys: once by its absolute path, once relative to a
// source named by a relative path. Each key prepares a tree of its own from the
// same bytes, so the digest the record is found by names two trees. Two
// references to one response under each spelling, the second of each handed
// the bytes, written with the absolute spelling first, the order that failed,
// and reversed, must each fail where the missing target is, never on the bytes.
func TestSettle_AChainResolvesUnderEitherSpellingOfItsDocument(t *testing.T) {
	t.Parallel()
	dir := externalDir(t, map[string]string{"other.yaml": "components:\n  responses:\n" +
		"    Alias: {$ref: '#/components/responses/Missing'}\n"})
	cwd, err := os.Getwd()
	require.NoError(t, err)
	source, err := filepath.Rel(cwd, filepath.Join(dir, "root.yaml"))
	require.NoError(t, err)
	absolute := responseRef("AbsA", filepath.Join(dir, "other.yaml")+"#/components/responses/Alias") +
		responseRef("AbsB", filepath.Join(dir, "other.yaml")+"#/components/responses/Alias")
	relative := responseRef("RelA", "./other.yaml#/components/responses/Alias") +
		responseRef("RelB", "./other.yaml#/components/responses/Alias")

	for _, spec := range []string{rootOfResponses(absolute, relative), rootOfResponses(relative, absolute)} {
		_, diags, err := resolveSpec(t, spec, source)
		require.NoError(t, err)
		require.Len(t, diags, 4, "%+v", diags)
		for _, d := range diags {
			assert.Contains(t, d.Message, `through "#/components/responses/Missing" in`, "%s: %s", d.Provenance.Pointer, spec)
			assert.NotContains(t, d.Message, "expected index", "%s: %s", d.Provenance.Pointer, spec)
		}
	}
}

// TestSettle_AChainResolvesInDocumentsOfTheSameBytes pins GitHub #761 on two
// documents served with the same bytes, each reached by a URL its request
// respells. Each is named twice, by a response naming a missing one, so the
// second reference of each is handed the bytes: it must fail where the target
// is missing, as the first does, and each of its records must hold the tree
// prepared under the key of its own document, not the tree the shared digest
// names. It holds in a second resolution too, which a respelled anchored
// document forces.
func TestSettle_AChainResolvesInDocumentsOfTheSameBytes(t *testing.T) {
	t.Parallel()
	const shared = "components:\n  responses:\n    Alias: {$ref: '#/components/responses/Missing'}\n"
	srv := newTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		_, err := w.Write([]byte(shared))
		assert.NoError(t, err)
	})
	trigger, _ := countingServer(t, anchoredExternalDoc)
	url := respelled(srv.URL, "HTTP")
	root := rootOfResponses(
		responseRef("A1", url+"/a.yaml#/components/responses/Alias"),
		responseRef("A2", url+"/a.yaml#/components/responses/Alias"),
		responseRef("B1", url+"/b.yaml#/components/responses/Alias"),
		responseRef("B2", url+"/b.yaml#/components/responses/Alias"))

	for _, paths := range []string{"paths: {}\n",
		"paths:\n  /x: {$ref: \"" + respelled(trigger.URL, "HTTP") + "/ext.yaml#/paths/~1x\"}\n"} {
		doc, diags, err := resolveSpec(t, strings.Replace(root, "paths: {}\n", paths, 1), "root.yaml")
		require.NoError(t, err)
		require.Len(t, diags, 4, "%s%+v", paths, diags)
		for _, d := range diags {
			assert.Contains(t, d.Message, `through "#/components/responses/Missing" in`, "%s: %s", d.Provenance.Pointer, paths)
			assert.NotContains(t, d.Message, "expected index", "%s: %s", d.Provenance.Pointer, paths)
		}
		for name, file := range map[string]string{"A2": "/a.yaml", "B2": "/b.yaml"} {
			response, ok := doc.Components.Responses.Get(name)
			require.True(t, ok)
			own, ok := doc.GetCachedExternalDocument(srv.URL + file)
			require.True(t, ok)
			records := resolutionChain(response).records
			for _, rec := range records {
				assert.Same(t, own, *rec.document, "%s's records hold its own document's tree: %s", name, paths)
			}
			assert.NotEmpty(t, records, "%s's chain read its document: %s", name, paths)
		}
	}
}
