package graphql_test // external test package — exercises only the public API

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/graphql"
	"github.com/dexpace/morphic/ir"
)

// TestRoundTrip asserts the serialization property for every corpus document:
// compile → MarshalJSON → UnmarshalJSON → MarshalJSON is byte-stable. Comparing
// the re-marshaled bytes exercises the sealed TypeDef codec without depending on
// deep-equality of the interface-valued type registry.
func TestRoundTrip(t *testing.T) {
	t.Parallel()
	for _, name := range corpusNames(t) {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			doc := compileFile(t, filepath.Join(conformanceDir, name))
			assertRoundTrips(t, doc)
		})
	}
	t.Run("social", func(t *testing.T) {
		t.Parallel()
		doc := compileFile(t, "../../testdata/golden/graphql/social.graphql")
		assertRoundTrips(t, doc)
	})
}

// assertRoundTrips marshals, unmarshals, and re-marshals doc, requiring the two
// serializations to be byte-identical.
func assertRoundTrips(t *testing.T, doc *ir.Document) {
	t.Helper()
	first, err := json.Marshal(doc)
	require.NoError(t, err)
	var back ir.Document
	require.NoError(t, json.Unmarshal(first, &back))
	second, err := json.Marshal(&back)
	require.NoError(t, err)
	if diff := cmp.Diff(string(first), string(second)); diff != "" {
		t.Errorf("round-trip mismatch (-first +second):\n%s", diff)
	}
}

// compileFile compiles one SDL file through the full compiler.
func compileFile(t *testing.T, path string) *ir.Document {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	doc, _, err := graphql.New().Compile(t.Context(),
		[]compilers.Source{{Path: filepath.Base(path), Data: data}}, compilers.Options{})
	require.NoError(t, err)
	require.NotNil(t, doc)
	return doc
}

// corpusNames returns every *.graphql file name in the conformance corpus.
func corpusNames(t *testing.T) []string {
	t.Helper()
	entries, err := os.ReadDir(conformanceDir)
	require.NoError(t, err)
	var names []string
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".graphql" {
			names = append(names, e.Name())
		}
	}
	require.NotEmpty(t, names)
	return names
}
