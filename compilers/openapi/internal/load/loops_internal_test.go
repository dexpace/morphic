package load

import (
	"encoding/json/jsontext"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/speakeasy-api/openapi/references"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/overlay"
	"github.com/dexpace/morphic/ir"
)

// loopsSpec writes a source holding chains in the model and in raw YAML, spelled
// each way the resolver opens the source by. abs is the source's absolute path.
func loopsSpec(abs string) string {
	return `openapi: 3.1.0
info: {title: T, version: "1"}
paths: {}
components:
  schemas:
    Plain: {type: object}
    Alias: {$ref: '#/components/schemas/Plain'}
    ByAlias: {$ref: 'root.yaml#/components/schemas/Alias'}
    Loop: {$ref: '#/components/schemas/Loop'}
    ByName: {$ref: 'root.yaml#/components/schemas/ByName'}
    ByDot: {$ref: './root.yaml#/components/schemas/ByDot'}
    ByDirectory: {$ref: 'sub/../root.yaml#/components/schemas/ByDirectory'}
    ByAbsolute: {$ref: '` + abs + `#/components/schemas/ByAbsolute'}
    A: {$ref: 'root.yaml#/components/schemas/B'}
    B: {$ref: '#/components/schemas/A'}
    Tail: {$ref: '#/components/schemas/ByName'}
    Elsewhere: {$ref: 'other.yaml#/components/schemas/E'}
    ThroughElsewhere: {$ref: '#/components/schemas/Elsewhere'}
    Anchored: {$anchor: a, $ref: '#a'}
    Defined: {$ref: '#/$defs/D'}
x-lib:
  first: {$ref: './root.yaml#/x-lib/second'}
  second: {$ref: '#/x-lib/first'}
`
}

// TestLoops_ReadsAChainAsTheResolverDoes pins which schema references loops
// finds never to end (GitHub #768): those whose chain, read as the resolver
// reads it, closes on a hop it already took. The resolver opens the source by
// any spelling of its file, which the lowering reads only as the bare name, and
// reads the source's tree from the first such hop on. Chains it cannot read as
// the resolver does, and those that end, are no answer.
func TestLoops_ReadsAChainAsTheResolverDoes(t *testing.T) {
	t.Parallel()
	abs, err := filepath.Abs("root.yaml")
	require.NoError(t, err)
	spec := loopsSpec(abs)
	root, _, err := decodeStream([]byte(spec))
	require.NoError(t, err)
	doc, _, err := unmarshal(t.Context(), []byte(spec), root)
	require.NoError(t, err)
	self := sourceDocument{path: "root.yaml", root: root}

	for _, c := range []struct {
		name, ref string
		want      bool
	}{
		{"a schema that ends", "#/components/schemas/Plain", false},
		{"an alias of one", "#/components/schemas/Alias", false},
		{"an alias by the file's name", "root.yaml#/components/schemas/ByAlias", false},
		{"a loop in the model", "#/components/schemas/Loop", true},
		{"a loop through the file's name", "root.yaml#/components/schemas/ByName", true},
		{"a loop through ./", "./root.yaml#/components/schemas/ByDot", true},
		{"a loop through a directory", "sub/../root.yaml#/components/schemas/ByDirectory", true},
		{"a loop through the absolute path", abs + "#/components/schemas/ByAbsolute", true},
		{"a loop that crosses from the model into the tree", "root.yaml#/components/schemas/A", true},
		{"a chain into a loop", "#/components/schemas/Tail", true},
		{"a loop of raw nodes", "./root.yaml#/x-lib/first", true},
		{"a hop into another file", "#/components/schemas/Elsewhere", false},
		{"a chain through another file", "#/components/schemas/ThroughElsewhere", false},
		{"an anchor", "#/components/schemas/Anchored", false},
		{"a definition", "#/components/schemas/Defined", false},
		{"a position nothing is at", "#/components/schemas/Missing", false},
	} {
		assert.Equal(t, c.want, newLoops(self, doc).into(references.Reference(c.ref)), c.name)
	}

	t.Run("a source with no tree or no model", func(t *testing.T) {
		t.Parallel()
		assert.False(t, newLoops(sourceDocument{path: "root.yaml"}, doc).into("#/components/schemas/Loop"))
		assert.False(t, newLoops(self, nil).into("#/components/schemas/Loop"))
	})

	t.Run("a source whose $self rebases its references", func(t *testing.T) {
		t.Parallel()
		rebased := strings.Replace(spec, "openapi: 3.1.0\n", "openapi: 3.2.0\n$self: https://example.com/dir/root.yaml\n", 1)
		rebasedRoot, _, err := decodeStream([]byte(rebased))
		require.NoError(t, err)
		rebasedDoc, _, err := unmarshal(t.Context(), []byte(rebased), rebasedRoot)
		require.NoError(t, err)
		l := newLoops(sourceDocument{path: "root.yaml", root: rebasedRoot}, rebasedDoc)

		assert.False(t, l.into("root.yaml#/components/schemas/ByName"), "root.yaml is a file beside the $self URL, not the source")
		assert.True(t, l.into("#/components/schemas/Loop"), "a pointer within the document is the source's")
	})
}

