// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package load

import (
	"encoding/json/jsontext"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/compilers/openapi/internal/reftable"
	"github.com/dexpace/morphic/compilers/openapi/internal/reftable/typed"
)

// readAside parses text into its first document's mapping.
func readAside(t *testing.T, text string) *yaml.Node {
	t.Helper()
	var doc yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte(text), &doc))
	return doc.Content[0]
}

// marshalled is what the tree encodes to.
func marshalled(t *testing.T, n *yaml.Node) string {
	t.Helper()
	out, err := yaml.Marshal(n)
	require.NoError(t, err)
	return string(out)
}

// TestAside_RestoreLeavesTheTreeAsWritten holds the set-asides of the model
// build to what they promise: a hidden pair and a spoiled node read otherwise
// until restore, and the tree encodes as it did after it, restored newest first,
// and a second restore changes nothing.
func TestAside_RestoreLeavesTheTreeAsWritten(t *testing.T) {
	t.Parallel()
	const text = "a: {$ref: ~, get: 1}\nb: {$ref: ~}\nc: {PURGE: ~, LINK: {x: 1}}\n"
	root := readAside(t, text)
	before := marshalled(t, root)
	a := aside{}

	a.hide(root.Content[1], func(key, _ *yaml.Node) bool { return key.Value == "$ref" })
	a.hide(root.Content[5], func(_, val *yaml.Node) bool { return val.Kind != yaml.MappingNode })
	a.spoil(root.Content[3].Content[1])

	assert.Equal(t, []string{"get"}, []string{root.Content[1].Content[0].Value}, "the $ref pair is hidden")
	assert.Len(t, root.Content[1].Content, 2)
	spoiled := root.Content[3].Content[1]
	assert.Equal(t, yaml.SequenceNode, spoiled.Kind, "the null reads as an empty sequence")
	assert.Empty(t, spoiled.Content)
	assert.Len(t, root.Content[5].Content, 2, "the entry that is no mapping is hidden, LINK stays")
	assert.NotEqual(t, before, marshalled(t, root))

	a.restore()

	assert.Equal(t, before, marshalled(t, root), "what the source wrote is back")
	a.restore()
	assert.Equal(t, before, marshalled(t, root), "a second restore holds nothing")
}

// TestAside_AMappingHiddenTwiceIsRestoredToTheFirstReading holds two hides of
// one mapping to being undone in the reverse order they were made.
func TestAside_AMappingHiddenTwiceIsRestoredToTheFirstReading(t *testing.T) {
	t.Parallel()
	root := readAside(t, "m: {a: 1, b: 2, c: 3}\n")
	m := root.Content[1]
	before := marshalled(t, root)
	a := aside{}

	a.hide(m, func(key, _ *yaml.Node) bool { return key.Value == "a" })
	a.hide(m, func(key, _ *yaml.Node) bool { return key.Value == "b" })
	assert.Equal(t, "m: {c: 3}\n", marshalled(t, root))

	a.restore()

	assert.Equal(t, before, marshalled(t, root))
}

// setAsideSource writes each shape the model faults on: a Reference Object
// whose $ref is null, a Path Item whose $ref is null beside its operations, an
// additionalOperations entry that is no mapping, in the mapping and through a
// merge key, and a key that is no scalar. Nothing is anchored, and every value
// the table refuses is already null, so no node is turned for the model.
const setAsideSource = `openapi: 3.2.0
info: {title: t, version: "1"}
paths:
  /a:
    get:
      parameters:
        - $ref:
      responses: {"200": {description: ok}}
  /c: {$ref: ~, get: {responses: {"200": {description: ok}}}}
  /d:
    additionalOperations:
      PURGE: ~
      LINK: {responses: {"200": {description: ok}}}
x-k:
  ? [k]
  : 1
`

// TestLoad_TheTreeIsAsWrittenOnceTheModelIsBuilt holds the tree a loaded source
// leaves to the source as written: the nodes set aside for the model build are
// back, so every reader after it, and every verbatim copy, sees the source.
func TestLoad_TheTreeIsAsWrittenOnceTheModelIsBuilt(t *testing.T) {
	t.Parallel()
	want := marshalled(t, readAside(t, setAsideSource))

	got, diags, err := Load(t.Context(), 0, compilers.Source{Path: "spec.yaml", Data: []byte(setAsideSource)}, Options{})

	require.NoError(t, err)
	require.NotNil(t, got, "the source is read, not refused: %+v", diags)
	assert.Equal(t, want, marshalled(t, got.Doc.GetRootNode()))
}

