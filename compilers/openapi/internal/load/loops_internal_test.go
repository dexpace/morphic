package load

import (
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/speakeasy-api/openapi/jsonpointer"
	"github.com/speakeasy-api/openapi/references"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "gopkg.in/yaml.v3"

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

		assert.True(t, l.into("root.yaml#/components/schemas/ByName"),
			"a schema $ref resolves against the source's path, which $self does not rebase")
		assert.True(t, l.into("#/components/schemas/Loop"), "a pointer within the document is the source's")
	})

	t.Run("a source whose name the resolver reads as a URL", func(t *testing.T) {
		t.Parallel()
		named := strings.ReplaceAll(spec, "'root.yaml#/components/schemas/ByName'", "'x:root.yaml#/components/schemas/ByName'")
		namedRoot, _, err := decodeStream([]byte(named))
		require.NoError(t, err)
		namedDoc, _, err := unmarshal(t.Context(), []byte(named), namedRoot)
		require.NoError(t, err)
		l := newLoops(sourceDocument{path: "x:root.yaml", root: namedRoot}, namedDoc)

		assert.True(t, l.into("x:root.yaml#/components/schemas/ByName"),
			"the source is held under its own spelling, which the resolver reads it by")
		assert.False(t, l.into("y:root.yaml#/components/schemas/ByName"), "another URL is another document")
	})
}

// TestLoops_ReadsEachHopOnce pins that loops settles a hop once, so the
// references of a chain cost the chain once between them, and that it stops at
// maxLoopWork without recording what it did not settle: a chain that is cut is
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
		l.work = maxLoopWork - 10

		assert.False(t, l.into("#/components/schemas/S0"), "a chain cut short is no answer")
		assert.Empty(t, l.known, "and nothing it read is recorded as settled")

		l.work = 0
		assert.True(t, l.into("#/components/schemas/S0"), "asked again within the bound, it is read in full")
	})
}

// TestLoops_AReadIsPricedTheKeysItScans pins GitHub #775: a hop read in the
// source's tree is charged the keys the library compares there, so n $refs
// naming entries of one mapping by the source's file name take about n squared
// steps, where the same $refs into a tree of four-key mappings take about n.
func TestLoops_AReadIsPricedTheKeysItScans(t *testing.T) {
	t.Parallel()
	// work returns what loops spends reading n such $refs in the layout named.
	work := func(n int, layout string) int {
		var lib strings.Builder
		refs := make([]string, n)
		for i := range n {
			refs[i] = fmt.Sprintf("root.yaml#/d/D%d", i)
			if layout == "tree" {
				refs[i] = fmt.Sprintf("root.yaml#/d/%d/%d/%d/%d", i/64, i/16%4, i/4%4, i%4)
				continue
			}
			fmt.Fprintf(&lib, "  D%d: {type: object}\n", i)
		}
		spec := rootOfSchemas("    A: {type: object}\n") + "d:\n" + lib.String()
		if layout == "tree" {
			spec = rootOfSchemas("    A: {type: object}\n") + schemaTree(n)
		}
		root, _, err := decodeStream([]byte(spec))
		require.NoError(t, err)
		doc, _, err := unmarshal(t.Context(), []byte(spec), root)
		require.NoError(t, err)
		l := newLoops(sourceDocument{path: "root.yaml", root: root}, doc)
		for _, ref := range refs {
			require.False(t, l.into(references.Reference(ref)), ref)
		}
		require.Equal(t, n, l.reads, "%s: each $ref is read, in the tree", layout)
		return l.work
	}
	const n = 64
	assert.Greater(t, work(2*n, "wide"), 3*work(n, "wide"), "one mapping holding them all")
	assert.LessOrEqual(t, 4*work(2*n, "tree"), 9*work(n, "tree"), "mappings four keys wide")
}

