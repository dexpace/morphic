// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package nodeview_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
)

// readMapping parses text and returns its root mapping.
func readMapping(t *testing.T, text string) *yaml.Node {
	t.Helper()
	var doc yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte(text), &doc))
	require.Equal(t, yaml.DocumentNode, doc.Kind)
	return doc.Content[0]
}

// TestUnread_ReadsAsNullAndWrittenReturnsWhatTheSourceWrote holds a node turned
// by Unread to every reading a null has, and Written to the node as it was.
func TestUnread_ReadsAsNullAndWrittenReturnsWhatTheSourceWrote(t *testing.T) {
	t.Parallel()
	root := readMapping(t, "a: {x: [1, hello], y: !local z}\nb: !local t\n")
	for _, key := range []string{"a", "b"} {
		t.Run(key, func(t *testing.T) {
			n := nodeview.Child(root, key)
			require.NotNil(t, n)
			was := *n
			wasContent := append([]*yaml.Node(nil), n.Content...)

			nodeview.Unread(n)

			assert.Equal(t, yaml.ScalarNode, n.Kind)
			assert.Equal(t, "!!null", n.ShortTag(), "every reader takes it for a null")
			written := nodeview.Written(n)
			require.NotNil(t, written)
			assert.Equal(t, was.Kind, written.Kind, "kind as written")
			assert.Equal(t, was.Tag, written.Tag, "tag as written")
			assert.Equal(t, was.Value, written.Value, "text as written")
			assert.Equal(t, wasContent, written.Content, "content as written")
			assert.Equal(t, was.Line, written.Line)
			assert.Equal(t, was.Column, written.Column)
		})
	}
}

// TestWritten_ANodeNobodyUnreadIsItself holds Written to its argument for a node
// that was left as written.
func TestWritten_ANodeNobodyUnreadIsItself(t *testing.T) {
	t.Parallel()
	root := readMapping(t, "a: hello\n")
	n := nodeview.Child(root, "a")

	assert.Same(t, n, nodeview.Written(n))
	assert.Nil(t, nodeview.Written(nil))
}

// TestUnread_TheDecoderAndEncoderReadNull holds the tree after Unread to what
// the library does with a null: it encodes one, and decodes into a struct and a
// map as nil.
func TestUnread_TheDecoderAndEncoderReadNull(t *testing.T) {
	t.Parallel()
	root := readMapping(t, "a: {x: [1, hello]}\nb: keep\n")
	nodeview.Unread(nodeview.Child(root, "a"))

	out, err := yaml.Marshal(root)
	require.NoError(t, err)
	assert.Equal(t, "a: null\nb: keep\n", string(out))

	var asMap map[string]any
	require.NoError(t, root.Decode(&asMap))
	assert.Equal(t, map[string]any{"a": nil, "b": "keep"}, asMap)

	var asStruct struct {
		A *map[string]any `yaml:"a"`
		B string          `yaml:"b"`
	}
	require.NoError(t, root.Decode(&asStruct))
	assert.Nil(t, asStruct.A)
	assert.Equal(t, "keep", asStruct.B)
}

// TestUnread_AWalkVisitsTheNullAndNothingBehindIt holds the compiler's walk,
// aliases followed or not, and Deref, to the null: nothing the node held is
// visited, as for a null written there.
func TestUnread_AWalkVisitsTheNullAndNothingBehindIt(t *testing.T) {
	t.Parallel()
	turned := readMapping(t, "a: {x: [1, hello]}\nb: keep\n")
	n := nodeview.Child(turned, "a")
	nodeview.Unread(n)
	plain := readMapping(t, "a: ~\nb: keep\n")

	for _, mode := range []nodeview.Aliases{nodeview.AliasLeaf, nodeview.AliasTarget} {
		var seen, want []string
		collect := func(into *[]string) func(*yaml.Node, int) nodeview.Step {
			return func(n *yaml.Node, _ int) nodeview.Step {
				*into = append(*into, n.Value)
				return nodeview.Descend
			}
		}
		got := nodeview.Walk{Nodes: 100, Aliases: mode, Visit: collect(&seen)}.Run(t.Context(), turned)
		ref := nodeview.Walk{Nodes: 100, Aliases: mode, Visit: collect(&want)}.Run(t.Context(), plain)

		assert.Equal(t, ref.Visited, got.Visited)
		assert.Len(t, seen, len(want))
		assert.NotContains(t, seen, "hello")
		assert.NotContains(t, seen, "x")
	}
	assert.Same(t, n, nodeview.Deref(n), "an Alias field on a scalar is no alias")
}

