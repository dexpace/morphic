package annotation

import (
	"maps"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
)

// TestRawChildNode_ReadsOnlyAMappingChild pins the raw reader every verbatim
// keyword goes through. Reading a key off something that is not a mapping, or
// failing to step through a document node, would report a keyword as absent that
// the source did write.
func TestRawChildNode_ReadsOnlyAMappingChild(t *testing.T) {
	t.Parallel()
	var doc yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte("a: 1\n"), &doc))

	require.Equal(t, yaml.DocumentNode, doc.Kind)
	found := RawChildNode(&doc, "a")
	require.NotNil(t, found, "a document node is stepped through to its mapping")
	assert.Equal(t, "1", found.Value)

	assert.Nil(t, RawChildNode(nil, "a"))
	assert.Nil(t, RawChildNode(&doc, "absent"))
	assert.Nil(t, RawChildNode(openapitest.YAMLNode(t, "[1, 2]"), "a"), "a sequence has no keyed children")
	assert.Nil(t, RawChildNode(openapitest.YAMLNode(t, "plain"), "a"), "nor does a scalar")
	assert.Nil(t, RawChildNode(&yaml.Node{Kind: yaml.DocumentNode}, "a"), "nor an empty document")
}

// TestRawMappingKeys_SpellsWhatRawChildNodeCanFind pins the raw key reader to
// the raw child reader beside it: every name it returns is one RawChildNode
// finds a value for, and a name RawChildNode cannot find — one merged in through
// `<<` — is not returned. The census hands each name straight to the keeper,
// which reads the value with RawChildNode, so a name spelled any other way would
// be announced as unreachable for a key the document wrote plainly.
func TestRawMappingKeys_SpellsWhatRawChildNodeCanFind(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		src  string
		want []string
	}{
		{"plain keys in written order", "b: 1\na: 2\n", []string{"b", "a"}},
		{"an anchored value is a key like any other", "a: &x {k: v}\nb: *x\n", []string{"a", "b"}},
		{"an aliased key is spelled by the scalar it stands for",
			"names: [&n spelled]\n*n : 1\n", []string{"names", "spelled"}},
		{"a repeated key is returned once", "a: 1\nb: 2\na: 3\n", []string{"a", "b"}},
		{"a merge key is not a key the mapping writes",
			"base: &b {merged: 1}\nuse:\n  <<: *b\n  own: 2\n", []string{"base", "use"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			root := openapitest.YAMLNode(t, tc.src)
			got := RawMappingKeys(root)
			assert.Equal(t, tc.want, got)
			for _, key := range got {
				assert.NotNil(t, RawChildNode(root, key), "RawChildNode finds a value for %q", key)
			}
		})
	}

	t.Run("a merge key's own pairs are read on the merging mapping", func(t *testing.T) {
		t.Parallel()
		use := RawChildNode(openapitest.YAMLNode(t, "base: &b {merged: 1}\nuse:\n  <<: *b\n  own: 2\n"), "use")
		require.NotNil(t, use)
		assert.Equal(t, []string{"own"}, RawMappingKeys(use),
			"the merged-in key is the parser's to present, not the raw mapping's — GitHub #395")
	})
}

// TestRawMappingKeys_ReadsOnlyAMapping pins the unwrap and refusal rules to
// RawChildNode's: a document node is stepped through, and anything that is not a
// mapping yields nothing rather than a partial read.
func TestRawMappingKeys_ReadsOnlyAMapping(t *testing.T) {
	t.Parallel()
	var doc yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte("a: 1\n"), &doc))
	require.Equal(t, yaml.DocumentNode, doc.Kind)
	assert.Equal(t, []string{"a"}, RawMappingKeys(&doc), "a document node is stepped through to its mapping")

	assert.Nil(t, RawMappingKeys(nil))
	assert.Nil(t, RawMappingKeys(openapitest.YAMLNode(t, "{}")), "a mapping writing no key yields none")
	assert.Nil(t, RawMappingKeys(openapitest.YAMLNode(t, "[1, 2]")), "a sequence has no keys")
	assert.Nil(t, RawMappingKeys(openapitest.YAMLNode(t, "plain")), "nor does a scalar")
	assert.Nil(t, RawMappingKeys(&yaml.Node{Kind: yaml.DocumentNode}), "nor an empty document")
}