// TestLoops_Priced pins what a hop's read is charged: a step and the library's
// read of the tree for a hop read there, and the model's too where the tree
// holds no node, as readAt then reads it; and the keys indexed to count it.
func TestLoops_Priced(t *testing.T) {
	t.Parallel()
	spec := rootOfSchemas("    A: {type: object}\n") + "x-lib: {a: {type: object}, b: {type: object}}\n"
	root, _, err := decodeStream([]byte(spec))
	require.NoError(t, err)
	doc, _, err := unmarshal(t.Context(), []byte(spec), root)
	require.NoError(t, err)
	tree := func(pointer string) int {
		steps, _ := newTreeReads().cost(root, pointer, maxLoopWork)
		return steps
	}
	model := func(pointer string) int {
		return newTreeReads().modelCost(doc, jsontext.Pointer(pointer), maxLoopWork)
	}
	l := newLoops(sourceDocument{path: "root.yaml", root: root}, doc)
	l.priced("/components/schemas/A", true)
	l.priced("/x-lib/b", false) // indexes every mapping the reads below meet
	for _, c := range []struct {
		pointer string
		inTree  bool
		want    int
	}{
		{"/x-lib/b", true, 1 + tree("/x-lib/b")},
		{"/x-lib/missing", true, 1 + tree("/x-lib/missing") + model("/x-lib/missing")},
		{"/x-lib/b", false, 1 + model("/x-lib/b")},
		{"/components/schemas/A", false, 1 + model("/components/schemas/A")},
	} {
		assert.Equal(t, c.want, l.priced(c.pointer, c.inTree), "%s in the tree %t", c.pointer, c.inTree)
	}
	fresh := newLoops(sourceDocument{path: "root.yaml", root: root}, doc)
	indexed := newTreeReads()
	indexed.cost(root, "/x-lib/b", maxLoopWork)
	assert.Equal(t, 1+tree("/x-lib/b")+indexed.drain(), fresh.priced("/x-lib/b", true),
		"the first read of a mapping indexes it, too")
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
// maxLoopWork is reported, once, at the document, as a warning that the
// protection against a cycle is incomplete, as the cycle scans report theirs,
// and that a read within the bound, and no reader, report nothing.
func TestLoops_IncompleteReportsTheBoundTheReadsStoppedAt(t *testing.T) {
	t.Parallel()
	at := pointerAt(0, overlay.Origin{})
	l := newLoops(sourceDocument{}, nil)

	assert.Empty(t, l.incomplete(at))
	l.work = maxLoopWork
	assert.Empty(t, l.incomplete(at), "reads up to the bound are all read")
	l.work = maxLoopWork + 1
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
		pass.loops.work = maxLoopWork + 1

		_, diags := pass.run(doc)

		assert.Equal(t, []string{" " + diag.CycleScanFailed}, diagLines(diags))
	})
}

// TestNewSourceDocument_ASourceThatSpellsIDIsNotHeld pins when the source is
// held under its file name: not where it has a scalar spelling $id, which
// rebases the references under it in a way loops does not read. The match is
// wider than a key, since reading the tree as the parser does is not certain
// and a source held less only reads as it did where nothing was held: the word
// as a value, in a list, and as a key an alias supplies all count, and text
// that merely contains it does not.
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
		{"a $id key an alias supplies", "x-k: &k '$id'\nx-h: {*k : 'http://example.com/a'}\n", true},
		{"the word as a value", "x-d: {description: '$id'}\n", true},
		{"the word in a list", "x-l: ['$id', b]\n", true},
		{"a key that only starts with it", "x-i: {$idx: 1, '$id ': 2}\n", false},
		{"text that contains it", "x-t: {description: 'the $id of a schema'}\n", false},
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

// TestNewSourceDocument_ASourceWritingMoreRefsThanTheGuardReadsIsNotHeld pins
// the size past which the source is not held: more $ref scalars than
// maxHeldRefs, which loops could run out of reads on. Held, a source that size
// let the resolver follow a cycle through its file name once the guard had no
// answer left.
func TestNewSourceDocument_ASourceWritingMoreRefsThanTheGuardReadsIsNotHeld(t *testing.T) {
	t.Parallel()
	for _, n := range []int{maxHeldRefs, maxHeldRefs + 1} {
		root := &yaml.Node{Kind: yaml.MappingNode}
		for range n {
			root.Content = append(root.Content, &yaml.Node{Kind: yaml.ScalarNode, Value: "$ref"},
				&yaml.Node{Kind: yaml.ScalarNode, Value: "#/x"})
		}

		self := newSourceDocument("root.yaml", nil, root, nil)

		assert.Equal(t, n > maxHeldRefs, self.holdless, "%d $refs", n)
	}
}

