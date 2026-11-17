// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test

import (
	"encoding/json/jsontext"
	"fmt"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
)

// TestEntryClosure_APatternBesideTheAllOfIsTypedWhateverItsEntriesDeclare
// holds that patternProperties written beside an allOf types the model's
// catch-all though an entry declares members: a pattern applies to every
// member name it matches and reads no properties keyword (JSON Schema 2020-12
// §10.3.2.2), so the members an entry declares change nothing it says. Nothing
// is kept and nothing reported, in every order of the entries.
func TestEntryClosure_APatternBesideTheAllOfIsTypedWhateverItsEntriesDeclare(t *testing.T) {
	t.Parallel()
	for _, order := range closureOrders("{properties: {b: {type: string}}}", closureRef) {
		t.Run(fmt.Sprintf("at-%d", order.at), func(t *testing.T) {
			t.Parallel()
			schema := "{allOf: " + order.list + ", patternProperties: {'^x-': {type: string}}}"
			inBothMappingOrders(t, conjunctionSpec("3.1.0", schema, closureUser), func(t *testing.T, spec string) {
				doc, diags := compileAnnotationSpec(t, "pattern", spec)
				requireErrorsAndWarnings(t, diags, nil)
				for _, at := range conjunctionPositions {
					model := closureModelAt(t, doc, at)
					requireClosurePatterns(t, model, string(at))
					require.NotNil(t, model.AdditionalProps.Value, "a key no pattern matches at %s", at)
					assert.Equal(t, prim(ir.PrimAny), model.AdditionalProps.Value.Target, "is anything at %s", at)
					assert.True(t, slices.ContainsFunc(model.Properties, func(p ir.Property) bool { return p.WireName == "b" }), at)
					assert.Empty(t, keptUnder(doc, at), "nothing is kept at %s", at)
					assert.Empty(t, degradedUnder(diags, at), "nothing is reported at %s", at)
				}
			})
		})
	}
}

// TestEntryClosure_TheClosureBesideTheAllOfIsTheModelsBeforeAnEntrys holds
// that where no conjunct declares a member, a closure beside the allOf types
// the model and one an entry writes is kept whole, as before: the model's
// catch-all has one source, the position, whichever index the entry is
// written at.
func TestEntryClosure_TheClosureBesideTheAllOfIsTheModelsBeforeAnEntrys(t *testing.T) {
	t.Parallel()
	for _, order := range closureOrders("{additionalProperties: false}", closureRef) {
		t.Run(fmt.Sprintf("at-%d", order.at), func(t *testing.T) {
			t.Parallel()
			schema := "{allOf: " + order.list + ", additionalProperties: {type: string}}"
			inBothMappingOrders(t, conjunctionSpec("3.0.3", schema, closureUser), func(t *testing.T, spec string) {
				doc, diags := compileAnnotationSpec(t, "both", spec)
				requireErrorsAndWarnings(t, diags, nil)
				for _, at := range conjunctionPositions {
					entry := at + jsontext.Pointer(fmt.Sprintf("/allOf/%d", order.at))
					model := closureModelAt(t, doc, at)
					requireClosureValues(t, model, string(at))
					assert.Equal(t, entryKept(fmt.Sprintf("openapi:allOf/%d", order.at)), keptOn(model), "the entry at %s", at)
					assert.Equal(t, []jsontext.Pointer{entry}, degradedUnder(diags, at), "one info at %s", at)
				}
			})
		})
	}
}