// TestRawChildNode_IsNotTheMergeAwareView pins the difference between this
// reader and nodeview's, which is why both exist: what a keyword is preserved
// *as* is what the source spelled, while what a pointer or $ref *resolves to*
// is what the parser will see.
//
// Answering a raw read through the view would make a merged keyword appear at a
// schema that never wrote it and replace an alias by its target; the first two
// cases pin those divergences. The third pins an agreement: since GitHub #356
// both take the last pair of a repeated key, as the parser does.
func TestRawChildNode_IsNotTheMergeAwareView(t *testing.T) {
	t.Parallel()

	// use is the mapping under test in each case; the raw read of a top-level
	// key is unambiguous, so it is safe to navigate with.
	useOf := func(t *testing.T, src string) *yaml.Node {
		t.Helper()
		use := RawChildNode(openapitest.YAMLNode(t, src), "use")
		require.NotNil(t, use, "the fixture must declare a `use` mapping")
		return use
	}

	t.Run("a merge key contributes nothing to the raw read", func(t *testing.T) {
		t.Parallel()
		use := useOf(t, "base: &b {title: merged}\nuse:\n  <<: *b\n")

		assert.Nil(t, RawChildNode(use, "title"),
			"the source wrote `<<`, not `title`, so nothing is preserved at title")
		merged := nodeview.New().ChildByToken(use, "title")
		require.NotNil(t, merged, "the parser, however, does see it")
		assert.Equal(t, "merged", merged.Value)
	})

	t.Run("an aliased value is not dereferenced by the raw read", func(t *testing.T) {
		t.Parallel()
		use := useOf(t, "base: &b anchored\nuse: {title: *b}\n")

		raw := RawChildNode(use, "title")
		require.NotNil(t, raw)
		assert.Equal(t, yaml.AliasNode, raw.Kind, "the raw tree keeps the alias the source wrote")
		viewed := nodeview.New().ChildByToken(use, "title")
		require.NotNil(t, viewed, "the view resolves the key the raw tree kept aliased")
		assert.Equal(t, "anchored", viewed.Value, "where the view stands the anchor in its place")
	})

	t.Run("a repeated key resolves alike on both sides", func(t *testing.T) {
		t.Parallel()
		use := useOf(t, "use: {title: first, title: last}\n")

		raw, viewed := RawChildNode(use, "title"), nodeview.New().ChildByToken(use, "title")
		require.NotNil(t, raw, "the raw read finds the key")
		require.NotNil(t, viewed, "and so does the view")
		assert.Equal(t, "last", raw.Value,
			"the raw read takes the pair the parser reads, not the first written")
		assert.Equal(t, "last", viewed.Value,
			"and the view takes the same one, so this is no longer a divergence")
	})
}

// TestRawChildNode_FindsAKeyWrittenAsAnAlias pins the one spelling where the raw
// tree and the parsed model disagree about a key's name. yaml.v3 leaves an alias
// node's own Value as the anchor, so matching it raw looks for "k" while the
// parser has already read the pair under "aliasedKey" — and every caller here
// asks by the name the parser used. A key the census reported as undeclared then
// reached no Unmodeled entry and, before this, no diagnostic either.
func TestRawChildNode_FindsAKeyWrittenAsAnAlias(t *testing.T) {
	t.Parallel()
	var doc yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte("anchor: &k aliasedKey\n*k : found\n"), &doc))

	found := RawChildNode(&doc, "aliasedKey")

	require.NotNil(t, found, "the key is looked up by the name it resolves to")
	assert.Equal(t, "found", found.Value)
	assert.Nil(t, RawChildNode(&doc, "k"), "and not by the anchor it is written as")
}

