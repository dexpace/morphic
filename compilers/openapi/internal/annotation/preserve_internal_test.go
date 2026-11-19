// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package annotation

import (
	"encoding/json/jsontext"
	"fmt"
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
	var unmodeled ir.Unmodeled
	PreserveInto(&unmodeled, "openapi:x", nil, ir.ReasonOutOfScope, sourced(0).At("/x"))
	assert.Nil(t, unmodeled, "an absent payload allocates nothing")

	PreserveInto(&unmodeled, "openapi:x", ir.RawValue(""), ir.ReasonOutOfScope, sourced(0).At("/x"))
	assert.Nil(t, unmodeled, "nor does an empty one")

	PreserveInto(&unmodeled, "openapi:x", ir.RawValue("1"), ir.ReasonOutOfScope, sourced(7).At("/x"))
	assert.Equal(t, ir.Unmodeled{"openapi:x": {
		Reason: ir.ReasonOutOfScope, Value: ir.RawValue("1"),
		Provenance: ir.Provenance{Source: 7, Pointer: "/x"},
	}}, unmodeled)
}

// TestPreserveNodeInto_ReportsWhichOfThreeOutcomesHappened pins the bool its
// callers gate their announcement on. An absent node must report false with no
// diagnostic, and an unconvertible one false with the diagnostic that says
// nothing reached the IR.
func TestPreserveNodeInto_ReportsWhichOfThreeOutcomesHappened(t *testing.T) {
	t.Parallel()
	var unmodeled ir.Unmodeled

	kept, diags := PreserveNodeInto(&unmodeled, "openapi:x", nil, ir.ReasonNoIRHome, sourced(0).At("/x"))
	assert.False(t, kept)
	assert.Empty(t, diags)
	assert.Nil(t, unmodeled)

	kept, diags = PreserveNodeInto(&unmodeled, "openapi:x", openapitest.YAMLNode(t, ".nan"), ir.ReasonNoIRHome, sourced(0).At("/x"))
	assert.False(t, kept)
	require.Len(t, diags, 1)
	assert.Equal(t, ir.SeverityError, diags[0].Severity)
	assert.Nil(t, unmodeled, "an unconvertible node writes no entry")

	kept, diags = PreserveNodeInto(&unmodeled, "openapi:x", openapitest.YAMLNode(t, "{a: 1}"), ir.ReasonNoIRHome, sourced(0).At("/x"))
	assert.True(t, kept)
	assert.Empty(t, diags)
	assert.JSONEq(t, `{"a":1}`, string(unmodeled["openapi:x"].Value))
}

// TestPreserveSchemaKeyword_KeysAndLocatesTheEntryAtTheKeyword pins what the
// function derives rather than takes: the census key, and the keyword's own
// node beneath the schema's pointer. The overlay supplied that node and not the
// schema, so placing the entry at the schema shows.
func TestPreserveSchemaKeyword_KeysAndLocatesTheEntryAtTheKeyword(t *testing.T) {
	t.Parallel()
	s := schemaFromYAML(t, "type: string\nformat: password\n")
	var unmodeled ir.Unmodeled

	kept, diags := PreserveSchemaKeyword(&unmodeled, standaloneSchema(s), "format", ir.ReasonNoIRHome, "/S", overlaid("/S/format"))

	assert.True(t, kept)
	assert.Empty(t, diags)
	assert.Equal(t, ir.Unmodeled{EntryKey("", "format"): {
		Reason: ir.ReasonNoIRHome, Value: ir.RawValue(`"password"`),
		Provenance: ir.Provenance{Pointer: "/S/format", WrittenBy: 1},
	}}, unmodeled)
}

// TestPreserveSchemaKeyword_ReportsAnUnkeptKeywordAtItsNode pins the other two
// outcomes PreserveNodeInto distinguishes, as this function reaches them: an
// absent keyword keeps and says nothing, and an unconvertible one keeps nothing
// and is reported at the keyword's node.
func TestPreserveSchemaKeyword_ReportsAnUnkeptKeywordAtItsNode(t *testing.T) {
	t.Parallel()
	s := schemaFromYAML(t, "type: number\ndefault: .nan\n")
	var unmodeled ir.Unmodeled

	kept, diags := PreserveSchemaKeyword(&unmodeled, standaloneSchema(s), "format", ir.ReasonNoIRHome, "/S", sourced(0))
	assert.False(t, kept)
	assert.Empty(t, diags, "an absent keyword is no construct")

	kept, diags = PreserveSchemaKeyword(&unmodeled, standaloneSchema(s), "default", ir.ReasonNoIRHome, "/S", overlaid("/S/default"))
	assert.False(t, kept)
	assert.Nil(t, unmodeled, "an unconvertible keyword writes no entry")
	require.Len(t, diags, 1)
	assert.Equal(t, diag.UnpreservableConstruct, diags[0].Code)
	assert.Equal(t, ir.Provenance{Pointer: "/S/default", WrittenBy: 1}, diags[0].Provenance)
}