// TestLoops_ReadsEachHopOnce pins that loops settles a hop once, so the
// references of a chain cost the chain once between them, and that it stops at
// maxLoopReads without recording what it did not settle: a chain that is cut is
// no answer, and a second ask must not read it as one.
func TestLoops_ReadsEachHopOnce(t *testing.T) {
	t.Parallel()
	const n = 200
	var schemas strings.Builder
	for i := range n {
		fmt.Fprintf(&schemas, "    S%d: {$ref: '#/components/schemas/S%d'}\n", i, i+1)
	}
	fmt.Fprintf(&schemas, "    S%d: {$ref: '#/components/schemas/S%d'}\n", n, n-1)
	spec := rootOfSchemas(schemas.String())
	root, _, err := decodeStream([]byte(spec))
	require.NoError(t, err)
	doc, _, err := unmarshal(t.Context(), []byte(spec), root)
	require.NoError(t, err)
	self := sourceDocument{path: "root.yaml", root: root}

	t.Run("a long chain into a loop", func(t *testing.T) {
		t.Parallel()
		l := newLoops(self, doc)
		require.True(t, l.into("#/components/schemas/S0"), "the chain ends in a two-schema loop")
		reads := l.reads
		assert.Equal(t, n+1, reads, "each hop is read once")

		for i := range n {
			assert.True(t, l.into(references.Reference(fmt.Sprintf("#/components/schemas/S%d", i))))
		}
		assert.Equal(t, reads, l.reads, "every other reference of the chain reads nothing more")
	})

	t.Run("a bound that cuts a chain", func(t *testing.T) {
		t.Parallel()
		l := newLoops(self, doc)
		l.reads = maxLoopReads - 3

		assert.False(t, l.into("#/components/schemas/S0"), "a chain cut short is no answer")
		assert.Empty(t, l.known, "and nothing it read is recorded as settled")

		l.reads = 0
		assert.True(t, l.into("#/components/schemas/S0"), "asked again within the bound, it is read in full")
	})
}

// TestNewResolution_ReadsChainsWhereExternalReferencesAreRead pins when the
// pass reads chains for a loop: with external references on, where a reference
// can reach the source by its file name, and not otherwise.
func TestNewResolution_ReadsChainsWhereExternalReferencesAreRead(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "root.yaml")
	for _, allow := range []bool{true, false} {
		spec := rootOfSchemas("    A: {type: object}\n")
		root, _, err := decodeStream([]byte(spec))
		require.NoError(t, err)
		doc, _, err := unmarshal(t.Context(), []byte(spec), root)
		require.NoError(t, err)
		self := newSourceDocument(path, []byte(spec), root, nil)
		opts := Options{AllowExternalRefs: allow}
		var reader *external
		if allow {
			r := newExternal(doc, opts, newExternalReads(self))
			reader = &r
		}

		got := newResolution(t.Context(), pointerAt(0, overlay.Origin{}), doc, self, opts, reader)

		assert.Equal(t, allow, got.loops != nil, "external references %t", allow)
	}
}

