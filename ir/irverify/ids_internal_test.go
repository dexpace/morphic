package irverify

import (
	"encoding/json/jsontext"
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/ir"
)

// TestRecordedPointer_ReadsTheProvenanceBesideTheID pins both halves of the
// helper checkDeclaredIDShapes reads a node's recorded pointer through. A node
// with a Provenance yields its pointer. One without, such as a group, yields
// none and does not fault on the missing field: no class declaredKinds holds to
// agreement lacks a Provenance today, so only this keeps a later edit to that
// table from panicking Verify on a valid document.
func TestRecordedPointer_ReadsTheProvenanceBesideTheID(t *testing.T) {
	t.Parallel()
	recorded := ir.Property{Provenance: ir.Provenance{Pointer: "/components/schemas/M/properties/f"}}
	assert.Equal(t, jsontext.Pointer("/components/schemas/M/properties/f"), recordedPointer(reflect.ValueOf(recorded)))
	assert.Empty(t, recordedPointer(reflect.ValueOf(ir.OperationGroup{})))
}
