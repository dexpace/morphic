package annotation

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/ir"
)

// TestPreserveInto_RecordsOnlyRealBytes pins the len check rather than a nil
// comparison: an empty payload preserves no construct, and it fails the encoding
// of the whole document that carries it.
func TestPreserveInto_RecordsOnlyRealBytes(t *testing.T) {
	t.Parallel()
	var p ir.Unmodeled
	PreserveInto(&p, "openapi:x", nil, ir.ReasonOutOfScope, sourced(0)("/x"))
	assert.Nil(t, p, "an absent payload allocates nothing")

	PreserveInto(&p, "openapi:x", ir.RawValue(""), ir.ReasonOutOfScope, sourced(0)("/x"))
	assert.Nil(t, p, "nor does an empty one")

	PreserveInto(&p, "openapi:x", ir.RawValue("1"), ir.ReasonOutOfScope, sourced(7)("/x"))
	assert.Equal(t, ir.Unmodeled{"openapi:x": {
		Reason: ir.ReasonOutOfScope, Value: ir.RawValue("1"),
		Provenance: ir.Provenance{Source: 7, Pointer: "/x"},
	}}, p)
}

// TestPreserveNodeInto_ReportsWhichOfThreeOutcomesHappened pins the bool its
// callers gate their announcement on. An absent node must report false with no
// diagnostic, and an unconvertible one false with the diagnostic that says
// nothing reached the IR.
func TestPreserveNodeInto_ReportsWhichOfThreeOutcomesHappened(t *testing.T) {
	t.Parallel()
	var p ir.Unmodeled

	kept, diags := PreserveNodeInto(&p, "openapi:x", nil, ir.ReasonNoIRHome, sourced(0)("/x"))
	assert.False(t, kept)
	assert.Empty(t, diags)
	assert.Nil(t, p)

	kept, diags = PreserveNodeInto(&p, "openapi:x", openapitest.YAMLNode(t, ".nan"), ir.ReasonNoIRHome, sourced(0)("/x"))
	assert.False(t, kept)
	require.Len(t, diags, 1)
	assert.Equal(t, ir.SeverityError, diags[0].Severity)
	assert.Nil(t, p, "an unconvertible node writes no entry")

	kept, diags = PreserveNodeInto(&p, "openapi:x", openapitest.YAMLNode(t, "{a: 1}"), ir.ReasonNoIRHome, sourced(0)("/x"))
	assert.True(t, kept)
	assert.Empty(t, diags)
	assert.JSONEq(t, `{"a":1}`, string(p["openapi:x"].Value))
}

// TestUnpreservableDiag_IsAnErrorNotADegradation pins the severity split: a
// degraded lowering still describes the source in a weaker shape, while this one
// leaves no trace at all, which is a losslessness failure.
func TestUnpreservableDiag_IsAnErrorNotADegradation(t *testing.T) {
	t.Parallel()
	_, err := RawFromNode(openapitest.YAMLNode(t, ".nan"))
	require.Error(t, err)

	got := UnpreservableDiag("openapi:not", sourced(2)("/components/schemas/S/not"), err)

	assert.Equal(t, ir.SeverityError, got.Severity)
	assert.Equal(t, ir.Provenance{Source: 2, Pointer: "/components/schemas/S/not"}, got.Provenance)
	assert.Contains(t, got.Message, "in no form at all")
}

func TestPreserveKeywordInto_EmptyPayloadRecordsAndAnnouncesNothing(t *testing.T) {
	t.Parallel()
	for name, raw := range map[string]ir.RawValue{"nil": nil, "empty non-nil": {}} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			var p ir.Unmodeled
			diags := PreserveKeywordInto(&p, "openapi:not", raw, sourced(0)("/p/not"), sourced(0)("/p"), "not")
			assert.Nil(t, p)
			assert.Empty(t, diags, "nothing was kept, so nothing is announced")
		})
	}
}