// TestPreserveSchemaKeyword_TheCensusLeavesItsEntryAlone holds the function to
// the reason it derives its key and position: the census, run after it as the
// schema lowering runs it, finds the entry it wrote and adds nothing. A key it
// does not recognise would be kept a second time, and a position it does not
// recognise reported as a collision.
func TestPreserveSchemaKeyword_TheCensusLeavesItsEntryAlone(t *testing.T) {
	t.Parallel()
	s := schemaFromYAML(t, "type: string\nnullable: true\n")
	var unmodeled ir.Unmodeled
	kept, diags := PreserveSchemaKeyword(&unmodeled, standaloneSchema(s), "nullable", ir.ReasonDegradedLowering, "/S", sourced(0))
	require.True(t, kept)
	require.Empty(t, diags)

	census := UndefinedKeywordsIn(&unmodeled, standaloneSchema(s), []string{"nullable"}, "/S", sourced(0))

	assert.Empty(t, census, "the census announces nothing it did not keep")
	require.Len(t, unmodeled, 1, "and keeps no second entry")
	assert.Equal(t, ir.ReasonDegradedLowering, unmodeled[EntryKey("", "nullable")].Reason, "nor rewrites this one")
}

// TestPreserveValidationOnlyKeyword_SplitsTheEntryFromItsAnnouncement pins the
// two positions it derives: the entry at the keyword's own node, which a
// validation emitter reports against, and the announcement at the schema that
// wrote it. The overlay supplied the keyword and not the schema, so swapping
// them shows.
func TestPreserveValidationOnlyKeyword_SplitsTheEntryFromItsAnnouncement(t *testing.T) {
	t.Parallel()
	s := schemaFromYAML(t, "type: string\nnot: {const: x}\n")
	var unmodeled ir.Unmodeled

	diags := PreserveValidationOnlyKeyword(&unmodeled, standaloneSchema(s), "not", "/S", overlaid("/S/not"))

	assert.Equal(t, ir.Unmodeled{EntryKey("", "not"): {
		Reason: ir.ReasonValidationOnly, Value: ir.RawValue(`{"const":"x"}`),
		Provenance: ir.Provenance{Pointer: "/S/not", WrittenBy: 1},
	}}, unmodeled)
	require.Len(t, diags, 1)
	assert.Equal(t, diag.ValidationOnlyKeyword, diags[0].Code)
	assert.Equal(t, ir.Provenance{Source: 0, Pointer: "/S"}, diags[0].Provenance)
}

// TestPreserveValidationOnlyKeyword_ReportsAnUnkeptKeywordAtItsNode pins that a
// keyword which cannot be converted is reported where its entry would have
// been, and that an absent one announces nothing.
func TestPreserveValidationOnlyKeyword_ReportsAnUnkeptKeywordAtItsNode(t *testing.T) {
	t.Parallel()
	s := schemaFromYAML(t, "type: object\ndependentRequired: {a: [.nan]}\n")
	var unmodeled ir.Unmodeled

	assert.Empty(t, PreserveValidationOnlyKeyword(&unmodeled, standaloneSchema(s), "not", "/S", sourced(0)), "an absent keyword is no construct")

	diags := PreserveValidationOnlyKeyword(&unmodeled, standaloneSchema(s), "dependentRequired", "/S", overlaid("/S/dependentRequired"))
	assert.Nil(t, unmodeled, "an unconvertible keyword writes no entry")
	require.Len(t, diags, 1)
	assert.Equal(t, diag.UnpreservableConstruct, diags[0].Code)
	assert.Equal(t, ir.Provenance{Pointer: "/S/dependentRequired", WrittenBy: 1}, diags[0].Provenance)
}