// TestEntryClosure_TwoEntriesWritingAClosureTypeNeither holds that where two
// entries write additionalProperties, neither types the model: each reads
// against no member, so the model's catch-all would be what both admit, which
// no single schema the IR names states. Each entry is kept whole and reported,
// and the model is open, in both orders of the entries.
func TestEntryClosure_TwoEntriesWritingAClosureTypeNeither(t *testing.T) {
	t.Parallel()
	for _, order := range closureOrders("{type: object, additionalProperties: {type: string}}", "{type: object, additionalProperties: {minLength: 1}}") {
		t.Run(fmt.Sprintf("at-%d", order.at), func(t *testing.T) {
			t.Parallel()
			inBothMappingOrders(t, conjunctionSpec("3.1.0", "{allOf: "+order.list+"}"), func(t *testing.T, spec string) {
				doc, diags := compileAnnotationSpec(t, "two", spec)
				requireErrorsAndWarnings(t, diags, nil)
				for _, at := range conjunctionPositions {
					model := closureModelAt(t, doc, at)
					assert.Equal(t, ir.AdditionalUnspecified, model.Additional, "open at %s", at)
					assert.Nil(t, model.AdditionalProps, "no catch-all at %s", at)
					assert.Equal(t, entryKept("openapi:allOf/0", "openapi:allOf/1"), keptOn(model), "both entries at %s", at)
					assert.Len(t, degradedUnder(diags, at), 2, "one info each at %s", at)
				}
			})
		})
	}
}

// TestEntryClosure_AFalseEntryKeepsTheCatchAllAnEntryTypes holds that a false
// entry closing the model keeps the catch-all value the entry closing it typed,
// since a closed model holds none: kept under its own key at the keyword, the
// false kept beside it, in both orders of the entries.
func TestEntryClosure_AFalseEntryKeepsTheCatchAllAnEntryTypes(t *testing.T) {
	t.Parallel()
	for _, order := range closureOrders("{additionalProperties: {type: string}}", "false") {
		t.Run(fmt.Sprintf("at-%d", order.at), func(t *testing.T) {
			t.Parallel()
			doc, diags := compileAnnotationSpec(t, "false", conjunctionSpec("3.1.0", "{allOf: "+order.list+"}"))
			requireErrorsAndWarnings(t, diags, nil)
			for _, at := range conjunctionPositions {
				model := closureModelAt(t, doc, at)
				requireClosureClosed(t, model, string(at))
				closer := at + jsontext.Pointer(fmt.Sprintf("/allOf/%d/additionalProperties", order.at))
				kept := unmodeledEntry(t, model.Unmodeled, "openapi:additionalProperties")
				assert.Equal(t, closer, kept.Provenance.Pointer, "the value is kept where it is written at %s", at)
				assert.JSONEq(t, `{"type":"string"}`, string(kept.Value), at)
				assert.Contains(t, model.Unmodeled, fmt.Sprintf("openapi:allOf/%d", 1-order.at), "the false at %s", at)
			}
		})
	}
}

