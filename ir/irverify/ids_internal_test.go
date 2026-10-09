package irverify

import (
	"encoding/json/jsontext"
	"maps"
	"reflect"
	"slices"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"

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

// TestAppendDeclaredIDViolations_ClassWithoutAPrefixIsLeftAlone pins the guard
// for a class that declares its own ID and has no kind prefix to hold it to.
// Channels and messages are such classes, but a registry holds them, so no
// document reaches this today; the guard keeps a later class from being judged
// against a prefix it does not have.
func TestAppendDeclaredIDViolations_ClassWithoutAPrefixIsLeftAlone(t *testing.T) {
	t.Parallel()
	node := reflect.ValueOf(ir.Channel{ID: "c/x/C"})
	assert.Empty(t, appendDeclaredIDViolations(nil, node, reflect.TypeFor[ir.ChannelID](), "c/x/C", "doc.Channels[c/x/C]"))
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