// TestUnpreservableDiag_IsAnErrorNotADegradation pins the severity split: a
// degraded lowering still describes the source in a weaker shape, while this one
// leaves no trace at all, which is a losslessness failure.
func TestUnpreservableDiag_IsAnErrorNotADegradation(t *testing.T) {
	t.Parallel()
	_, err := RawFromNode(openapitest.YAMLNode(t, ".nan"))
	require.Error(t, err)

	got := unpreservableDiag("openapi:not", sourced(2).At("/components/schemas/S/not"), err)

	assert.Equal(t, ir.SeverityError, got.Severity)
	assert.Equal(t, ir.Provenance{Source: 2, Pointer: "/components/schemas/S/not"}, got.Provenance)
	assert.Contains(t, got.Message, "in no form at all")
}

// TestPreserveValidationOnlyInto_EmptyPayloadRecordsAndAnnouncesNothing pins the
// recorder's len check: a nil and a zero-length payload both keep no entry, so
// neither may be announced as kept.
func TestPreserveValidationOnlyInto_EmptyPayloadRecordsAndAnnouncesNothing(t *testing.T) {
	t.Parallel()
	for name, raw := range map[string]ir.RawValue{"nil": nil, "empty non-nil": {}} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			var unmodeled ir.Unmodeled
			diags := preserveValidationOnlyInto(&unmodeled, "openapi:not", raw, sourced(0).At("/p/not"), sourced(0).At("/p"), "not")
			assert.Nil(t, unmodeled)
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

// TestUnderlayAdd_TheSiteOwnsWhatSeveralWrite pins what a folded branch may
// add: an entry the site does not hold. One the site's own schema holds stays
// the site's, its provenance with it; one another folded branch wrote takes the
// site's provenance, so no order of the folded branches names a writer.
func TestUnderlayAdd_TheSiteOwnsWhatSeveralWrite(t *testing.T) {
	t.Parallel()
	site := ir.Provenance{Pointer: "/site"}
	near := ir.UnmodeledEntry{Reason: ir.ReasonVendorExtension, Provenance: ir.Provenance{Pointer: "/near"}}
	far := ir.UnmodeledEntry{Reason: ir.ReasonVendorExtension, Provenance: ir.Provenance{Pointer: "/far"}}
	atSite := ir.UnmodeledEntry{Reason: ir.ReasonVendorExtension, Provenance: site}

	own := underlay{own: map[string]bool{"x-a": true}}
	got := own.add(ir.Unmodeled{"x-a": near}, ir.Unmodeled{"x-a": far, "x-b": far}, false, site)
	assert.Equal(t, ir.Unmodeled{"x-a": near, "x-b": far}, got, "the site's own entry stands")

	var underlayValue underlay
	got = underlayValue.add(ir.Unmodeled{"x-a": near}, ir.Unmodeled{"x-a": far}, false, site)
	assert.Equal(t, ir.Unmodeled{"x-a": atSite}, got, "an entry two voices write is the site's")

	assert.Equal(t, ir.Unmodeled{"x-b": far}, underlayValue.add(nil, ir.Unmodeled{"x-b": far}, false, site))
	assert.Nil(t, underlayValue.add(nil, nil, false, site), "nothing to add allocates nothing")
}

// TestUnderlayAdd_AFoldedBranchKeptWholeWritesForNoOne pins that a folded
// branch kept whole, whose branch holds what it folds, counts as no writer: it
// fills a key alone, gives it up to a folded branch kept apart in either order,
// and leaves one two folded branches kept apart write at the site.
func TestUnderlayAdd_AFoldedBranchKeptWholeWritesForNoOne(t *testing.T) {
	t.Parallel()
	site := ir.Provenance{Pointer: "/site"}
	whole := ir.UnmodeledEntry{Reason: ir.ReasonVendorExtension, Provenance: ir.Provenance{Pointer: "/allOf/1/x-a"}}
	apart := ir.UnmodeledEntry{Reason: ir.ReasonVendorExtension, Provenance: ir.Provenance{Pointer: "/allOf/2/x-a"}}
	atSite := ir.UnmodeledEntry{Reason: ir.ReasonVendorExtension, Provenance: site}

	var first underlay
	got := first.add(nil, ir.Unmodeled{"x-a": whole}, true, site)
	assert.Equal(t, ir.Unmodeled{"x-a": whole}, got, "a voice kept whole fills a key alone")
	got = first.add(got, ir.Unmodeled{"x-a": whole}, true, site)
	assert.Equal(t, ir.Unmodeled{"x-a": whole}, got, "a second voice kept whole writes nothing")
	got = first.add(got, ir.Unmodeled{"x-a": apart}, false, site)
	assert.Equal(t, ir.Unmodeled{"x-a": apart}, got, "a voice kept apart takes the key")
	got = first.add(got, ir.Unmodeled{"x-a": apart}, false, site)
	assert.Equal(t, ir.Unmodeled{"x-a": atSite}, got, "two voices kept apart leave it at the site")

	var last underlay
	got = last.add(nil, ir.Unmodeled{"x-a": apart}, false, site)
	got = last.add(got, ir.Unmodeled{"x-a": whole}, true, site)
	assert.Equal(t, ir.Unmodeled{"x-a": apart}, got, "a voice kept whole after it writes nothing")
}

// TestCopyEntriesInto_KeepsWhatTheCarrierHolds pins that a copied entry never
// replaces one the carrier already holds: the held one stays, the copy is
// reported missing from that carrier, and every other entry lands with bytes of
// its own.
func TestCopyEntriesInto_KeepsWhatTheCarrierHolds(t *testing.T) {
	t.Parallel()
	entry := func(value, pointer string) ir.UnmodeledEntry {
		return ir.UnmodeledEntry{Reason: ir.ReasonOutOfScope, Value: ir.RawValue(value),
			Provenance: ir.Provenance{Pointer: jsontext.Pointer(pointer)}}
	}
	held := entry(`"own"`, "/paths/~1a/get/k")
	unmodeled := ir.Unmodeled{"openapi:k": held}
	src := ir.Unmodeled{"openapi:k": entry(`2`, "/paths/~1a/k"), "openapi:x-a": entry(`1`, "/paths/~1a/x-a")}

	diags := CopyEntriesInto(&unmodeled, src)

	assert.Equal(t, held, unmodeled["openapi:k"], "the entry the carrier held stays")
	require.Len(t, diags, 1)
	assert.Equal(t, diag.UnknownKeyEntryAlreadyClaimed, diags[0].Code)
	assert.Equal(t, jsontext.Pointer("/paths/~1a/k"), diags[0].Provenance.Pointer, "sited at the copy that was refused")
	assert.Contains(t, diags[0].Message, `"/paths/~1a/get/k"`, "naming the construct that holds the entry")
	assert.NotContains(t, diags[0].Message, "in no form", "another carrier may hold the copy, so the IR is not said to lack it")
	unmodeled["openapi:x-a"].Value[0] = '9'
	assert.JSONEq(t, `1`, string(src["openapi:x-a"].Value), "the copy owns its bytes")

	var unset ir.Unmodeled
	assert.Empty(t, CopyEntriesInto(&unset, src))
	assert.Len(t, unset, 2, "a carrier holding nothing takes every entry")
	var untouched ir.Unmodeled
	assert.Nil(t, CopyEntriesInto(&untouched, nil))
	assert.Nil(t, untouched, "nothing to copy allocates nothing")
}

// TestCopyEntriesInto_ReportsInKeyOrder pins that the refused entries are
// reported in key order rather than the order a map iterates in. Go iterates a
// map of eight or fewer entries as a rotation of the order they were written in,
// so eight keys written sorted would let an unsorted report pass one run in
// eight. Sixty-four, written in reverse, leave the order to the map's hash seed.
func TestCopyEntriesInto_ReportsInKeyOrder(t *testing.T) {
	t.Parallel()
	const refused = 64
	unmodeled, src, want := ir.Unmodeled{}, ir.Unmodeled{}, make([]string, refused)
	for i := refused - 1; i >= 0; i-- {
		key := fmt.Sprintf("openapi:k%02d", i)
		unmodeled[key] = ir.UnmodeledEntry{Value: ir.RawValue(`1`), Provenance: ir.Provenance{Pointer: "/held"}}
		src[key] = ir.UnmodeledEntry{Value: ir.RawValue(`2`), Provenance: ir.Provenance{Pointer: jsontext.Pointer("/" + key)}}
		want[i] = "/" + key
	}

	diags := CopyEntriesInto(&unmodeled, src)
	got := make([]string, 0, len(diags))
	for _, diagnostic := range diags {
		got = append(got, string(diagnostic.Provenance.Pointer))
	}
	assert.Equal(t, want, got)
}
