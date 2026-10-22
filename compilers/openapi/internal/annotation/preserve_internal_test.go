package annotation

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
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

// TestPreserveSchemaKeyword_KeysAndLocatesTheEntryAtTheKeyword pins what the
// function derives rather than takes: the census key, and the keyword's own
// node beneath the schema's pointer. The Locator answers a different source for
// that node than for the schema, so placing the entry at the schema shows.
func TestPreserveSchemaKeyword_KeysAndLocatesTheEntryAtTheKeyword(t *testing.T) {
	t.Parallel()
	s := schemaFromYAML(t, "type: string\nformat: password\n")
	var p ir.Unmodeled

	kept, diags := PreserveSchemaKeyword(&p, s, "format", ir.ReasonNoIRHome, "/S", overlaid("/S/format"))

	assert.True(t, kept)
	assert.Empty(t, diags)
	assert.Equal(t, ir.Unmodeled{EntryKey("", "format"): {
		Reason: ir.ReasonNoIRHome, Value: ir.RawValue(`"password"`),
		Provenance: ir.Provenance{Source: 1, Pointer: "/S/format"},
	}}, p)
}

// TestPreserveSchemaKeyword_ReportsAnUnkeptKeywordAtItsNode pins the other two
// outcomes PreserveNodeInto distinguishes, as this function reaches them: an
// absent keyword keeps and says nothing, nor asks the Locator anything, and an
// unconvertible one keeps nothing and is reported at the keyword's node.
func TestPreserveSchemaKeyword_ReportsAnUnkeptKeywordAtItsNode(t *testing.T) {
	t.Parallel()
	s := schemaFromYAML(t, "type: number\ndefault: .nan\n")
	var p ir.Unmodeled
	var calls []recordedCall

	kept, diags := PreserveSchemaKeyword(&p, s, "format", ir.ReasonNoIRHome, "/S", recording(&calls))
	assert.False(t, kept)
	assert.Empty(t, diags, "an absent keyword is no construct")
	assert.Empty(t, calls, "and no position to ask about")

	kept, diags = PreserveSchemaKeyword(&p, s, "default", ir.ReasonNoIRHome, "/S", overlaid("/S/default"))
	assert.False(t, kept)
	assert.Nil(t, p, "an unconvertible keyword writes no entry")
	require.Len(t, diags, 1)
	assert.Equal(t, diag.UnpreservableConstruct, diags[0].Code)
	assert.Equal(t, ir.Provenance{Source: 1, Pointer: "/S/default"}, diags[0].Provenance)
}

// TestPreserveValidationOnlyKeyword_SplitsTheEntryFromItsAnnouncement pins the
// two positions it derives: the entry at the keyword's own node, which a
// validation emitter reports against, and the announcement at the schema that
// wrote it. The Locator answers a different source for each, so swapping them
// shows.
func TestPreserveValidationOnlyKeyword_SplitsTheEntryFromItsAnnouncement(t *testing.T) {
	t.Parallel()
	s := schemaFromYAML(t, "type: string\nnot: {const: x}\n")
	var p ir.Unmodeled

	diags := PreserveValidationOnlyKeyword(&p, s, "not", "/S", overlaid("/S/not"))

	assert.Equal(t, ir.Unmodeled{EntryKey("", "not"): {
		Reason: ir.ReasonValidationOnly, Value: ir.RawValue(`{"const":"x"}`),
		Provenance: ir.Provenance{Source: 1, Pointer: "/S/not"},
	}}, p)
	require.Len(t, diags, 1)
	assert.Equal(t, diag.ValidationOnlyKeyword, diags[0].Code)
	assert.Equal(t, ir.Provenance{Source: 0, Pointer: "/S"}, diags[0].Provenance)
}

// TestPreserveValidationOnlyKeyword_ReportsAnUnkeptKeywordAtItsNode pins that a
// keyword which cannot be converted is reported where its entry would have
// been, and that an absent one announces nothing and asks the Locator nothing.
func TestPreserveValidationOnlyKeyword_ReportsAnUnkeptKeywordAtItsNode(t *testing.T) {
	t.Parallel()
	s := schemaFromYAML(t, "type: object\ndependentRequired: {a: [.nan]}\n")
	var p ir.Unmodeled
	var calls []recordedCall

	assert.Empty(t, PreserveValidationOnlyKeyword(&p, s, "not", "/S", recording(&calls)), "an absent keyword is no construct")
	assert.Empty(t, calls, "and no position to ask about")

	diags := PreserveValidationOnlyKeyword(&p, s, "dependentRequired", "/S", overlaid("/S/dependentRequired"))
	assert.Nil(t, p, "an unconvertible keyword writes no entry")
	require.Len(t, diags, 1)
	assert.Equal(t, diag.UnpreservableConstruct, diags[0].Code)
	assert.Equal(t, ir.Provenance{Source: 1, Pointer: "/S/dependentRequired"}, diags[0].Provenance)
}

// TestUnpreservableDiag_IsAnErrorNotADegradation pins the severity split: a
// degraded lowering still describes the source in a weaker shape, while this one
// leaves no trace at all, which is a losslessness failure.
func TestUnpreservableDiag_IsAnErrorNotADegradation(t *testing.T) {
	t.Parallel()
	_, err := RawFromNode(openapitest.YAMLNode(t, ".nan"))
	require.Error(t, err)

	got := unpreservableDiag("openapi:not", sourced(2)("/components/schemas/S/not"), err)

	assert.Equal(t, ir.SeverityError, got.Severity)
	assert.Equal(t, ir.Provenance{Source: 2, Pointer: "/components/schemas/S/not"}, got.Provenance)
	assert.Contains(t, got.Message, "in no form at all")
}

func TestPreserveValidationOnlyInto_EmptyPayloadRecordsAndAnnouncesNothing(t *testing.T) {
	t.Parallel()
	for name, raw := range map[string]ir.RawValue{"nil": nil, "empty non-nil": {}} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			var p ir.Unmodeled
			diags := preserveValidationOnlyInto(&p, "openapi:not", raw, sourced(0)("/p/not"), sourced(0)("/p"), "not")
			assert.Nil(t, p)
			assert.Empty(t, diags, "nothing was kept, so nothing is announced")
		})
	}
}

// TestMergeUnmodeled_AllocatesOnlyOnFirstWrite pins the overlay: merging nothing
// leaves the destination exactly as it was — including nil, which must not
// become an empty map.
func TestMergeUnmodeled_AllocatesOnlyOnFirstWrite(t *testing.T) {
	t.Parallel()
	assert.Nil(t, MergeUnmodeled(nil, nil))
	assert.Nil(t, MergeUnmodeled(nil, ir.Unmodeled{}))

	src := ir.Unmodeled{"a": {Reason: ir.ReasonOutOfScope}}
	assert.Equal(t, src, MergeUnmodeled(nil, src), "a nil destination is allocated to hold the first entry")

	dst := ir.Unmodeled{"b": {Reason: ir.ReasonNoIRHome}}
	assert.Equal(t, ir.Unmodeled{
		"a": {Reason: ir.ReasonOutOfScope}, "b": {Reason: ir.ReasonNoIRHome},
	}, MergeUnmodeled(dst, src))
}