// TestEntryClosure_UnevaluatedPropertiesBesideTheClosingEntry holds what an
// unevaluatedProperties: false beside the allOf does where an entry closes the
// model. A catch-all the entry writes evaluates every member, so the keyword is
// inert: the model takes the entry's openness and keeps the keyword as
// validation logic. A pattern alone leaves other members to it, so it closes
// the model after composition. Beside a closure kept for an entry's members, it
// is inert beside that closure, and both are kept.
func TestEntryClosure_UnevaluatedPropertiesBesideTheClosingEntry(t *testing.T) {
	t.Parallel()
	const up = ", unevaluatedProperties: false}"
	doc, diags := compileAnnotationSpec(t, "unevaluated", conjunctionSpec("3.1.0",
		"{type: object, allOf: [{additionalProperties: {type: integer}}]"+up,
	))
	requireErrorsAndWarnings(t, diags, nil)
	for _, at := range conjunctionPositions {
		m, ok := kindNode(t, doc, usedAt(t, doc, at)).(*ir.MapT)
		require.True(t, ok, "a map of the entry's catch-all at %s", at)
		assert.Equal(t, prim(ir.PrimInteger), m.Value.Target, at)
		assert.Equal(t, map[string]ir.UnmodeledReason{"openapi:unevaluated": ir.ReasonValidationOnly}, keptOn(m), at)
	}

	doc, diags = compileAnnotationSpec(t, "patterns", conjunctionSpec("3.1.0",
		"{type: object, allOf: [{patternProperties: {'^x-': {type: string}}}]"+up))
	requireErrorsAndWarnings(t, diags, nil)
	for _, at := range conjunctionPositions {
		model := closureModelAt(t, doc, at)
		assert.Equal(t, ir.AdditionalClosedAfterComposition, model.Additional, at)
		require.NotNil(t, model.AdditionalProps, at)
		assert.Nil(t, model.AdditionalProps.Value, "a key no pattern matches is the composition's at %s", at)
		assert.Len(t, model.AdditionalProps.Patterns, 1, at)
		assert.Empty(t, keptUnder(doc, at), at)
	}

	doc, diags = compileAnnotationSpec(t, "closed", conjunctionSpec("3.1.0",
		"{allOf: ["+closureRef+", {additionalProperties: false}]"+up, closureUser))
	requireErrorsAndWarnings(t, diags, nil)
	for _, at := range conjunctionPositions {
		model := closureModelAt(t, doc, at)
		requireClosureClosed(t, model, string(at))
		assert.Equal(t, map[string]ir.UnmodeledReason{"openapi:unevaluated": ir.ReasonValidationOnly}, keptOn(model), at)
	}

	doc, diags = compileAnnotationSpec(t, "kept", conjunctionSpec("3.1.0",
		"{allOf: [{type: object, properties: {a: {type: string}}}], additionalProperties: {type: integer}"+up))
	requireErrorsAndWarnings(t, diags, nil)
	for _, at := range conjunctionPositions {
		model := closureModelAt(t, doc, at)
		assert.Equal(t, ir.AdditionalUnspecified, model.Additional, at)
		assert.Nil(t, model.AdditionalProps, at)
		assert.Equal(t, map[string]ir.UnmodeledReason{
			"openapi:additionalProperties": ir.ReasonDegradedLowering,
			"openapi:unevaluated":          ir.ReasonValidationOnly,
		}, keptOn(model), at)
		assert.JSONEq(t, `{"additionalProperties":{"type":"integer"},"unevaluatedProperties":false}`,
			string(model.Unmodeled["openapi:unevaluated"].Value), "inert beside the closure at %s", at)
	}
}

// TestEntryClosure_ARefEntrysOwnMembersAreDeclaredBesideTheClosure holds that
// properties written beside a $ref entry are members another schema object of
// the conjunction declares, which a closure beside the allOf does not read, so
// it is kept; the members the $ref names are the target's, and a closure
// beside them types the model.
func TestEntryClosure_ARefEntrysOwnMembersAreDeclaredBesideTheClosure(t *testing.T) {
	t.Parallel()
	const entry = "{$ref: '#/components/schemas/User', properties: {b: {type: string}}}"
	doc, diags := compileAnnotationSpec(t, "siblings", conjunctionSpec("3.1.0",
		"{allOf: ["+entry+"], additionalProperties: false}", closureUser))
	requireErrorsAndWarnings(t, diags, nil)
	for _, at := range conjunctionPositions {
		model := closureModelAt(t, doc, at)
		assert.Equal(t, ir.AdditionalUnspecified, model.Additional, "open at %s", at)
		assert.Equal(t, ir.ReasonDegradedLowering, unmodeledEntry(t, model.Unmodeled, "openapi:additionalProperties").Reason, at)
		assert.Contains(t, degradedUnder(diags, at), at, "reported at the schema writing it at %s", at)
	}
}