// TestLoops_IncompleteReportsTheBoundTheReadsStoppedAt pins that a read cut by
// maxLoopReads is reported, once, at the document, as a warning that the
// protection against a cycle is incomplete, as the cycle scans report theirs,
// and that a read within the bound, and no reader, report nothing.
func TestLoops_IncompleteReportsTheBoundTheReadsStoppedAt(t *testing.T) {
	t.Parallel()
	at := pointerAt(0, overlay.Origin{})
	l := newLoops(sourceDocument{}, nil)

	assert.Empty(t, l.incomplete(at))
	l.reads = maxLoopReads
	assert.Empty(t, l.incomplete(at), "reads up to the bound are all read")
	l.reads = maxLoopReads + 1
	diags := l.incomplete(at)
	require.Len(t, diags, 1)
	assert.Equal(t, diag.CycleScanFailed, diags[0].Code)
	assert.Equal(t, ir.SeverityWarning, diags[0].Severity)
	assert.Equal(t, jsontext.Pointer(""), diags[0].Provenance.Pointer)
	assert.Contains(t, diags[0].Message, "protection is incomplete")
	assert.Empty(t, (*loops)(nil).incomplete(at), "no reader, no report")

	t.Run("surfaced by the pass", func(t *testing.T) {
		t.Parallel()
		doc, pass := heldResolution(t, rootOfSchemas("    A: {type: object}\n"), filepath.Join(t.TempDir(), "root.yaml"))
		pass.loops.reads = maxLoopReads + 1

		_, diags := pass.run(doc)

		assert.Equal(t, []string{" " + diag.CycleScanFailed}, diagLines(diags))
	})
}

// TestNewSourceDocument_ASourceThatSpellsIDIsNotHeld pins when the source is
// held under its file name: not where a mapping of it has a $id key, which
// rebases the references under it in a way loops does not read. A key an anchor
// supplies counts, and the word as a value, in a list or in a key's text does
// not.
func TestNewSourceDocument_ASourceThatSpellsIDIsNotHeld(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name, extra string
		holdless    bool
	}{
		{"nothing of the kind", "", false},
		{"a $id at the top", "$id: 'http://example.com/root'\n", true},
		{"a $id in a schema", "x-s: {type: object, $id: 'http://example.com/a'}\n", true},
		{"a $id in an anchored mapping", "x-a: &a {$id: 'http://example.com/a'}\nx-b: *a\n", true},
		{"a $id merged in", "x-a: &a {$id: 'http://example.com/a'}\nx-b: {<<: *a}\n", true},
		{"the word as a value", "x-d: {description: '$id'}\n", false},
		{"the word in a list", "x-l: ['$id', b]\n", false},
		{"a key that only starts with it", "x-i: {$idx: 1, '$id ': 2}\n", false},
	} {
		spec := rootOfSchemas("    A: {type: object}\n") + c.extra
		root, _, err := decodeStream([]byte(spec))
		require.NoError(t, err)

		self := newSourceDocument("root.yaml", []byte(spec), root, nil)

		assert.Equal(t, c.holdless, self.holdless, c.name)
		assert.Equal(t, !c.holdless, len(self.keys()) > 0, "%s: held under its keys unless it spells $id", c.name)
		assert.Equal(t, !c.holdless, self.names("root.yaml"), "%s: opened as the source unless it spells $id", c.name)
	}
}

