// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package irverify

import (
	"encoding/json/jsontext"
	"maps"
	"reflect"
	"slices"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
)

// TestRecordedPointer_ReadsTheProvenanceBesideTheID pins both halves of the
// helper appendDeclaredIDViolations reads a node's recorded pointer through. A
// node with a Provenance yields its pointer. One without, such as a group, yields
// none and does not fault on the missing field: no class in pointerDerived lacks
// a Provenance today, so only this keeps a later edit to that table from
// panicking Verify on a valid document.
func TestRecordedPointer_ReadsTheProvenanceBesideTheID(t *testing.T) {
	t.Parallel()
	recorded := ir.Property{Provenance: ir.Provenance{Pointer: "/components/schemas/M/properties/f"}}
	assert.Equal(t, jsontext.Pointer("/components/schemas/M/properties/f"), recordedPointer(reflect.ValueOf(recorded)))
	assert.Empty(t, recordedPointer(reflect.ValueOf(ir.OperationGroup{})))
}

// TestKindPrefixes_NameEveryIDKindOnce holds the class table to the kind list.
// A kind added to ir.IDKinds without a class here would have its IDs skipped by
// the grammar check and the namespace check alike, silently, because a class
// with no prefix is left alone; and a kind two classes share could not tell them
// apart. Nothing else ties the table to the list.
func TestKindPrefixes_NameEveryIDKindOnce(t *testing.T) {
	t.Parallel()
	got := slices.Sorted(maps.Values(kindPrefixes))
	want := slices.Sorted(slices.Values(ir.IDKinds()))
	assert.Empty(t, cmp.Diff(want, got),
		"every kind prefix ir lists names exactly one class of ID (-ir.IDKinds +kindPrefixes)")
}

// unlistedID is a class of ID kindPrefixes does not name, which only a test
// can declare: every class the IR declares has a prefix.
type unlistedID string

// unlistedNode declares an ID of unlistedID as its own, as an IR node does.
type unlistedNode struct{ ID unlistedID }

// TestAppendDeclaredIDViolations_AClassWithNoPrefixIsMalformed pins that a
// node declaring an ID of a class kindPrefixes does not name is reported,
// whatever the ID's spelling, one opening with the separator among them, where
// an empty prefix would read it as well-formed.
func TestAppendDeclaredIDViolations_AClassWithNoPrefixIsMalformed(t *testing.T) {
	t.Parallel()
	for _, id := range []string{"c/x/C", "/x/C", "x"} {
		node := reflect.ValueOf(unlistedNode{ID: unlistedID(id)})
		vs := appendDeclaredIDViolations(nil, node, reflect.TypeFor[unlistedID](), id, ir.WalkPath{})
		require.Len(t, vs, 1, "id %q", id)
		assert.Equal(t, "ir/id-malformed", vs[0].Code, "id %q", id)
	}
}

// TestIDSpaceViolations_AClassWithNoPrefixIsNotPlaced pins that an ID of a class
// with no prefix is left to the malformed report rather than also placed in a
// namespace: read with an empty prefix, "/x/C" would name the namespace "x".
func TestIDSpaceViolations_AClassWithNoPrefixIsNotPlaced(t *testing.T) {
	t.Parallel()
	var collected ir.IDDeclarations
	ir.WalkValues(&unlistedNode{ID: "/x/C"}, ir.DocumentPath, &collected)
	require.Len(t, collected.List(), 1, "the node declares its ID")
	doc := &ir.Document{IDSpaces: map[string][]string{ir.IDKindType: {"y"}}}

	for _, v := range idSpaceViolations(doc, declarations{collected: &collected}) {
		assert.NotEqual(t, "ir/id-space-undeclared", v.Code, "%s at %s", v.Message, v.Path)
	}
}
