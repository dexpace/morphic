// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema

import (
	"encoding/json/jsontext"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/annotation"
	"github.com/dexpace/morphic/compilers/openapi/internal/statement"
)

// keptRefEntry is a $ref entry at pointer writing text beside x-a, whose value
// is the scalar value.
func keptRefEntry(pointer jsontext.Pointer, text, value string) keptBranch {
	node := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map", Content: []*yaml.Node{
		{Kind: yaml.ScalarNode, Tag: "!!str", Value: "$ref"}, {Kind: yaml.ScalarNode, Tag: "!!str", Value: text},
		{Kind: yaml.ScalarNode, Tag: "!!str", Value: "x-a"}, {Kind: yaml.ScalarNode, Tag: "!!str", Value: value},
	}}
	st := &statement.Statement{Schema: &statement.Schema{Node: node, Ref: &statement.Reference{Keyword: "$ref", Text: text}}}
	return keptBranch{entry: allOfBranch{schema: st, pointer: pointer}}
}

// TestKeptRefKeys_AnEntryThatCannotRenderIsAlikeToNoOther pins the rank of a
// kept $ref entry that does not render as JSON, here for invalid UTF-8: it is
// ranked by its pointer, so two such entries writing one reference are two
// keys, each at its own entry, where two entries rendering alike are one key at
// the position.
func TestKeptRefKeys_AnEntryThatCannotRenderIsAlikeToNoOther(t *testing.T) {
	t.Parallel()
	const bad = "\xff"
	_, err := annotation.RawFromNode(keptRefEntry("", "#/T", bad).entry.schema.Node)
	require.Error(t, err, "the entry must be one that does not render")

	keys := keptRefKeys([]keptBranch{keptRefEntry("/S/allOf/0", "#/T", bad), keptRefEntry("/S/allOf/1", "#/T", bad)}, "/S")
	assert.Equal(t, map[jsontext.Pointer]keptRef{
		"/S/allOf/0": {key: "openapi:$ref/0", at: "/S/allOf/0"},
		"/S/allOf/1": {key: "openapi:$ref/1", at: "/S/allOf/1"},
	}, keys)

	keys = keptRefKeys([]keptBranch{keptRefEntry("/S/allOf/0", "#/T", "ok"), keptRefEntry("/S/allOf/1", "#/T", "ok")}, "/S")
	assert.Equal(t, map[jsontext.Pointer]keptRef{
		"/S/allOf/0": {key: "openapi:$ref", at: "/S"},
		"/S/allOf/1": {key: "openapi:$ref", at: "/S"},
	}, keys)
}
