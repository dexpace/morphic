// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package load

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers"
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

// head31L opens the documents above.
const head31L = "openapi: 3.1.0\ninfo: {title: t, version: \"1\"}\n"
