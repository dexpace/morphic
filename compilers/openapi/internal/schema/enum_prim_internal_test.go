// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/ir"
)

// scalar is a YAML scalar node of tag holding value.
func scalar(tag, value string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: tag, Value: value}
}

// TestEnumMemberPrimitive_ReadsWhatEnumMembersGives pins enumMemberPrimitive, the reading
// a position asking only its ValueType takes, to enumMembers' under a type's
// ID: one PrimitiveKind and one refusal for each list, members of one kind, of
// two, of a kind no Enum holds, and none.
func TestEnumMemberPrimitive_ReadsWhatEnumMembersGives(t *testing.T) {
	t.Parallel()
	for name, nodes := range map[string][]*yaml.Node{
		"strings":  {scalar("!!str", "a"), scalar("!!str", "b"), scalar("!!str", "a")},
		"numbers":  {scalar("!!int", "1"), scalar("!!float", "2.5")},
		"booleans": {scalar("!!bool", "true")},
		"mixed":    {scalar("!!str", "a"), scalar("!!int", "1")},
		"a list":   {{Kind: yaml.SequenceNode, Tag: "!!seq"}},
		"none":     nil,
	} {
		entries := readEnum(nodes)
		members, wantPrimitive, wantOK := enumMembers(entries, "type/openapi/components/schemas/E")
		primitive, ok := enumMemberPrimitive(entries)
		assert.Equal(t, wantOK, ok, name)
		assert.Equal(t, wantPrimitive, primitive, name)
		for _, primitiveKind := range []ir.PrimitiveKind{ir.PrimitiveString, ir.PrimitiveInteger, ir.PrimitiveNumber, ir.PrimitiveBool} {
			assert.Equal(t, enumMembersAre(members, primitiveKind), !wantOK || entriesAre(entries, primitiveKind), "%s %s", name, primitiveKind)
		}
	}
}

// TestEnumMemberPrimitive_BuildsNoMember pins that a ValueType's reading of a set
// allocates nothing however many members it holds: no member, ID or name is
// minted for a position that only asks its type.
func TestEnumMemberPrimitive_BuildsNoMember(t *testing.T) {
	nodes := make([]*yaml.Node, 0, 200)
	for i := range 200 {
		nodes = append(nodes, scalar("!!str", fmt.Sprintf("v%d", i)))
	}
	entries := readEnum(nodes)
	allocs := testing.AllocsPerRun(10, func() {
		if _, ok := enumMemberPrimitive(entries); !ok || !entriesAre(entries, ir.PrimitiveString) {
			t.Fatal("200 strings are an Enum of strings")
		}
	})
	assert.Zero(t, allocs)
}