// TestAliasField_OnlyNodeviewReadsItOnAScalar holds every read of a node's
// Alias field in the compiler's production code to one under a check that the
// node is an alias, or to nodeview's Written and Unread: the field of a turned
// scalar holds what the source wrote, which no other reader may follow.
func TestAliasField_OnlyNodeviewReadsItOnAScalar(t *testing.T) {
	t.Parallel()
	root := filepath.Join("..", "..")
	fset := token.NewFileSet()
	var unguarded []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		if strings.Contains(filepath.ToSlash(path), "/openapitest/") {
			return nil
		}
		file, perr := parser.ParseFile(fset, path, nil, 0)
		require.NoError(t, perr)
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			reads, guarded := false, false
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				if x, ok := n.(*ast.SelectorExpr); ok {
					switch x.Sel.Name {
					case "Alias":
						reads = true
					case "AliasNode":
						guarded = true
					}
				}
				return true
			})
			inNodeview := file.Name.Name == "nodeview" && (fn.Name.Name == "Written" || fn.Name.Name == "Unread")
			if reads && !guarded && !inNodeview {
				unguarded = append(unguarded, filepath.ToSlash(path)+": "+fn.Name.Name)
			}
		}
		return nil
	})
	require.NoError(t, err)
	assert.Empty(t, unguarded, "a read of Alias with no check of the node's kind")
}

// TestKeyOf_AKeyThatIsNoScalarNamesNoKey holds a pair whose key, its alias
// followed, is no scalar to no key: it is no pair of the mapping, for every
// reader of one.
func TestKeyOf_AKeyThatIsNoScalarNamesNoKey(t *testing.T) {
	t.Parallel()
	root := readMapping(t, "k: &k [x]\n? [a]\n: 1\n? *k\n: 2\n? {m: 1}\n: 3\nz: 4\n")
	pairs := root.Content

	for i := 0; i < len(pairs); i += 2 {
		key, ok := nodeview.KeyOf(pairs[i])
		switch pairs[i].Kind {
		case yaml.ScalarNode:
			assert.True(t, ok)
			assert.NotEmpty(t, key)
		default:
			assert.False(t, ok, "line %d: a sequence or mapping names no key", pairs[i].Line)
			assert.Empty(t, key)
		}
	}
	got := nodeview.Pairs(root)
	names := make([]string, 0, len(got))
	for _, p := range got {
		names = append(names, p.Key)
	}
	assert.Equal(t, []string{"k", "z"}, names, "the entries with such keys are left out")
	assert.Nil(t, nodeview.Child(root, ""), "and no entry stands for the key \"\"")
	assert.Empty(t, nodeview.RepeatedKeys(root), "two such keys are not one key written twice")
}

// TestKeyOf_AnAliasOfAScalarKeyStillNamesIt holds the other side: an alias
// whose target is a scalar is the key it names.
func TestKeyOf_AnAliasOfAScalarKeyStillNamesIt(t *testing.T) {
	t.Parallel()
	root := readMapping(t, "x: &a name\n*a : 1\n")

	pairs := nodeview.Pairs(root)
	names := make([]string, 0, len(pairs))
	for _, p := range pairs {
		names = append(names, p.Key)
	}

	assert.Equal(t, []string{"x", "name"}, names)
}

// TestMergeSources_NamesTheMappingsAMergeKeyNames holds the exported reader of
// a `<<` value to the mappings it merges: a list of them, in order, aliases
// followed.
func TestMergeSources_NamesTheMappingsAMergeKeyNames(t *testing.T) {
	t.Parallel()
	root := readMapping(t, "p: &p {a: 1}\nq: &q {b: 2}\nmany: {<<: [*p, *q]}\n")

	many := nodeview.Child(root, "many")
	require.NotNil(t, many)
	require.True(t, nodeview.IsMergeKey(many.Content[0]))
	merge := many.Content[1]

	require.NotNil(t, merge)
	got := nodeview.MergeSources(merge)
	require.Len(t, got, 2)
	assert.Equal(t, "a", got[0].Content[0].Value)
	assert.Equal(t, "b", got[1].Content[0].Value)
}