// TestEntryClosure_AClosureBesideAUnionReadsTheAllOfAlike holds that the body a
// union distributes into reads a closure as the allOf it writes says: an entry
// closing the model closes every variant, and a closure beside entries
// declaring members closes none, kept on each and reported once.
func TestEntryClosure_AClosureBesideAUnionReadsTheAllOfAlike(t *testing.T) {
	t.Parallel()
	const union = "oneOf: [{$ref: '#/components/schemas/A'}, {$ref: '#/components/schemas/B'}]"
	components := []string{
		closureUser,
		"A: {type: object, properties: {a: {type: string}}}",
		"B: {type: object, properties: {b: {type: string}}}",
	}
	for _, tc := range []struct {
		name, schema string
		closed       bool
		kept         map[string]ir.UnmodeledReason
		infos        int
	}{
		{"an entry closing", "{allOf: [" + closureRef + ", {additionalProperties: false}], " + union + "}", true, map[string]ir.UnmodeledReason{}, 0},
		{"a closure kept", "{allOf: [{properties: {c: {type: string}}}], additionalProperties: false, " + union + "}", false,
			entryKept("openapi:additionalProperties"), 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc, diags := compileAnnotationSpec(t, "union", conjunctionSpec("3.1.0", tc.schema, components...))
			requireErrorsAndWarnings(t, diags, nil)
			for _, at := range conjunctionPositions {
				u, ok := kindNode(t, doc, usedAt(t, doc, at)).(*ir.Union)
				require.True(t, ok, "a union at %s", at)
				require.Len(t, u.Variants, 2, at)
				for _, v := range u.Variants {
					variant, ok := kindNode(t, doc, v.Type.Target).(*ir.Model)
					require.True(t, ok, "a composed variant at %s", at)
					assert.Equal(t, tc.closed, variant.Additional == ir.AdditionalClosed, "closed at %s", at)
					assert.Equal(t, tc.kept, keptOn(variant), at)
				}
				assert.Len(t, degradedUnder(diags, at), tc.infos, at)
			}
		})
	}
}

// TestEntryClosure_TheEntryOrderLeavesTheDocumentAlone holds that the closure
// each spelling types or keeps is the same document whichever order its two
// entries are written in, diagnostics included, but for the pointers of the
// entries: whether an entry closes the model, is kept whole, or leaves the
// closure beside the allOf kept is read from what the conjuncts declare, never
// from where they stand.
func TestEntryClosure_TheEntryOrderLeavesTheDocumentAlone(t *testing.T) {
	t.Parallel()
	const union = ", oneOf: [{$ref: '#/components/schemas/A'}, {$ref: '#/components/schemas/B'}]"
	for _, tc := range []struct{ name, first, second, beside string }{
		{"an entry closing", closureRef, "{additionalProperties: false}", ""},
		{"an entry kept", "{properties: {b: {type: string}}}", "{additionalProperties: false}", ""},
		{"two entries", "{additionalProperties: {type: string}}", "{additionalProperties: {minLength: 1}}", ""},
		{"a false entry", "{additionalProperties: {type: string}}", "false", ""},
		{"a closure beside kept", "{properties: {b: {type: string}}}", closureRef, ", additionalProperties: false"},
		{"a closure beside first", closureRef, "{additionalProperties: false}", ", additionalProperties: {type: string}"},
		{"a pattern beside", "{properties: {b: {type: string}}}", closureRef, ", patternProperties: {'^x-': {type: string}}"},
		{"beside a union", closureRef, "{additionalProperties: false}", union},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			components := []string{
				closureUser,
				"A: {type: object, properties: {a: {type: string}}}",
				"B: {type: object, properties: {c: {type: string}}}",
			}
			first, _ := compileAnnotationSpec(t, "order", conjunctionSpec("3.1.0",
				"{allOf: ["+tc.first+", "+tc.second+"]"+tc.beside+"}", components...))
			last, _ := compileAnnotationSpec(t, "order", conjunctionSpec("3.1.0",
				"{allOf: ["+tc.second+", "+tc.first+"]"+tc.beside+"}", components...))
			assert.Equal(t, comparableDocument(t, last), entriesSwapped(t, first), "the entry order decided the document")
		})
	}
}
