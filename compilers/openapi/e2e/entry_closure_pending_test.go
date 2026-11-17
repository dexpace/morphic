// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

//go:build pending

package e2e_test

import (
	"fmt"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
)

// closureTyped is a closure written on an entry of an allOf, and what the
// model the position lowers to holds of it where the entry is the only
// conjunct declaring members, or none declares any.
type closureTyped struct {
	name    string
	closure string   // the entry
	others  []string // the other entries
	base    bool     // the model's base is closureUser
	props   []string // the members the model declares, by wire name
	only31  bool     // patternProperties is a 3.1 keyword
	check   func(t *testing.T, model *ir.Model, at string)
}

// requireClosureClosed holds a model to be closed and to have no catch-all.
func requireClosureClosed(t *testing.T, model *ir.Model, at string) {
	t.Helper()
	assert.Equal(t, ir.AdditionalClosed, model.Additional, "closed at %s", at)
	assert.Nil(t, model.AdditionalProps, "no catch-all at %s", at)
}

// requireClosurePatterns holds a model to hold the one pattern ^x- of strings.
func requireClosurePatterns(t *testing.T, model *ir.Model, at string) {
	t.Helper()
	assert.Equal(t, ir.AdditionalUnspecified, model.Additional, "open at %s", at)
	require.NotNil(t, model.AdditionalProps, "a catch-all at %s", at)
	require.Len(t, model.AdditionalProps.Patterns, 1, at)
	assert.Equal(t, "^x-", model.AdditionalProps.Patterns[0].Pattern, at)
	assert.Equal(t, prim(ir.PrimString), model.AdditionalProps.Patterns[0].Value.Target, at)
}

// requireClosureValues holds a model to hold a catch-all of strings.
func requireClosureValues(t *testing.T, model *ir.Model, at string) {
	t.Helper()
	assert.Equal(t, ir.AdditionalUnspecified, model.Additional, "open at %s", at)
	require.NotNil(t, model.AdditionalProps, "a catch-all at %s", at)
	require.NotNil(t, model.AdditionalProps.Value, at)
	assert.Equal(t, prim(ir.PrimString), model.AdditionalProps.Value.Target, at)
}

// closureTypedCases are the entries that type the closure they write.
func closureTypedCases() []closureTyped {
	return []closureTyped{
		{name: "a closure, no member declared", closure: "{additionalProperties: false}", others: []string{closureRef}, base: true, check: requireClosureClosed},
		{name: "a closure of an object, no member declared", closure: "{type: object, additionalProperties: false}", others: []string{closureRef}, base: true, check: requireClosureClosed},
		{
			name: "a closure declaring its own members", closure: "{properties: {b: {type: string}}, additionalProperties: false}",
			others: []string{closureRef}, base: true, props: []string{"b"}, check: requireClosureClosed,
		},
		{name: "a closure of no $ref, no member declared", closure: "{type: object, additionalProperties: false}", others: []string{"{required: [a]}"}, check: requireClosureClosed},
		{name: "a catch-all, no member declared", closure: "{additionalProperties: {type: string}}", others: []string{closureRef}, base: true, check: requireClosureValues},
		{
			name: "patterns, no member declared", closure: "{patternProperties: {'^x-': {type: string}}}",
			others: []string{closureRef}, base: true, only31: true, check: requireClosurePatterns,
		},
		{
			name: "patterns declaring their own members", closure: "{properties: {b: {type: string}}, patternProperties: {'^x-': {type: string}}}",
			others: []string{closureRef}, base: true, props: []string{"b"}, only31: true, check: requireClosurePatterns,
		},
	}
}

// TestEntryClosure_AClosureOfTheOnlyEntryDeclaringMembersIsTyped holds that an
// entry writing additionalProperties or patternProperties types it where it is
// the only conjunct declaring properties or patternProperties, or none does:
// the model closes, or takes the catch-all, as it does where the keyword is
// written beside the allOf, with nothing kept and nothing reported, at every
// position, in both versions the keyword exists in and in every order of the
// entries.
func TestEntryClosure_AClosureOfTheOnlyEntryDeclaringMembersIsTyped(t *testing.T) {
	t.Parallel()
	for _, tc := range closureTypedCases() {
		for _, version := range []string{"3.0.3", "3.1.0"} {
			if tc.only31 && version != "3.1.0" {
				continue
			}
			for _, order := range closureOrders(tc.closure, tc.others...) {
				t.Run(fmt.Sprintf("%s/%s/at-%d", version, tc.name, order.at), func(t *testing.T) {
					t.Parallel()
					inBothMappingOrders(t, conjunctionSpec(version, "{allOf: "+order.list+"}", closureUser), func(t *testing.T, spec string) {
						doc, diags := compileAnnotationSpec(t, "typed", spec)
						requireErrorsAndWarnings(t, diags, nil)
						for _, at := range conjunctionPositions {
							model := closureModelAt(t, doc, at)
							tc.check(t, model, string(at))
							if tc.base {
								require.NotNil(t, model.Base, "the base at %s", at)
								assert.Equal(t, componentID("User"), model.Base.Target, at)
							} else {
								assert.Nil(t, model.Base, "no base at %s", at)
							}
							var got []string
							for _, p := range model.Properties {
								got = append(got, p.WireName)
							}
							assert.Equal(t, tc.props, got, "the members declared at %s", at)
							assert.Empty(t, keptUnder(doc, at), "nothing is kept at %s", at)
							assert.Empty(t, degradedUnder(diags, at), "nothing is reported at %s", at)
						}
					})
				})
			}
		}
	}
}