// TestVisit_AChainTheGuardCouldNotReadIsLeftUnresolved pins what visit does
// once loops has spent its reads: in a held source, a schema $ref it could not
// read is not resolved, since its chain could close through the source's file
// name, which the resolver would follow until the stack ran out. Read, or in a
// source that is not held, it is resolved as ever.
func TestVisit_AChainTheGuardCouldNotReadIsLeftUnresolved(t *testing.T) {
	t.Parallel()
	spec := rootOfSchemas("    A: {$ref: '#/components/schemas/B'}\n    B: {type: object}\n")
	for _, c := range []struct {
		name     string
		spent    bool
		extra    string
		resolved bool
	}{
		{"within the bound", false, "", true},
		{"past the bound, in a held source", true, "", false},
		{"past the bound, in a source not held", true, "x-id: {$id: 'http://example.com/a'}\n", true},
	} {
		doc, pass := heldResolution(t, spec+c.extra, filepath.Join(t.TempDir(), "root.yaml"))
		if c.spent {
			pass.loops.work = maxLoopWork
		}
		a, ok := doc.GetComponents().GetSchemas().Get("A")
		require.True(t, ok)

		pass.visit("/components/schemas/A", a, a.GetRef())

		assert.Equal(t, c.resolved, a.IsResolved(), c.name)
		assert.Empty(t, pass.failures, c.name)
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
		{"through a definition two held references name", func(string) []string {
			return []string{"    H:\n      $defs:\n        X: {$ref: 'root.yaml#/components/schemas/G'}\n" +
				"      properties:\n        p: {$ref: '#/$defs/X'}\n        q: {$ref: '#/$defs/X'}\n",
				"    G: {$ref: '#/components/schemas/H/$defs/X'}\n"}
		}, []string{"/components/schemas/G", "/components/schemas/H/$defs/X",
			"/components/schemas/H/properties/p", "/components/schemas/H/properties/q"}, ""},
		{"through a held definition a walked reference read first", func(string) []string {
			return []string{"    H:\n      $defs:\n        X: {$ref: 'root.yaml#/components/schemas/G'}\n        Y: {$ref: '#/$defs/X'}\n" +
				"      properties:\n        p: {$ref: '#/$defs/Y'}\n        q: {$ref: '#/$defs/Y'}\n",
				"    G: {$ref: '#/components/schemas/H/$defs/X'}\n", "    K: {$ref: '#/components/schemas/H/$defs/Y'}\n"}
		}, []string{"/components/schemas/G", "/components/schemas/H/$defs/X", "/components/schemas/H/$defs/Y",
			"/components/schemas/H/properties/p", "/components/schemas/H/properties/q"}, ""},
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

// TestLoops_AHopPastAReferenceNotYetResolvedIsReadAgain pins a schema chain
// whose pointer passes a reference the walk resolves only after it has read the
// chain once: there, the model reads no target. Settled as ending, the hop kept
// answering so once the reference resolved, the guard let the cycle through,
// and the resolver followed it until the stack ran out. Each document is loaded
// in a child, in both orders of the responses, by '#' and by the file's name.
func TestLoops_AHopPastAReferenceNotYetResolvedIsReadAgain(t *testing.T) {
	if dir := os.Getenv(settleChildEnv); dir != "" {
		printLoad(t, dir)
		return
	}
	t.Parallel()
	for _, spelled := range []string{"#", "root.yaml#"} {
		t.Run(spelled, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			responses := []string{"    R: {$ref: '" + spelled + "/components/responses/R2'}\n",
				"    R2:\n      description: ok\n      content: {application/json: {schema: {$ref: '#/components/schemas/A'}}}\n"}
			for _, order := range [][]string{responses, {responses[1], responses[0]}} {
				writeFile(t, dir, "root.yaml", rootOfSchemas(
					"    A: {$ref: '#/components/responses/R/content/application~1json/schema'}\n")+
					"  responses:\n"+strings.Join(order, ""))
				lines := loadInChild(t, "TestLoops_AHopPastAReferenceNotYetResolvedIsReadAgain", dir)
				assert.NotEmpty(t, lines, "the cycle is reported, and the load ends")
			}
		})
	}
}

// TestLoops_ATreeHopThroughAReferenceReadsTheModel pins a chain that names the
// source by its file name, then a pointer through a path item's $ref, where
// the tree holds no node. The resolver answers that pointer from its cache, with
// the model's object past the reference, so the chain closes there; read in the
// tree alone it seemed to end, and the resolver followed it until the stack ran
// out. Each document is loaded in a child, in both orders of the paths.
func TestLoops_ATreeHopThroughAReferenceReadsTheModel(t *testing.T) {
	if dir := os.Getenv(settleChildEnv); dir != "" {
		printLoad(t, dir)
		return
	}
	t.Parallel()
	for _, spelled := range []string{"root.yaml#", "./root.yaml#"} {
		t.Run(spelled, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			paths := []string{"  /c: {$ref: 'root.yaml#/paths/~1d'}\n", "  /d: {get: {responses: {'200': {description: ok, " +
				"content: {application/json: {schema: {$ref: '" + spelled + "/components/schemas/A'}}}}}}}\n"}
			for _, order := range [][]string{paths, {paths[1], paths[0]}} {
				writeFile(t, dir, "root.yaml", "openapi: 3.1.0\ninfo: {title: T, version: '1'}\npaths:\n"+strings.Join(order, "")+
					"components:\n  schemas:\n    A: {$ref: '#/paths/~1c/get/responses/200/content/application~1json/schema'}\n")
				var cyclic []string
				for _, line := range loadInChild(t, "TestLoops_ATreeHopThroughAReferenceReadsTheModel", dir) {
					if site, rest, _ := strings.Cut(line, " "); strings.HasPrefix(rest, diag.CyclicRef+" ") {
						cyclic = append(cyclic, site)
					}
				}
				assert.Contains(t, cyclic, "/components/schemas/A", "the chain is refused as the cycle it is")
			}
		})
	}
}

