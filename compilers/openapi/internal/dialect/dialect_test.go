// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package dialect_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/dialect"
	"github.com/dexpace/morphic/compilers/openapi/internal/reftable"
	"github.com/dexpace/morphic/compilers/openapi/internal/specrules"
)

// rows returns the Schema Object's rows at version in the dialect uri names,
// which the table must have.
func rows(t *testing.T, version, uri string) specrules.SchemaRules {
	t.Helper()
	schemaRules, unread := specrules.Table().SchemaRules(version, uri)
	require.False(t, unread, "%s %s", version, uri)
	return schemaRules
}

// TestTable_FallsBackFromTheRootToTheDocumentToTheSource pins the order every
// question of the table falls back in: a root at or above a position, else
// the position's document, else the source, as for a document the compile
// did not read beside it. A node no root governs reads the source's, and the
// zero Table the zero rows.
func TestTable_FallsBackFromTheRootToTheDocumentToTheSource(t *testing.T) {
	t.Parallel()
	source, other := rows(t, "3.0.3", ""), rows(t, "3.1.0", "")
	root := rows(t, "3.1.0", "http://json-schema.org/draft-04/schema#")
	var tree yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte("{type: object, properties: {a: {type: integer}}}"), &tree))
	held := tree.Content[0]
	b := dialect.NewBuilder(source)
	b.Document(1, other)
	b.Root(reftable.DocumentPointer{Doc: 1, Pointer: "/R"}, held, root)
	table := b.Table()

	for _, tc := range []struct {
		at   reftable.DocumentPointer
		want specrules.SchemaRules
		name string
	}{
		{reftable.DocumentPointer{Doc: 1, Pointer: "/R"}, root, "the root itself"},
		{reftable.DocumentPointer{Doc: 1, Pointer: "/R/properties/a"}, root, "beneath the root"},
		{reftable.DocumentPointer{Doc: 1, Pointer: "/S"}, other, "beside it, its document's"},
		{reftable.DocumentPointer{Doc: 0, Pointer: "/R"}, source, "the source's own"},
		{reftable.DocumentPointer{Doc: 2, Pointer: "/R"}, source, "a document the compile did not read"},
	} {
		if table.At(tc.at) != tc.want {
			t.Errorf("At(%v), %s: read another document's rows", tc.at, tc.name)
		}
	}
	if table.In(1) != other || table.In(reftable.Source) != source || table.In(2) != source {
		t.Error("In reads another document's rows than its own, or than the source's")
	}
	if table.Of(held.Content[3]) != root || table.Of(&yaml.Node{Kind: yaml.MappingNode}) != source {
		t.Error("Of reads a node another root's rows than the one holding it, or than the source's")
	}
	schemaRules, ok := table.RootAt(reftable.DocumentPointer{Doc: 1, Pointer: "/R"})
	assert.True(t, ok)
	assert.Equal(t, root, schemaRules)
	_, ok = table.RootAt(reftable.DocumentPointer{Doc: 1, Pointer: "/R/properties/a"})
	assert.False(t, ok, "a root above a position is not at it")
	assert.Equal(t, specrules.SchemaRules{}, dialect.SchemaDialectRows{}.At(reftable.DocumentPointer{}), "the zero Table reads the zero rows")
}

// TestTable_AsksTheSourcesRowsAtNoCost pins the cost of the question the
// schema readers ask of every schema they read: a lookup by position, where
// one by a document's name would parse it as a URL on each miss. It runs
// alone, since the allocation counts are the process's.
func TestTable_AsksTheSourcesRowsAtNoCost(t *testing.T) {
	table := dialect.NewBuilder(rows(t, "3.0.3", "")).Table()
	var defines bool
	allocs := testing.AllocsPerRun(100, func() {
		defines = table.At(reftable.DocumentPointer{Pointer: "/components/schemas/A"}).Defines("nullable")
	})
	assert.True(t, defines)
	assert.Zero(t, allocs)
}