// TestLoad_ANullRefOfAReferenceObjectIsReadWithoutAFaultAtEachModelBuild holds
// the window of the set-asides to every build of the model: a null $ref that
// only a reference reaches, one reached through a path item's $ref into another
// place of the document, and one a parameter of a callback holds, are each read
// without a fault.
func TestLoad_ANullRefOfAReferenceObjectIsReadWithoutAFaultAtEachModelBuild(t *testing.T) {
	t.Parallel()
	for name, spec := range map[string]string{
		"reached by a path item's $ref": head31L + "paths:\n  /a: {$ref: '#/x-lib/item'}\nx-lib:\n  item:\n    get:\n" +
			"      parameters:\n        - $ref:\n      responses: {\"200\": {description: ok}}\n",
		"reached by a parameter's $ref": head31L + "paths:\n  /a:\n    get:\n      parameters:\n        - $ref: '#/x-lib/p'\n" +
			"      responses: {\"200\": {description: ok}}\nx-lib:\n  p:\n    $ref:\n",
		"an alias of a null": head31L + "x-n: &n ~\npaths:\n  /a:\n    get:\n      parameters:\n        - {$ref: *n}\n" +
			"      responses: {\"200\": {description: ok}}\n",
		"merged in by a `<<`, read as left out": head31L + "x-b: &b {$ref: ~}\npaths:\n  /a:\n    get:\n" +
			"      parameters:\n        - {<<: *b}\n      responses: {\"200\": {description: ok}}\n",
		"in a callback": head31L + "paths:\n  /a:\n    post:\n      callbacks:\n        cb:\n          '{$request.body#/u}':\n" +
			"            post:\n              parameters:\n                - $ref:\n              responses: {\"200\": {description: ok}}\n" +
			"      responses: {\"200\": {description: ok}}\n",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got, diags, err := Load(t.Context(), 0, compilers.Source{Path: "spec.yaml", Data: []byte(spec)}, Options{})

			require.NoError(t, err)
			require.NotNil(t, got, "no panic reaches the load: %+v", diags)
		})
	}
}

// TestLoad_AnAdditionalOperationAMergeKeySuppliesIsReadWithoutAFault holds an
// additionalOperations entry that is no mapping to the window the entries it
// writes itself have, wherever a `<<` takes it from: a mapping written there,
// an alias of one, and one among a list of sources. Each is read without a
// fault, and the tree is as written once the model is built.
func TestLoad_AnAdditionalOperationAMergeKeySuppliesIsReadWithoutAFault(t *testing.T) {
	t.Parallel()
	const head = "openapi: 3.2.0\ninfo: {title: t, version: \"1\"}\n"
	const ok = "{responses: {\"200\": {description: ok}}}"
	for name, spec := range map[string]string{
		"a mapping":                  head + "paths:\n  /a:\n    additionalOperations:\n      <<: {PURGE: ~}\n      LINK: " + ok + "\n",
		"an alias":                   head + "x-ops: &ops {PURGE: ~}\npaths:\n  /a:\n    additionalOperations:\n      <<: *ops\n      LINK: " + ok + "\n",
		"one of a list":              head + "paths:\n  /a:\n    additionalOperations:\n      <<: [{LINK: " + ok + "}, {PURGE: ~}]\n",
		"the whole mapping an alias": head + "x-ops: &ops {PURGE: ~, LINK: " + ok + "}\npaths:\n  /a:\n    additionalOperations: *ops\n",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			want := marshalled(t, readAside(t, spec))

			got, diags, err := Load(t.Context(), 0, compilers.Source{Path: "spec.yaml", Data: []byte(spec)}, Options{})

			require.NoError(t, err)
			require.NotNil(t, got, "no panic reaches the load: %+v", diags)
			assert.Equal(t, want, marshalled(t, got.Doc.GetRootNode()))
		})
	}
}

// head31L opens the documents above.
const head31L = "openapi: 3.1.0\ninfo: {title: t, version: \"1\"}\n"