// TestLoops_APendingHopIsReadAgainAlone pins what a chain ending in a pending
// read costs when asked again: the hops it followed are taken unread, and only
// the pending one is read, so a chain shared by many references is not read
// once per reference until the walk resolves what it passes.
func TestLoops_APendingHopIsReadAgainAlone(t *testing.T) {
	t.Parallel()
	const n = 20
	var schemas strings.Builder
	for i := range n {
		fmt.Fprintf(&schemas, "    S%d: {$ref: '#/components/schemas/S%d'}\n", i, i+1)
	}
	fmt.Fprintf(&schemas, "    S%d: {$ref: '#/components/responses/R/content/application~1json/schema'}\n", n)
	spec := rootOfSchemas(schemas.String()) + "  responses:\n    R: {$ref: '#/components/responses/R2'}\n" +
		"    R2: {description: ok, content: {application/json: {schema: {type: object}}}}\n"
	root, _, err := decodeStream([]byte(spec))
	require.NoError(t, err)
	doc, _, err := unmarshal(t.Context(), []byte(spec), root)
	require.NoError(t, err)
	l := newLoops(sourceDocument{path: "root.yaml", root: root}, doc)

	require.False(t, l.into("#/components/schemas/S0"), "R is not resolved, so the chain reads no further")
	first := l.reads
	require.Equal(t, n+2, first, "each hop is read, the pending one last")
	assert.Empty(t, l.known, "a pending chain is not settled")

	assert.False(t, l.into("#/components/schemas/S0"))
	assert.Equal(t, first+1, l.reads, "asked again, only the pending hop is read")
}

// TestSettledRead pins which failed reads settle a hop as ending: a pointer
// that names nothing, or one the document cannot be read through, does; one
// through a reference the walk has not resolved yet reads on once it is.
func TestSettledRead(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name string
		err  error
		want hopKind
	}{
		{"no target", jsonpointer.ErrNotFound, hopEnds},
		{"through a scalar", jsonpointer.ErrInvalidPath, hopEnds},
		{"a malformed pointer", jsonpointer.ErrValidation, hopEnds},
		{"through a reference not yet resolved", errors.New("unresolved reference"), hopPending},
	} {
		assert.Equal(t, c.want, settledRead(c.err), c.name)
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