// TestLoops_ACycleThroughTheSourcesFileNameIsRefusedNotFollowed pins GitHub
// #768 end to end: with external references on, a schema $ref cycle closing
// through the source's file, however it is spelled, is reported as a cycle at
// each reference on it, where the resolver followed it until the stack ran
// out. A source that spells $id is not held, so its file is read as on main and
// nothing there is followed that was not. Each document is loaded in a child,
// in the order written and reversed: the report is the same in both, and an
// overflow fails this test, not the binary.
func TestLoops_ACycleThroughTheSourcesFileNameIsRefusedNotFollowed(t *testing.T) {
	if dir := os.Getenv(settleChildEnv); dir != "" {
		printLoad(t, dir)
		return
	}
	t.Parallel()
	abs := func(dir string) string { return filepath.Join(dir, "root.yaml") }
	for _, c := range []struct {
		name    string
		entries func(dir string) []string
		want    []string
		// extra is written after the entries.
		extra string
	}{
		{"through the file's name", func(string) []string {
			return []string{"    A: {$ref: 'root.yaml#/components/schemas/D'}\n", "    D: {$ref: './root.yaml#/x-lib/D'}\n",
				"    C: {$ref: './root.yaml#/components/schemas/D'}\n"}
		}, []string{"/components/schemas/A", "/components/schemas/C", "/components/schemas/D"}, ""},
		{"through the absolute path", func(dir string) []string {
			return []string{"    A: {$ref: '" + abs(dir) + "#/components/schemas/E'}\n", "    E: {$ref: '#/components/schemas/A'}\n"}
		}, []string{"/components/schemas/A", "/components/schemas/E"}, ""},
		{"a reference into the cycle", func(dir string) []string {
			return []string{"    A: {$ref: '" + abs(dir) + "#/components/schemas/A'}\n", "    In: {$ref: '#/components/schemas/A'}\n"}
		}, []string{"/components/schemas/A", "/components/schemas/In"}, ""},
		{"in a source that spells $id, which is not held", func(dir string) []string {
			return []string{"    A: {$ref: '" + abs(dir) + "#/components/schemas/E'}\n", "    E: {$ref: '#/components/schemas/A'}\n"}
		}, nil, "x-id: {$id: 'http://example.com/a'}\n"},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			entries := c.entries(dir)
			reversed := slices.Clone(entries)
			slices.Reverse(reversed)
			for _, order := range [][]string{entries, reversed} {
				writeFile(t, dir, "root.yaml", rootOfSchemas(order...)+"x-lib:\n  D: {$ref: './root.yaml#/components/schemas/A'}\n"+c.extra)

				var cyclic []string
				for _, line := range loadInChild(t, "TestLoops_ACycleThroughTheSourcesFileNameIsRefusedNotFollowed", dir) {
					if site, ok := strings.CutSuffix(strings.SplitN(line, " ", 3)[0]+" "+strings.SplitN(line, " ", 3)[1], " openapi/cyclic-ref"); ok {
						cyclic = append(cyclic, site)
					}
				}
				slices.Sort(cyclic)
				assert.Equal(t, c.want, cyclic, "each reference on the cycle is reported as one")
			}
		})
	}
}

// TestHold_ASourceThatSpellsIDIsNotHeld pins that hold stores nothing for a
// source that spells $id, as for one with no path: the resolver reads the file
// for each reference naming it, as it does where nothing is held.
func TestHold_ASourceThatSpellsIDIsNotHeld(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		extra string
		held  bool
	}{{"", true}, {"x-id: {$id: 'http://example.com/a'}\n", false}} {
		spec := rootOfSchemas("    A: {type: object}\n") + c.extra
		root, _, err := decodeStream([]byte(spec))
		require.NoError(t, err)
		doc, _, err := unmarshal(t.Context(), []byte(spec), root)
		require.NoError(t, err)
		reader := newExternal(doc, Options{}, newExternalReads(newSourceDocument("root.yaml", []byte(spec), root, nil)))

		reader.hold(t.Context())

		_, held := doc.GetCachedExternalDocument("root.yaml")
		assert.Equal(t, c.held, held, "spelling $id: %t", !c.held)
	}
}