// TestAside_WhileWrittenReadsTheTreeAsWrittenThenSetsItAsideAgain holds the
// window a judgement of what references reach reads in: inside it the tree is
// as written, and after it every change is made again, but to a node the
// reading turned, which a change to the node as it was would undo: a turned
// mapping stays turned for good, and restore leaves it so.
func TestAside_WhileWrittenReadsTheTreeAsWrittenThenSetsItAsideAgain(t *testing.T) {
	t.Parallel()
	root := readAside(t, "a: {x: 1, y: 2}\nb: {$ref: ~}\nc: {p: 1, q: 2}\n")
	before := marshalled(t, root)
	a := aside{}
	dropX := func(key, _ *yaml.Node) bool { return key.Value == "x" }
	a.hide(root.Content[1], dropX)
	a.spoil(root.Content[3].Content[1])
	a.hide(root.Content[5], func(key, _ *yaml.Node) bool { return key.Value == "p" })
	aside := marshalled(t, root)

	var read string
	a.whileWritten(func() {
		read = marshalled(t, root)
		nodeview.Unread(root.Content[5])
	})

	assert.Equal(t, before, read, "the reading sees the tree as written")
	assert.Equal(t, "a: {y: 2}\nb: {$ref: []}\nc: null\n", marshalled(t, root), "set aside again, c turned")
	assert.NotEqual(t, aside, marshalled(t, root))
	a.restore()
	assert.Equal(t, "a: {x: 1, y: 2}\nb: {$ref: ~}\nc: null\n", marshalled(t, root), "the turn is for good")
}

// TestBuildObjects_AShapeTheModelFaultsOnIsAFailedBuild holds the object
// build's guard: a landing the parser faults on, built without the load
// setting it aside, is a failed build with the fault as why, never a panic.
func TestBuildObjects_AShapeTheModelFaultsOnIsAFailedBuild(t *testing.T) {
	t.Parallel()
	root := readAside(t, "openapi: 3.1.0\npaths:\n  /a:\n    get:\n      parameters: [{$ref: '#/x-p'}]\nx-p: {$ref: ~}\n")
	table := reftable.Bind(t.Context(), &yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{root}},
		reftable.Config{Self: "spec.yaml"})
	require.NotEmpty(t, table.ObjectLandings())

	failed := buildObjects(t.Context(), typed.NewBuilder(table), table, MediaTypes{}, mediaFaults{}, &Siblings{})

	require.Len(t, failed, 1)
	assert.Equal(t, jsontext.Pointer("/x-p"), failed[0].landing.At.Pointer)
	assert.ErrorContains(t, failed[0].err, "parser panicked")
}

// TestLoad_AReachedPathItemsAdditionalOperationsAreSetAside holds a 3.2 path
// item only a $ref reaches, under an extension, whose additionalOperations
// merges in an entry that is no mapping: the entry is set aside in the mapping
// the merge names, so nothing faults and the table's one error stands; a path
// item reached whose own $ref resolves nowhere holds no lists to read; and one
// whose operation repeats a parameter is reported where it is written.
func TestLoad_AReachedPathItemsAdditionalOperationsAreSetAside(t *testing.T) {
	t.Parallel()
	const spec = "openapi: 3.2.0\ninfo: {title: t, version: \"1\"}\npaths:\n  /a: {$ref: '#/x-lib/item'}\n" +
		"  /b: {$ref: '#/x-lib/via'}\n  /c: {$ref: '#/x-lib/dup'}\nx-lib:\n  item:\n    additionalOperations:\n      <<: {PURGE: ~}\n" +
		"      LINK: {responses: {\"200\": {description: ok}}}\n  via: {$ref: '#/x-lib/none'}\n" +
		"  dup:\n    get:\n      parameters: [{$ref: '#/x-lib/q'}, {$ref: '#/x-lib/q'}]\n" +
		"      responses: {\"200\": {description: ok}}\n  q: {name: q, in: query, schema: {type: string}}\n"

	doc, diags, err := Load(t.Context(), 0, openapitest.SourceOf(spec), Options{})

	require.NoError(t, err)
	require.NotNil(t, doc)
	got := make([]string, 0, len(diags))
	for _, diagnostic := range diags {
		got = append(got, diagnostic.Code+" "+string(diagnostic.Provenance.Pointer))
	}
	assert.ElementsMatch(t, []string{
		"openapi/validation/validation-type-mismatch /paths/~1a",
		"openapi/unresolved-ref /x-lib/via",
		"openapi/duplicate-parameter /x-lib/dup/get/parameters/1",
	}, got, "%+v", diags)
}