// TestRawChildNode_RepeatedKeyReadsTheLastPair holds this reader to the pair
// the parser reads, which is the last of a repeated key.
//
// The repeat is spelled with an alias because yaml.v3 refuses a plainly
// repeated key when decoding into a typed value, as the model parse does. An
// explicit pair and an aliased one are two nodes here and one key to the
// parser.
func TestRawChildNode_RepeatedKeyReadsTheLastPair(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, body, want string }{
		{"aliased pair last", "anchor: &k dup\ndup: first\n*k : last\n", "last"},
		{"aliased pair first", "anchor: &k dup\n*k : first\ndup: last\n", "last"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var doc yaml.Node
			require.NoError(t, yaml.Unmarshal([]byte(tc.body), &doc))

			found := RawChildNode(&doc, "dup")

			require.NotNil(t, found)
			assert.Equal(t, tc.want, found.Value, "the last pair spelling the key is the effective one")
		})
	}
}

// TestRawPropertyNode_NilSchemaReadsNothing pins the nil guard on the schema
// side of the same reader, which every caller relies on to ask about a position
// that may have no body written at it.
func TestRawPropertyNode_NilSchemaReadsNothing(t *testing.T) {
	t.Parallel()
	assert.Nil(t, RawPropertyNode(nil, "type"))

	s := schemaFromYAML(t, "type: string\n")
	require.NotNil(t, RawPropertyNode(s, "type"))
	assert.Nil(t, RawPropertyNode(s, "title"), "a keyword the schema did not write")
}

// TestRawChildNodes_AgreesWithRawChildNode holds the all-keys reader to the one
// it batches, key by key: every name either reader answers for, a name merged
// in through `<<`, and one the mapping never writes. The fixtures are the
// spellings RawChildNode has a rule for: a key repeated through an alias, whose
// last pair wins; a key written as an alias, found under the name it resolves
// to; and a merge key, read as the pair it is rather than the pairs it names.
func TestRawChildNodes_AgreesWithRawChildNode(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, body string }{
		{"repeated key", "anchor: &k dup\ndup: first\n*k : last\n"},
		{"aliased key", "anchor: &k aliasedKey\n*k : found\n"},
		{"merge key", "base: &b {title: merged}\n<<: *b\nown: 1\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var doc yaml.Node
			require.NoError(t, yaml.Unmarshal([]byte(tc.body), &doc))

			children := RawChildNodes(&doc)

			require.NotEmpty(t, children, "a document node is stepped through to its mapping")
			keys := append([]string{"title", "absent"}, RawMappingKeys(&doc)...)
			keys = slices.AppendSeq(keys, maps.Keys(children))
			for _, key := range keys {
				assert.Same(t, RawChildNode(&doc, key), children[key], "the two readers disagree on %q", key)
			}
		})
	}
}

// TestRawChildNodes_ReadsOnlyAMapping is TestRawChildNode_ReadsOnlyAMappingChild
// for the reader that batches it.
func TestRawChildNodes_ReadsOnlyAMapping(t *testing.T) {
	t.Parallel()
	assert.Nil(t, RawChildNodes(nil))
	assert.Nil(t, RawChildNodes(openapitest.YAMLNode(t, "[1, 2]")), "a sequence has no keyed children")
	assert.Nil(t, RawChildNodes(openapitest.YAMLNode(t, "plain")), "nor does a scalar")
	assert.Nil(t, RawChildNodes(&yaml.Node{Kind: yaml.DocumentNode}), "nor an empty document")
}

// TestDeclaresAny_AsksTheRawNodes pins the gate's agreement with the recorders it
// gates: it must answer from the same raw nodes they read, so a keyword one sees
// and the other does not cannot arise.
func TestDeclaresAny_AsksTheRawNodes(t *testing.T) {
	t.Parallel()
	assert.True(t, DeclaresAny(schemaFromYAML(t, "$id: 'urn:a'\ntype: string\n"), DialectKeywords))
	assert.False(t, DeclaresAny(schemaFromYAML(t, "type: string\n"), DialectKeywords))
	assert.False(t, DeclaresAny(schemaFromYAML(t, "type: string\n"), nil))
}
