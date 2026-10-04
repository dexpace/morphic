// This file is a package-level suite, not a per-source-file test: it sweeps the
// whole committed corpus through the compiler as another document's content,
// so it has no single source file to pair with.
package openapi_test // external test package — exercises only the public API

import (
	"encoding/json/v2"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi"
)

// externalOrderDependent names the corpus specs whose external compile still
// follows declaration order, each with the issue that tracks it. A listed spec
// that stops following it fails the sweep too, so the list cannot outlive its
// reason.
var externalOrderDependent = map[string]string{
	"pointer_hint_cross_path_subtree.yaml":  "GitHub #762",
	"ref_back_into_a_node_being_built.yaml": "GitHub #762",
}

// TestExternalCorpus_DeclarationOrderDecidesNothing compiles every OpenAPI 3
// spec in the corpus as another document, reached by a source that $refs each
// of its path items and components, once in its order and once reversed. The
// two must report the same diagnostics and build the same types.
//
// It is the harness's order oracle for what a $ref brings in from another
// document, which the harness never compiles: a schema chain there failed in
// one order only (GitHub #761).
func TestExternalCorpus_DeclarationOrderDecidesNothing(t *testing.T) {
	t.Parallel()
	swept := 0
	for _, path := range corpusSpecs(t) {
		data, err := os.ReadFile(path)
		require.NoError(t, err)
		forward, reversed, ok := referencingRoots(data)
		if !ok {
			continue
		}
		swept++
		t.Run(filepath.Base(path), func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(dir, "ext.yaml"), data, 0o600))
			diff := cmp.Diff(compiledThrough(t, dir, forward), compiledThrough(t, dir, reversed))
			if issue, known := externalOrderDependent[filepath.Base(path)]; known {
				assert.NotEmpty(t, diff, "%s no longer follows declaration order: drop it from the list", issue)
				return
			}
			assert.Empty(t, diff, "declaration order decided what compiling the spec as another document reports")
		})
	}
	assert.Positive(t, swept, "the sweep reached no OpenAPI 3 spec")
}

// referencingRoots returns two sources that each $ref every path item and every
// component of the OpenAPI 3 document data as ext.yaml: one in data's order,
// and one with each list reversed. It reports false for data that is no OpenAPI
// 3 document.
func referencingRoots(data []byte) (forward, reversed string, ok bool) {
	var doc yaml.Node
	if yaml.Unmarshal(data, &doc) != nil || len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		return "", "", false
	}
	top := doc.Content[0]
	if version := mappingValue(top, "openapi"); version == nil || !strings.HasPrefix(version.Value, "3.") {
		return "", "", false
	}
	paths := mappingKeys(mappingValue(top, "paths"))
	components := mappingValue(top, "components")
	kinds := mappingKeys(components)
	render := func(order func([]string) []string) string {
		var pathItems, kindEntries strings.Builder
		for _, p := range order(paths) {
			fmt.Fprintf(&pathItems, "  %q: {$ref: %q}\n", p, "./ext.yaml#/paths/"+pointerToken(p))
		}
		for _, kind := range kinds {
			fmt.Fprintf(&kindEntries, "  %q:\n", kind)
			for _, name := range order(mappingKeys(mappingValue(components, kind))) {
				fmt.Fprintf(&kindEntries, "    %q: {$ref: %q}\n", name,
					"./ext.yaml#/components/"+pointerToken(kind)+"/"+pointerToken(name))
			}
		}
		return "openapi: 3.1.0\ninfo: {title: T, version: \"1\"}\n" +
			block("paths", pathItems.String()) + block("components", kindEntries.String())
	}
	reverse := func(s []string) []string {
		out := slices.Clone(s)
		slices.Reverse(out)
		return out
	}
	return render(slices.Clone[[]string]), render(reverse), true
}

// block renders the mapping key with the entries given, or empty.
func block(key, entries string) string {
	if entries == "" {
		return key + ": {}\n"
	}
	return key + ":\n" + entries
}

// mappingValue returns the value under key in mapping n, or nil.
func mappingValue(n *yaml.Node, key string) *yaml.Node {
	if n == nil || n.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		if n.Content[i].Value == key {
			return n.Content[i+1]
		}
	}
	return nil
}

// mappingKeys returns the keys of mapping n that name entries with a mapping
// for a value, which is every path item and component a $ref can name, or
// none when n is no mapping.
func mappingKeys(n *yaml.Node) []string {
	if n == nil || n.Kind != yaml.MappingNode {
		return nil
	}
	var out []string
	for i := 0; i+1 < len(n.Content); i += 2 {
		if n.Content[i+1].Kind == yaml.MappingNode {
			out = append(out, n.Content[i].Value)
		}
	}
	return out
}

// pointerToken escapes s as one JSON pointer token.
func pointerToken(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, "~", "~0"), "/", "~1")
}

// compiledThrough compiles root as root.yaml in dir with external references
// allowed, and returns what an order comparison reads: each diagnostic, sorted,
// and the types, encoded deterministically.
func compiledThrough(t *testing.T, dir, root string) []string {
	t.Helper()
	path := filepath.Join(dir, "root.yaml")
	require.NoError(t, os.WriteFile(path, []byte(root), 0o600))
	doc, diags, err := openapi.New().Compile(t.Context(), []compilers.Source{{Path: path, Data: []byte(root)}},
		compilers.Options{FormatOptions: openapi.Options{AllowExternalRefs: true}})
	require.NoError(t, err)
	out := make([]string, 0, len(diags)+1)
	for _, d := range diags {
		out = append(out, fmt.Sprintf("%s %s %s %s", d.Severity, d.Code, d.Provenance.Pointer, d.Message))
	}
	slices.Sort(out)
	if doc != nil {
		types, err := json.Marshal(doc.Types, json.Deterministic(true))
		require.NoError(t, err)
		out = append(out, string(types))
	}
	return out
}