// TestMapped_PutsAURLUnderTheLongestPrefix holds the caller's mapping of a URL
// to a location: the longest prefix starting it wins, a URL location takes the
// rest as written, a directory one, relative to the source's, takes it
// segment by segment, decoded, and a segment decoding to a separator, or
// holding an escape that is none, names no location; a URL no prefix starts
// is not mapped.
func TestMapped_PutsAURLUnderTheLongestPrefix(t *testing.T) {
	t.Parallel()
	sourceDocumentValue := sourceDocument{path: "/r/spec.yaml", mappings: []Mapping{
		{Prefix: "https://e.test/x/", Location: "lib"},
		{Prefix: "https://e.test/x/y/", Location: "https://mirror.test/m/"},
		{Prefix: "https://e.test/abs/", Location: "/srv/abs"},
	}}
	for named, want := range map[string]string{
		"https://e.test/x/a%20b.yaml":  "/r/lib/a b.yaml",
		"https://e.test/x/y/c%2F.yaml": "https://mirror.test/m/c%2F.yaml",
		"https://e.test/abs/d/e.yaml":  "/srv/abs/d/e.yaml",
	} {
		got, mapped, err := sourceDocumentValue.mapped(named)
		require.NoError(t, err, named)
		assert.True(t, mapped, named)
		assert.Equal(t, want, got, named)
	}
	for named, segment := range map[string]string{
		"https://e.test/x/a%2F..%2Fb.yaml": `"a/../b.yaml"`,
		"https://e.test/x/a%zz.yaml":       `"a%zz.yaml"`,
	} {
		_, mapped, err := sourceDocumentValue.mapped(named)
		assert.True(t, mapped, named)
		assert.ErrorContains(t, err, "the segment "+segment+" holds an escaped separator")
	}
	_, mapped, err := sourceDocumentValue.mapped("https://e.test/z/b.yaml")
	assert.False(t, mapped)
	assert.NoError(t, err)
}

// TestLoad_AMappedURLNamingNoLocationIsRefusedUnread holds a reference whose
// URL a mapping puts at no location, a segment decoding to a separator, to
// the refusal naming the segment, the reader never asked; and a reference a
// source at a path no reference can be read against writes to its own
// failure; a URL the mapping puts at a file is read there.
func TestLoad_AMappedURLNamingNoLocationIsRefusedUnread(t *testing.T) {
	t.Parallel()
	const head = "openapi: 3.1.0\ninfo: {title: t, version: \"1\"}\npaths: {}\ncomponents:\n  schemas:\n"
	maps := []Mapping{{Prefix: "https://e.test/x/", Location: "lib"}}
	for path, tc := range map[string]struct{ ref, want string }{
		"/r/spec.yaml": {"https://e.test/x/a%2F..%2Fb.yaml#/B",
			`its URL maps to no location: the segment "a/../b.yaml" holds an escaped separator`},
		"/r/a%zz/spec.yaml": {"b.yaml#/B", "it names no document a reference can"},
		"/r/x/spec.yaml":    {"https://e.test/x/b.yaml#/B", ""},
	} {
		fake := &fakeReader{data: []byte("B: {type: string}\n")}

		_, diags, err := Load(t.Context(), 0, compilers.Source{Path: path, Data: []byte(head + "    P: {$ref: '" + tc.ref + "'}\n")},
			Options{Reader: fake, Mappings: maps})

		require.NoError(t, err)
		var got []string
		for _, diagnostic := range diags {
			if diagnostic.Code == "openapi/unresolved-ref" {
				got = append(got, diagnostic.Message)
			}
		}
		if tc.want == "" {
			assert.Empty(t, got, path)
			require.Len(t, fake.reqs, 1, path)
			assert.Equal(t, "/r/x/lib/b.yaml", fake.reqs[0].Location, "%s: read where the mapping puts it", path)
			continue
		}
		require.Len(t, got, 1, "%s: %+v", path, diags)
		assert.Contains(t, got[0], tc.want, path)
		assert.Empty(t, fake.reqs, "%s: the reader is never asked", path)
	}
}

// TestLoad_AReachedPathItemsAdditionalOperationIsHeldToItsRepeats holds a 3.2
// path item only a $ref reaches, under an extension, to the check of repeated
// parameters each of its operations gets where the source writes it, an
// additionalOperations entry's among them.
func TestLoad_AReachedPathItemsAdditionalOperationIsHeldToItsRepeats(t *testing.T) {
	t.Parallel()
	const spec = "openapi: 3.2.0\ninfo: {title: t, version: \"1\"}\npaths:\n  /a: {$ref: '#/x-lib/item'}\n" +
		"x-lib:\n  item:\n    additionalOperations:\n      LINK:\n" +
		"        parameters: [{name: q, in: query, schema: {type: string}}, {name: q, in: query, schema: {type: string}}]\n" +
		"        responses: {\"200\": {description: ok}}\n"

	_, diags, err := Load(t.Context(), 0, openapitest.SourceOf(spec), Options{})

	require.NoError(t, err)
	require.Len(t, diags, 1, "%+v", diags)
	assert.Equal(t, "openapi/duplicate-parameter", diags[0].Code)
	assert.Equal(t, jsontext.Pointer("/x-lib/item/additionalOperations/LINK/parameters/1"), diags[0].Provenance.Pointer)
}