// TestEntryClosure_TheEntryAndTheBesideSpellingsAreOneModel holds that the
// closure, the catch-all and the patterns written on an entry give the model
// the beside spelling gives, the one declaring no member: the two documents
// are one but for the pointer each is written at.
func TestEntryClosure_TheEntryAndTheBesideSpellingsAreOneModel(t *testing.T) {
	t.Parallel()
	for _, keyword := range []struct {
		name, text string
		only31     bool
	}{
		{"closure", "additionalProperties: false", false},
		{"catch-all", "additionalProperties: {type: string}", false},
		{"patterns", "patternProperties: {'^x-': {type: string}}", true},
	} {
		for _, version := range []string{"3.0.3", "3.1.0"} {
			if keyword.only31 && version != "3.1.0" {
				continue
			}
			t.Run(fmt.Sprintf("%s/%s", version, keyword.name), func(t *testing.T) {
				t.Parallel()
				beside, _ := compileAnnotationSpec(t, "one", conjunctionSpec(version, "{allOf: ["+closureRef+"], "+keyword.text+"}", closureUser))
				entry, _ := compileAnnotationSpec(t, "one", conjunctionSpec(version, "{allOf: ["+closureRef+", {"+keyword.text+"}]}", closureUser))
				for _, at := range conjunctionPositions {
					want := closureModelAt(t, beside, at)
					got := closureModelAt(t, entry, at)
					assert.Equal(t, want.Additional, got.Additional, "openness at %s", at)
					assert.Equal(t, want.AdditionalProps, got.AdditionalProps, "catch-all at %s", at)
					assert.Equal(t, want.Base, got.Base, "base at %s", at)
				}
			})
		}
	}
}

// TestEntryClosure_ABesideClosureReadingAnotherConjunctsMembersIsKept holds
// that a closure or a pattern written beside an allOf whose entries declare
// the members, the position declaring none of its own, is kept under its own
// key for want of a home: it reads against the position's own properties, as
// it does on an entry, so it does not close the members the entries declare.
// The model is open, the keyword is kept once on it and reported once.
func TestEntryClosure_ABesideClosureReadingAnotherConjunctsMembersIsKept(t *testing.T) {
	t.Parallel()
	const props = "{properties: {b: {type: string}}}"
	for _, tc := range []struct {
		name, besides, key string
		only31             bool
	}{
		{"closure", "additionalProperties: false", "openapi:additionalProperties", false},
		{"patterns", "patternProperties: {'^x-': {type: string}}", "openapi:patternProperties", true},
	} {
		for _, version := range []string{"3.0.3", "3.1.0"} {
			if tc.only31 && version != "3.1.0" {
				continue
			}
			for name, entries := range map[string]string{
				"an entry and a ref":      "[" + props + ", " + closureRef + "]",
				"the same, the other way": "[" + closureRef + ", " + props + "]",
				"one entry":               "[" + props + "]",
			} {
				t.Run(fmt.Sprintf("%s/%s/%s", version, tc.name, name), func(t *testing.T) {
					t.Parallel()
					schema := "{allOf: " + entries + ", " + tc.besides + "}"
					inBothMappingOrders(t, conjunctionSpec(version, schema, closureUser), func(t *testing.T, spec string) {
						doc, diags := compileAnnotationSpec(t, "beside", spec)
						requireErrorsAndWarnings(t, diags, nil)
						for _, at := range conjunctionPositions {
							model := closureModelAt(t, doc, at)
							assert.Equal(t, ir.AdditionalUnspecified, model.Additional, "open at %s", at)
							assert.Nil(t, model.AdditionalProps, "no catch-all at %s", at)
							assert.Equal(t, entryKept(tc.key), keptOn(model), "what the model keeps at %s", at)
							assert.Len(t, entryKeptUnder(doc, at), 1, "nothing else is kept at %s", at)
							assert.Len(t, degradedUnder(diags, at), 1, "one info at %s", at)
							assert.True(t, slices.ContainsFunc(model.Properties, func(p ir.Property) bool { return p.WireName == "b" }), at)
						}
					})
				})
			}
		}
	}
}
